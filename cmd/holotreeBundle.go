package cmd

import (
	"archive/zip"
	"encoding/binary"
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
	bundleJsonFlag    bool
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

		// Open the bundle as a ZIP file (or appended ZIP)
		zr, closer, err := openBundle(bundleFile)
		pretty.Guard(err == nil, 2, "Could not open bundle %q: %v", bundleFile, err)
		defer closer.Close()

		// Import hololib if present
		err = importHololib(zr)
		pretty.Guard(err == nil, 3, "Failed to import hololib from bundle: %v", err)

		// Process environments
		results, err := processBundleEnvs(zr, bundleFile, bundleForceFlag, bundleRestoreFlag)
		pretty.Guard(err == nil, 4, "Failed to process environments: %v", err)

		if len(results) == 0 {
			pretty.Exit(3, "No environment definitions (envs/*/conda.yaml) found in bundle %q", bundleFile)
		}

		// Output results
		if bundleJsonFlag {
			output := make(map[string]interface{})
			output["bundle"] = bundleFile
			output["total"] = len(results)
			succeeded := 0
			for _, r := range results {
				if r.Success {
					succeeded++
				}
			}
			output["succeeded"] = succeeded
			output["failed"] = len(results) - succeeded
			output["environments"] = results

			jsonOutput, err := operations.NiceJsonOutput(output)
			pretty.Guard(err == nil, 5, "Failed to generate JSON output: %v", err)
			fmt.Println(jsonOutput)
		} else {
			// Final summary
			failed := 0
			for _, r := range results {
				if !r.Success {
					failed++
				}
			}
			if failed > 0 {
				pretty.Exit(4, "%d out of %d environment builds failed! See output above for details.", failed, len(results))
			}

			pretty.Ok()
			pretty.Note("Successfully built %d environment(s) from bundle.", len(results))
		}
	},
}

type envFileInfo struct {
	name    string
	zipFile *zip.File
}

// findEnvFiles searches for all conda.yaml files under envs/ in the ZIP
func findEnvFiles(zr *zip.Reader) []envFileInfo {
	var files []envFileInfo

	for _, f := range zr.File {
		// Normalize path separators
		name := filepath.ToSlash(f.Name)

		// Look for envs/<name>/conda.yaml pattern
		if strings.HasPrefix(name, "envs/") && strings.HasSuffix(name, "/conda.yaml") {
			// Extract environment name
			parts := strings.Split(name, "/")
			if len(parts) >= 3 {
				envName := parts[1]
				files = append(files, envFileInfo{
					name:    envName,
					zipFile: f,
				})
			}
		}
	}

	return files
}

// hasHololibZip checks if the bundle contains a hololib.zip
func hasHololibZip(zr *zip.Reader) bool {
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

type offsetReaderAt struct {
	r      io.ReaderAt
	offset int64
}

func (o *offsetReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	return o.r.ReadAt(p, off+o.offset)
}

func openBundle(filename string) (*zip.Reader, io.Closer, error) {
	// Try standard zip open first
	zr, err := zip.OpenReader(filename)
	if err == nil {
		return &zr.Reader, zr, nil
	}

	// If failed, try to find appended zip
	f, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	size := fi.Size()

	start, err := findZipStart(f, size)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("could not open as zip or find appended zip: %v", err)
	}

	ra := &offsetReaderAt{r: f, offset: start}
	zr2, err := zip.NewReader(ra, size-start)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return zr2, f, nil
}

func findZipStart(f *os.File, size int64) (int64, error) {
	const eocdHeaderSize = 22
	const maxCommentSize = 65535

	// Read last 64KB + 22 bytes
	readSize := int64(maxCommentSize + eocdHeaderSize)
	if readSize > size {
		readSize = size
	}

	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, size-readSize); err != nil {
		return 0, err
	}

	// Scan backwards for signature 0x06054b50
	for i := len(buf) - eocdHeaderSize; i >= 0; i-- {
		// Check signature
		if buf[i] == 0x50 && buf[i+1] == 0x4b && buf[i+2] == 0x05 && buf[i+3] == 0x06 {
			// Found signature candidate
			// Check comment length
			commentLen := int(binary.LittleEndian.Uint16(buf[i+20 : i+22]))
			expectedEnd := i + eocdHeaderSize + commentLen
			if expectedEnd == len(buf) {
				// Valid EOCD found
				// Get Size of CD (offset 12)
				sizeCD := int64(binary.LittleEndian.Uint32(buf[i+12 : i+16]))
				// Get Offset of CD (offset 16)
				offsetCD := int64(binary.LittleEndian.Uint32(buf[i+16 : i+20]))

				// EOCD starts at (size - readSize + i)
				eocdStart := size - readSize + int64(i)

				// Start of ZIP = EOCD Start - Size CD - Offset CD
				zipStart := eocdStart - sizeCD - offsetCD

				if zipStart < 0 {
					continue // Invalid, maybe false positive signature
				}
				return zipStart, nil
			}
		}
	}
	return 0, fmt.Errorf("EOCD not found")
}

