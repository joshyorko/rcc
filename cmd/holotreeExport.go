package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/sbom"
	"github.com/spf13/cobra"
)

var (
	holozip          string
	exportRobot      string
	exportIncludeSBOM bool
	exportSBOMFormat  string
)

func holotreeExport(catalogs, known []string, archive string) {
	common.Debug("Ignoring content from catalogs:")
	for _, catalog := range known {
		common.Debug("- %s", catalog)
	}

	common.Debug("Exporting catalogs:")
	for _, catalog := range catalogs {
		common.Debug("- %s", catalog)
	}

	tree, err := htfs.New()
	pretty.Guard(err == nil, 2, "%s", err)

	err = tree.Export(catalogs, known, archive)
	pretty.Guard(err == nil, 3, "%s", err)
}

func listCatalogs(jsonForm bool) {
	if jsonForm {
		nice, err := json.MarshalIndent(htfs.CatalogNames(), "", "  ")
		pretty.Guard(err == nil, 2, "%s", err)
		common.Stdout("%s\n", nice)
	} else {
		common.Log("Selectable catalogs (you can use substrings):")
		for _, catalog := range htfs.CatalogNames() {
			common.Log("- %s", catalog)
		}
	}
}

func selectCatalogs(filters []string) []string {
	result := make([]string, 0, len(filters))
	for _, catalog := range htfs.CatalogNames() {
		for _, filter := range filters {
			if strings.Contains(catalog, filter) {
				result = append(result, catalog)
				break
			}
		}
	}
	sort.Strings(result)
	return result
}

var holotreeExportCmd = &cobra.Command{
	Use:   "export catalog+",
	Short: "Export existing holotree catalog and library parts.",
	Long:  "Export existing holotree catalog and library parts.",
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Holotree export command lasted").Report()
		}
		var blueprintHash string
		if len(exportRobot) > 0 {
			devDependencies := false
			_, holotreeBlueprint, err := htfs.ComposeFinalBlueprint(nil, exportRobot, devDependencies)
			pretty.Guard(err == nil, 1, "Blueprint calculation failed: %v", err)
			blueprintHash = common.BlueprintHash(holotreeBlueprint)
			args = append(args, htfs.CatalogName(blueprintHash))
		}
		if len(args) == 0 {
			listCatalogs(jsonFlag)
		} else {
			selectedCatalogs := selectCatalogs(args)
			holotreeExport(selectedCatalogs, nil, holozip)
			
			// Generate SBOM if requested
			if exportIncludeSBOM && len(selectedCatalogs) > 0 {
				generateExportSBOM(selectedCatalogs, blueprintHash)
			}
		}
		pretty.Ok()
	},
}

func generateExportSBOM(catalogs []string, blueprintHash string) {
	format, err := sbom.ParseFormat(exportSBOMFormat)
	if err != nil {
		pretty.Warning("Invalid SBOM format %q: %v", exportSBOMFormat, err)
		return
	}

	library, err := htfs.New()
	if err != nil {
		pretty.Warning("Failed to create library for SBOM: %v", err)
		return
	}

	// Load catalogs and find matching ones
	_, roots := htfs.LoadCatalogs()
	for _, catalog := range catalogs {
		for _, root := range roots {
			// Extract blueprint hash from catalog name for exact matching
			// Catalog names have format: <hash>v12.<platform>
			catalogBlueprint := strings.Split(filepath.Base(catalog), "v12.")[0]
			if catalogBlueprint != root.Blueprint {
				continue
			}

			hash := root.Blueprint
			if blueprintHash != "" {
				hash = blueprintHash
			}

			generator := sbom.NewGenerator(library, hash, root.Platform)
			sbomData, err := generator.Generate(root, format)
			if err != nil {
				pretty.Warning("Failed to generate SBOM for %s: %v", catalog, err)
				continue
			}

			// Write SBOM alongside the zip file
			sbomFilename := strings.TrimSuffix(holozip, filepath.Ext(holozip)) + ".sbom.json"
			err = os.WriteFile(sbomFilename, sbomData, 0644)
			if err != nil {
				pretty.Warning("Failed to write SBOM to %s: %v", sbomFilename, err)
				continue
			}

			common.Log("SBOM written to %s", sbomFilename)
			break // Only generate one SBOM per export
		}
	}
}

func init() {
	holotreeCmd.AddCommand(holotreeExportCmd)
	holotreeExportCmd.Flags().StringVarP(&holozip, "zipfile", "z", "hololib.zip", "Name of zipfile to export.")
	holotreeExportCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output in JSON format")
	holotreeExportCmd.Flags().StringVarP(&exportRobot, "robot", "r", "", "Full path to 'robot.yaml' configuration file to export as catalog. <optional>")
	holotreeExportCmd.Flags().BoolVar(&exportIncludeSBOM, "include-sbom", false, "Include SBOM (Software Bill of Materials) in the export")
	holotreeExportCmd.Flags().StringVar(&exportSBOMFormat, "sbom-format", "cyclonedx", "SBOM format: cyclonedx or spdx (used with --include-sbom)")
}
