package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

var (
	bundleRobot  string
	bundleOutput string
)

var bundleCmd = &cobra.Command{
	Use:   "bundle",
	Short: "Create a self-contained robot bundle.",
	Long: `Create a self-contained robot bundle that includes the robot code and the
environment (hololib). The output is an executable Python script that can be
executed by 'rcc robot run-from-bundle'.`,
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
		tempHololib := filepath.Join(os.TempDir(), fmt.Sprintf("hololib_%s.zip", hash))
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
		err = createBundle(bundleRobot, tempHololib, bundleOutput, config.CondaConfigFile())
		pretty.Guard(err == nil, 7, "Failed to create bundle: %v", err)

		pretty.Ok()
	},
}

func init() {
	robotCmd.AddCommand(bundleCmd)
	bundleCmd.Flags().StringVarP(&bundleRobot, "robot", "r", "robot.yaml", "Path to robot.yaml.")
	bundleCmd.Flags().StringVarP(&bundleOutput, "output", "o", "bundle.py", "Output bundle filename.")
}

func createBundle(robotYamlPath, hololibPath, outputPath, condaConfigPath string) error {
	// Create output file
	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

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
	defer zw.Close()

	// Add robot files
	baseDir := filepath.Dir(robotYamlPath)
	
	// Get absolute path of output to avoid skipping wrong files
	absOutputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	
	err = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}

		// Ignore output directory and .git, etc.
		// Also ignore the bundle itself if it's being created inside the directory
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if absPath == absOutputPath {
			return nil
		}
		
		if strings.HasPrefix(relPath, "output") || strings.HasPrefix(relPath, ".git") || strings.HasPrefix(relPath, ".") {
			if info.IsDir() && relPath != "." {
				return filepath.SkipDir
			}
			if relPath != "." {
				return nil
			}
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

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		return err
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
		f, err := os.Open(condaConfigPath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		if err != nil {
			return err
		}
	}

	// Add hololib.zip
	w, err := zw.Create("hololib/hololib.zip")
	if err != nil {
		return err
	}
	f, err := os.Open(hololibPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)

	return err
}
