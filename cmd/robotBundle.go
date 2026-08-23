package cmd

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/operations"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/robot"
	"github.com/spf13/cobra"
)

var (
	bundleRobot         string
	bundleOutput        string
	bundleArtifact      string
	bundlePlatformIndex string
)

var bundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Create a self-contained robot bundle.",
	Long: `Create a robot bundle containing project files and an exported Holotree environment.

Project files excluded by robot.yaml ignoreFiles are omitted. Project symlinks are
rejected because their targets are outside the bundle's portable trust boundary.
The output is replaced atomically only after the complete bundle is written.

The generated Python file is a ZIP-compatible launcher that prints instructions;
run the robot with 'rcc robot run-from-bundle'. It is not an RCC carrier executable.`,
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Bundle creation lasted").Report()
		}

		// 1. Calculate environment hash
		config, blueprint, err := htfs.ComposeFinalBlueprint(nil, bundleRobot, false)
		pretty.Guard(err == nil, 1, "Failed to load robot configuration: %v", err)

		hash := common.BlueprintHash(blueprint)
		common.Log("Environment hash: %s", hash)

		// Ensure environment exists
		condafile := filepath.Join(common.ProductTemp(), hash)
		err = pathlib.WriteFile(condafile, blueprint, 0o644)
		pretty.Guard(err == nil, 2, "Failed to write conda file: %v", err)

		holozip := ""
		if config != nil {
			holozip = config.Holozip()
		}

		_, _, err = htfs.NewEnvironment(condafile, holozip, false, false, operations.PullCatalog)
		pretty.Guard(err == nil, 3, "Failed to create environment: %v", err)

		// 2. Export holotree
		temp, err := os.CreateTemp("", fmt.Sprintf("rcc-hololib-%s-*.zip", hash))
		pretty.Guard(err == nil, 6, "Failed to reserve temporary hololib file: %v", err)
		tempHololib := temp.Name()
		err = temp.Close()
		pretty.Guard(err == nil, 6, "Failed to close temporary hololib file: %v", err)
		err = os.Remove(tempHololib)
		pretty.Guard(err == nil, 6, "Failed to prepare temporary hololib path: %v", err)
		defer os.Remove(tempHololib)

		common.Log("Exporting holotree to %s...", tempHololib)
		tree, err := htfs.New()
		pretty.Guard(err == nil, 4, "Failed to initialize holotree: %v", err)

		// Find the full catalog name
		catalogs := htfs.CatalogNames()
		var catalogName string
		for _, name := range catalogs {
			if strings.HasPrefix(name, hash) {
				catalogName = name
				break
			}
		}
		pretty.Guard(catalogName != "", 5, "Could not find catalog for hash %s", hash)

		err = tree.Export([]string{catalogName}, nil, tempHololib)
		pretty.Guard(err == nil, 6, "Failed to export holotree: %v", err)

		// 3. Create bundle
		common.Log("Creating bundle %s...", bundleOutput)
		err = createBundleWithArtifactAndIndex(bundleRobot, tempHololib, bundleOutput, config.CondaConfigFile(), bundleArtifact, bundlePlatformIndex)
		pretty.Guard(err == nil, 7, "Failed to create bundle: %v", err)

		pretty.Ok()
	},
}

func init() {
	robotCmd.AddCommand(bundleCmd)
	bundleCmd.Flags().StringVarP(&bundleRobot, "robot", "r", "robot.yaml", "Path to robot.yaml.")
	bundleCmd.Flags().StringVarP(&bundleOutput, "output", "o", "bundle.py", "Output bundle filename.")
	bundleCmd.Flags().StringVar(&bundleArtifact, "artifact-archive", "", "Optional canonical environment artifact archive to embed.")
	bundleCmd.Flags().StringVar(&bundlePlatformIndex, "artifact-index", "", "Optional canonical multi-platform artifact index to embed.")
}

func createBundle(robotYamlPath, hololibPath, outputPath, condaConfigPath string) error {
	return createBundleWithArtifact(robotYamlPath, hololibPath, outputPath, condaConfigPath, "")
}

