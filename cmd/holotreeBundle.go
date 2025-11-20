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
	bundleForceFlag   bool
	bundleRestoreFlag bool
)

var holotreeBundleCmd = &cobra.Command{
	Use:   "build-from-bundle <bundle-file>",
	Short: "Build Holotree environments from a single-file bundle.",
	Long: `Build Holotree environments from a single-file bundle.

This command reads environment definitions (conda.yaml files) from a bundle
file (ZIP format) and builds the corresponding Holotree environments. The
bundle should contain environment definitions at paths like:
  envs/<env-name>/conda.yaml

Each environment found in the bundle will be built and cataloged in the
local Holotree library.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		bundleFile := args[0]

		if common.DebugFlag() {
			defer common.Stopwatch("Holotree build-from-bundle lasted").Report()
		}

		// Verify bundle file exists
		if !pathlib.IsFile(bundleFile) {
			pretty.Exit(1, "Bundle file %q does not exist or is not a file.", bundleFile)
		}

		// Open the bundle as a ZIP file
		zr, err := zip.OpenReader(bundleFile)
		pretty.Guard(err == nil, 2, "Could not open bundle %q as ZIP: %v", bundleFile, err)
		defer zr.Close()

		// Find all conda.yaml files under envs/
		envFiles := findEnvFiles(zr)
		if len(envFiles) == 0 {
			pretty.Exit(3, "No environment definitions (envs/*/conda.yaml) found in bundle %q", bundleFile)
		}

		pretty.Note("Found %d environment(s) in bundle %q", len(envFiles), bundleFile)

		// Check for hololib.zip (note but don't import in v1)
		if hasHololibZip(zr) {
			pretty.Note("Bundle contains pre-baked hololib.zip at /hololib/hololib.zip (not imported in v1).")
		}

		// Process each environment
		total := len(envFiles)
		failed := 0
		for at, envFile := range envFiles {
			envName := envFile.name
			pretty.Note("%d/%d: Processing environment %q", at+1, total, envName)

			// Extract conda.yaml to temp file
			tmpConda, err := extractCondaYaml(envFile.zipFile, envName)
			if err != nil {
				failed++
				pretty.Warning("%d/%d: Failed to extract conda.yaml for %q: %v", at+1, total, envName, err)
				continue
			}
			defer os.Remove(tmpConda)

			// Compose blueprint and get hash
			_, blueprint, err := htfs.ComposeFinalBlueprint([]string{tmpConda}, "", false)
			if err != nil {
				failed++
				pretty.Warning("%d/%d: Failed to compose blueprint for %q: %v", at+1, total, envName, err)
				continue
			}

			hash := common.BlueprintHash(blueprint)
			pretty.Note("%d/%d: Blueprint hash for %q is %s", at+1, total, envName, hash)

			// Build environment via existing Holotree pipeline
			if bundleRestoreFlag {
				// Use NewEnvironment which includes restore
				label, _, err := htfs.NewEnvironment(tmpConda, "", true, bundleForceFlag, operations.PullCatalog)
				if err != nil {
					failed++
					pretty.Warning("%d/%d: Failed to build environment for %q: %v", at+1, total, envName, err)
				} else {
					pretty.Ok()
					pretty.Note("%d/%d: Built and restored environment %q with hash %s at %q", at+1, total, envName, hash, label)
				}
			} else {
				// Use RecordEnvironment to just build without restoring space
				tree, err := htfs.New()
				if err != nil {
					failed++
					pretty.Warning("%d/%d: Failed to create Holotree library for %q: %v", at+1, total, envName, err)
					continue
				}

				scorecard := common.NewScorecard()
				err = htfs.RecordEnvironment(tree, blueprint, bundleForceFlag, scorecard, operations.PullCatalog)
				if err != nil {
					failed++
					pretty.Warning("%d/%d: Failed to record environment for %q: %v", at+1, total, envName, err)
				} else {
					pretty.Ok()
					pretty.Note("%d/%d: Built environment %q with hash %s", at+1, total, envName, hash)
				}
			}
		}

		// Final summary
		if failed > 0 {
			pretty.Exit(4, "%d out of %d environment builds failed! See output above for details.", failed, total)
		}

		pretty.Ok()
		pretty.Note("Successfully built %d environment(s) from bundle.", total)
	},
}

type envFileInfo struct {
	name    string
	zipFile *zip.File
}

// findEnvFiles searches for all conda.yaml files under envs/ in the ZIP
func findEnvFiles(zr *zip.ReadCloser) []envFileInfo {
	var envFiles []envFileInfo

	for _, f := range zr.File {
		// Normalize path separators
		name := filepath.ToSlash(f.Name)

		// Look for envs/<name>/conda.yaml pattern
		if strings.HasPrefix(name, "envs/") && strings.HasSuffix(name, "/conda.yaml") {
			// Extract environment name
			parts := strings.Split(name, "/")
			if len(parts) >= 3 {
				envName := parts[1]
				envFiles = append(envFiles, envFileInfo{
					name:    envName,
					zipFile: f,
				})
			}
		}
	}

	return envFiles
}

// hasHololibZip checks if the bundle contains a hololib.zip
func hasHololibZip(zr *zip.ReadCloser) bool {
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if name == "hololib/hololib.zip" {
			return true
		}
	}
	return false
}

// extractCondaYaml extracts a conda.yaml file from the ZIP to a temp file
func extractCondaYaml(f *zip.File, envName string) (string, error) {
	// Open the file in the ZIP
	rc, err := f.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open file in ZIP: %w", err)
	}
	defer rc.Close()

	// Read the content
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("failed to read file content: %w", err)
	}

	// Create temp file
	tmpDir := pathlib.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("bundle_%s_conda.yaml", envName))

	// Write to temp file
	err = os.WriteFile(tmpFile, data, 0o644)
	if err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	return tmpFile, nil
}

func init() {
	holotreeCmd.AddCommand(holotreeBundleCmd)
	holotreeBundleCmd.Flags().BoolVarP(&bundleForceFlag, "force", "f", false, "Force environment builds, even when blueprint is already present.")
	holotreeBundleCmd.Flags().BoolVarP(&bundleRestoreFlag, "restore", "r", false, "Also restore the environment to a space (not just build catalog).")
}
