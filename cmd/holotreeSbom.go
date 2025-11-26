package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/oci"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/sbom"
	"github.com/spf13/cobra"
)

var (
	sbomFormat   string
	sbomOutput   string
	sbomRegistry string
	sbomTag      string
	sbomRobot    string
)

func findCatalogByHash(hash string) (*htfs.Root, string, error) {
	_, roots := htfs.LoadCatalogs()
	for _, root := range roots {
		if strings.Contains(root.Blueprint, hash) {
			return root, root.Blueprint, nil
		}
	}
	return nil, "", fmt.Errorf("catalog with hash %q not found", hash)
}

func findCatalogFromRobot(robotFile string) (*htfs.Root, string, error) {
	_, holotreeBlueprint, err := htfs.ComposeFinalBlueprint(nil, robotFile, common.DevDependencies)
	if err != nil {
		return nil, "", fmt.Errorf("failed to calculate blueprint from robot.yaml: %w", err)
	}

	hash := common.BlueprintHash(holotreeBlueprint)
	catalogName := htfs.CatalogName(hash)

	// Load catalogs and find the matching one
	_, roots := htfs.LoadCatalogs()
	for _, root := range roots {
		if root.Blueprint == hash {
			return root, hash, nil
		}
	}

	return nil, "", fmt.Errorf("catalog %q not found - environment may not be built yet", catalogName)
}

func generateSBOM(root *htfs.Root, blueprintHash string, format sbom.FormatType) ([]byte, error) {
	library, err := htfs.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create library: %w", err)
	}

	generator := sbom.NewGenerator(library, blueprintHash, root.Platform)
	return generator.Generate(root, format)
}

var holotreeSbomCmd = &cobra.Command{
	Use:   "sbom [catalog]",
	Short: "Generate Software Bill of Materials (SBOM) from holotree catalog.",
	Long: `Generate Software Bill of Materials (SBOM) from holotree catalog.

The SBOM includes all conda and pip packages from the specified holotree environment.
Supported formats are CycloneDX and SPDX JSON.

Examples:
  # Generate SBOM for a catalog and output to file
  rcc holotree sbom abc123 --format cyclonedx --output sbom.json

  # Generate SBOM from a robot.yaml
  rcc holotree sbom --robot robot.yaml --format spdx

  # Generate and push SBOM to OCI registry
  rcc holotree sbom abc123 --registry ghcr.io/org/sboms --tag v1.0.0`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Holotree SBOM command lasted").Report()
		}

		var root *htfs.Root
		var blueprintHash string
		var err error

		// Determine the catalog source
		if sbomRobot != "" {
			root, blueprintHash, err = findCatalogFromRobot(sbomRobot)
			pretty.Guard(err == nil, 1, "Failed to find catalog from robot.yaml: %v", err)
		} else if len(args) > 0 {
			root, blueprintHash, err = findCatalogByHash(args[0])
			pretty.Guard(err == nil, 1, "Failed to find catalog: %v", err)
		} else {
			// List available catalogs
			catalogs := htfs.CatalogNames()
			if len(catalogs) == 0 {
				pretty.Guard(false, 1, "No catalogs available. Build an environment first.")
			}
			common.Log("Available catalogs (use hash as argument):")
			for _, catalog := range catalogs {
				common.Log("  - %s", catalog)
			}
			pretty.Ok()
			return
		}

		// Parse and validate format
		format, err := sbom.ParseFormat(sbomFormat)
		pretty.Guard(err == nil, 1, "%v", err)

		// Generate SBOM
		sbomData, err := generateSBOM(root, blueprintHash, format)
		pretty.Guard(err == nil, 2, "Failed to generate SBOM: %v", err)

		// Handle output
		result := make(map[string]interface{})
		result["blueprintHash"] = blueprintHash
		result["format"] = sbomFormat
		result["platform"] = root.Platform

		if sbomRegistry != "" {
			// Push to OCI registry
			tag := sbomTag
			if tag == "" {
				tag = blueprintHash
			}

			client := oci.NewClientFromEnv(sbomRegistry, tag)
			pushResult, err := client.Push(context.Background(), sbomData, sbom.GetMediaType(format))
			pretty.Guard(err == nil, 3, "Failed to push SBOM to registry: %v", err)

			result["pushed"] = true
			result["registry"] = pushResult.Registry
			result["tag"] = pushResult.Tag
			result["digest"] = pushResult.Digest

			if jsonFlag {
				nice, err := json.MarshalIndent(result, "", "  ")
				pretty.Guard(err == nil, 4, "%v", err)
				common.Stdout("%s\n", nice)
			} else {
				common.Log("SBOM pushed to %s:%s", pushResult.Registry, pushResult.Tag)
				common.Log("Digest: %s", pushResult.Digest)
			}
		} else if sbomOutput != "" {
			// Write to file
			outputPath := sbomOutput
			if !filepath.IsAbs(outputPath) {
				cwd, _ := os.Getwd()
				outputPath = filepath.Join(cwd, outputPath)
			}

			err = os.WriteFile(outputPath, sbomData, 0644)
			pretty.Guard(err == nil, 3, "Failed to write SBOM to file: %v", err)

			result["output"] = outputPath

			if jsonFlag {
				nice, err := json.MarshalIndent(result, "", "  ")
				pretty.Guard(err == nil, 4, "%v", err)
				common.Stdout("%s\n", nice)
			} else {
				common.Log("SBOM written to %s", outputPath)
			}
		} else {
			// Output to stdout
			if jsonFlag {
				// Wrap SBOM in metadata
				var sbomContent interface{}
				json.Unmarshal(sbomData, &sbomContent)
				result["sbom"] = sbomContent
				nice, err := json.MarshalIndent(result, "", "  ")
				pretty.Guard(err == nil, 4, "%v", err)
				common.Stdout("%s\n", nice)
			} else {
				common.Stdout("%s\n", sbomData)
			}
		}

		pretty.Ok()
	},
}

func init() {
	holotreeCmd.AddCommand(holotreeSbomCmd)
	holotreeSbomCmd.Flags().StringVarP(&sbomFormat, "format", "f", "cyclonedx", "SBOM format: cyclonedx or spdx")
	holotreeSbomCmd.Flags().StringVarP(&sbomOutput, "output", "o", "", "Output file path (writes to stdout if not specified)")
	holotreeSbomCmd.Flags().StringVarP(&sbomRegistry, "registry", "r", "", "OCI registry URL to push SBOM (optional)")
	holotreeSbomCmd.Flags().StringVarP(&sbomTag, "tag", "t", "", "Tag for the OCI artifact (defaults to blueprint hash)")
	holotreeSbomCmd.Flags().StringVar(&sbomRobot, "robot", "", "Path to robot.yaml to generate SBOM for its catalog")
	holotreeSbomCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output in JSON format")
	holotreeSbomCmd.Flags().BoolVarP(&common.DevDependencies, "devdeps", "", false, "Include dev-dependencies when using --robot")
}