func createBundleWithArtifact(robotYamlPath, hololibPath, outputPath, condaConfigPath, artifactPath string) error {
	return createBundleWithArtifactAndIndex(robotYamlPath, hololibPath, outputPath, condaConfigPath, artifactPath, "")
}

func createBundleWithArtifactAndIndex(robotYamlPath, hololibPath, outputPath, condaConfigPath, artifactPath, platformIndexPath string) error {
	config, err := robot.LoadRobotYaml(robotYamlPath, false)
	if err != nil {
		return err
	}
	ignored, err := pathlib.LoadIgnoreFiles(config.IgnoreFiles())
	if err != nil {
		return err
	}

	absOutputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(absOutputPath), "."+filepath.Base(absOutputPath)+"-*.tmp")
	if err != nil {
		return err
	}
	tempOutputPath := out.Name()
	committed := false
	defer func() {
		if !committed {
			out.Close()
			os.Remove(tempOutputPath)
		}
	}()

	// Make it executable
	if err := out.Chmod(0755); err != nil {
		return err
	}

	// Write stub
	stub := `#!/usr/bin/env python3
import sys
import os

# Payload marker - everything after this line is the ZIP payload
PAYLOAD_MARKER = b"===RCC_PAYLOAD_START==="

def main():
    print("This is a self-contained robot bundle.")
    print("Run it with: rcc robot run-from-bundle " + os.path.basename(__file__))

if __name__ == "__main__":
    main()
`
	// Write stub + marker
	if _, err := out.WriteString(stub); err != nil {
		return err
	}
	// Ensure newline before marker
	if _, err := out.WriteString("\n# " + "===RCC_PAYLOAD_START===" + "\n"); err != nil {
		return err
	}

	// Create zip writer
	zw := zip.NewWriter(out)

	// Add robot files
	baseDir := filepath.Dir(robotYamlPath)
	defaults := operations.DefaultIgnores(absOutputPath)

	err = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if absPath == absOutputPath || absPath == tempOutputPath {
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("project path %q is a symbolic link", relPath)
		}
		if relPath != "." && (defaults(info) || ignored(info)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		// Add to robot/ prefix
		zipPath := filepath.Join("robot", relPath)
		// Ensure forward slashes for zip
		zipPath = filepath.ToSlash(zipPath)

		w, err := zw.Create(zipPath)
		if err != nil {
			return err
		}

		return copyBundleFile(w, path)
	})
	if err != nil {
		return err
	}

	// Add conda.yaml to envs/default/conda.yaml
	if condaConfigPath != "" {
		w, err := zw.Create("envs/default/conda.yaml")
		if err != nil {
			return err
		}
		if err := copyBundleFile(w, condaConfigPath); err != nil {
			return err
		}
	}

	// Add hololib.zip
	w, err := zw.Create("hololib/hololib.zip")
	if err != nil {
		return err
	}
	if err := copyBundleFile(w, hololibPath); err != nil {
		return err
	}
	manifest := map[string]string{"schemaVersion": "1", "sourceMode": "source-only"}
	if artifactPath != "" {
		manifest["sourceMode"] = "source+artifact"
		if platformIndexPath != "" {
			manifest["sourceMode"] = "source+artifact-index"
		}
		artifactWriter, err := zw.Create("environment/artifact.rcca")
		if err != nil {
			return err
		}
		if err := copyBundleFile(artifactWriter, artifactPath); err != nil {
			return err
		}
	}
	if platformIndexPath != "" {
		indexBytes, err := os.ReadFile(platformIndexPath)
		if err != nil {
			return err
		}
		if _, err := environmentartifact.DecodePlatformIndex(indexBytes); err != nil {
			return fmt.Errorf("invalid platform index: %w", err)
		}
		indexWriter, err := zw.Create("environment/platform-index.json")
		if err != nil {
			return err
		}
		if _, err := indexWriter.Write(indexBytes); err != nil {
			return err
		}
	}
	manifestWriter, err := zw.Create("environment/bundle.json")
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if _, err := manifestWriter.Write(encoded); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempOutputPath, absOutputPath); err != nil {
		return err
	}
	committed = true
	return nil
}

func copyBundleFile(target io.Writer, sourcePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := source.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