func init() {
	holotreeCmd.AddCommand(holotreeBundleCmd)
	holotreeBundleCmd.Flags().BoolVarP(&bundleForceFlag, "force", "f", false, "Force environment builds, even when blueprint is already present.")
	holotreeBundleCmd.Flags().BoolVarP(&bundleRestoreFlag, "restore", "r", false, "Also restore the environment to a space (not just build catalog).")
	holotreeBundleCmd.Flags().BoolVarP(&bundleJsonFlag, "json", "j", false, "Output results as JSON.")
}

type envResult struct {
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Path    string `json:"path,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// processBundleEnvs processes all environment definitions found in the bundle's envs/ directory.
// For each environment, it either builds or restores it based on the restore flag.
// It returns a slice of envResult containing the result for each environment processed.
//
// Parameters:
//   zr             - the zip.Reader for the bundle file
//   bundleFilename - the name of the bundle file
//   force          - if true, forces environment builds even if the blueprint is present
//   restore        - if true, restores the environment to a space (not just build catalog)
//
// Returns:
//   []envResult - results for each environment processed
//   error       - error if processing fails
func processBundleEnvs(zr *zip.Reader, bundleFilename string, force bool, restore bool) ([]envResult, error) {
	envFiles := findEnvFiles(zr)
	if len(envFiles) == 0 {
		return nil, nil
	}

	pretty.Note("Found %d environment(s) in bundle %q", len(envFiles), bundleFilename)

	results := make([]envResult, 0, len(envFiles))
	total := len(envFiles)

	for at, envFile := range envFiles {
		envName := envFile.name
		result := envResult{Name: envName, Success: false}

		pretty.Note("%d/%d: Processing environment %q", at+1, total, envName)

		tmpConda, err := extractCondaYaml(envFile.zipFile, envName)
		if err != nil {
			result.Error = fmt.Sprintf("Failed to extract conda.yaml: %v", err)
			results = append(results, result)
			pretty.Warning("%d/%d: Failed to extract conda.yaml for %q: %v", at+1, total, envName, err)
			continue
		}
		defer os.Remove(tmpConda)

		_, blueprint, err := htfs.ComposeFinalBlueprint([]string{tmpConda}, "", false)
		if err != nil {
			result.Error = fmt.Sprintf("Failed to compose blueprint: %v", err)
			results = append(results, result)
			pretty.Warning("%d/%d: Failed to compose blueprint for %q: %v", at+1, total, envName, err)
			continue
		}

		hash := common.BlueprintHash(blueprint)
		result.Hash = hash
		pretty.Note("%d/%d: Blueprint hash for %q is %s", at+1, total, envName, hash)

		if restore {
			label, _, err := htfs.NewEnvironment(tmpConda, "", true, force, operations.PullCatalog)
			if err != nil {
				result.Error = fmt.Sprintf("Failed to build environment: %v", err)
				results = append(results, result)
				pretty.Warning("%d/%d: Failed to build environment for %q: %v", at+1, total, envName, err)
			} else {
				result.Success = true
				result.Path = label
				results = append(results, result)
				pretty.Ok()
				pretty.Note("%d/%d: Built and restored environment %q with hash %s at %q", at+1, total, envName, hash, label)
			}
		} else {
			tree, err := htfs.New()
			if err != nil {
				result.Error = fmt.Sprintf("Failed to create Holotree library: %v", err)
				results = append(results, result)
				pretty.Warning("%d/%d: Failed to create Holotree library for %q: %v", at+1, total, envName, err)
				continue
			}

			scorecard := common.NewScorecard()
			err = htfs.RecordEnvironment(tree, blueprint, force, scorecard, operations.PullCatalog)
			if err != nil {

// importHololib checks for and imports a hololib.zip file from the bundle if present,
// extracting it to a temporary location and calling ProtectedImport.
				results = append(results, result)
				pretty.Warning("%d/%d: Failed to record environment for %q: %v", at+1, total, envName, err)
			} else {
				result.Success = true
				results = append(results, result)
				pretty.Ok()
				pretty.Note("%d/%d: Built environment %q with hash %s", at+1, total, envName, hash)
			}
		}
	}
	return results, nil
}

func importHololib(zr *zip.Reader) error {
	if !hasHololibZip(zr) {
		return nil
	}
	pretty.Note("Found hololib.zip in bundle, importing...")

	var hololibFile *zip.File
	for _, f := range zr.File {
		if filepath.ToSlash(f.Name) == "hololib/hololib.zip" {
			hololibFile = f
			break
		}
	}

	if hololibFile == nil {
		return nil
	}

	rc, err := hololibFile.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	tmpDir := pathlib.TempDir()
	tmpFile := filepath.Join(tmpDir, "hololib_import.zip")

	out, err := os.Create(tmpFile)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, rc)
	out.Close()
	if err != nil {
		os.Remove(tmpFile)
		return err
	}
	defer os.Remove(tmpFile)

	return operations.ProtectedImport(tmpFile)
}
