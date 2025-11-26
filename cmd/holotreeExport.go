package cmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	holozip           string
	exportRobot       string
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
	catalogPaths, roots := htfs.LoadCatalogs()
	rootByCatalog := make(map[string]*htfs.Root)
	for idx, catalogPath := range catalogPaths {
		if idx < len(roots) && roots[idx] != nil {
			rootByCatalog[filepath.Base(catalogPath)] = roots[idx]
		}
	}

	matchedRoots := make([]*htfs.Root, 0, len(catalogs))
	matchedCatalogs := make([]string, 0, len(catalogs))

	for _, catalog := range catalogs {
		base := filepath.Base(catalog)
		root := rootByCatalog[base]
		if root == nil {
			pretty.Warning("Catalog %s not found or failed to load, skipping SBOM generation", catalog)
			continue
		}
		matchedRoots = append(matchedRoots, root)
		matchedCatalogs = append(matchedCatalogs, base)
	}

	if len(matchedRoots) == 0 {
		pretty.Warning("No catalogs matched for SBOM generation")
		return
	}

	components := make([]*sbom.Component, 0, 100)
	seen := make(map[string]bool)

	for _, root := range matchedRoots {
		rootComponents, err := sbom.ExtractComponents(library, root)
		if err != nil {
			pretty.Warning("Failed to extract components for %s: %v", root.Blueprint, err)
			continue
		}
		for _, component := range rootComponents {
			key := component.Purl
			if key == "" {
				key = fmt.Sprintf("%s|%s|%s|%s", component.Origin, component.Type, component.Name, component.Version)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			components = append(components, component)
		}
	}

	if len(components) == 0 {
		pretty.Warning("No components collected for SBOM generation")
		return
	}

	combinedHash := blueprintHash
	if combinedHash == "" {
		if len(matchedRoots) == 1 {
			combinedHash = matchedRoots[0].Blueprint
		} else {
			hashes := make([]string, 0, len(matchedRoots))
			for _, root := range matchedRoots {
				hashes = append(hashes, root.Blueprint)
			}
			sort.Strings(hashes)
			sum := sha256.Sum256([]byte(strings.Join(hashes, "|")))
			combinedHash = fmt.Sprintf("%x", sum[:])
		}
	}

	platform := matchedRoots[0].Platform
	for _, root := range matchedRoots[1:] {
		if root.Platform != platform {
			platform = "multi"
			break
		}
	}

	generator := sbom.NewGenerator(library, combinedHash, platform)
	sbomData, err := generator.GenerateFromComponents(components, format)
	if err != nil {
		pretty.Warning("Failed to generate SBOM: %v", err)
		return
	}

	// Write SBOM alongside the zip file
	sbomFilename := strings.TrimSuffix(holozip, filepath.Ext(holozip)) + ".sbom.json"
	err = os.WriteFile(sbomFilename, sbomData, 0644)
	if err != nil {
		pretty.Warning("Failed to write SBOM to %s: %v", sbomFilename, err)
		return
	}

	common.Log("SBOM written to %s (catalogs: %s)", sbomFilename, strings.Join(matchedCatalogs, ", "))
}

func init() {
	holotreeCmd.AddCommand(holotreeExportCmd)
	holotreeExportCmd.Flags().StringVarP(&holozip, "zipfile", "z", "hololib.zip", "Name of zipfile to export.")
	holotreeExportCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output in JSON format")
	holotreeExportCmd.Flags().StringVarP(&exportRobot, "robot", "r", "", "Full path to 'robot.yaml' configuration file to export as catalog. <optional>")
	holotreeExportCmd.Flags().BoolVar(&exportIncludeSBOM, "include-sbom", false, "Include SBOM (Software Bill of Materials) in the export")
	holotreeExportCmd.Flags().StringVar(&exportSBOMFormat, "sbom-format", "cyclonedx", "SBOM format: cyclonedx or spdx (used with --include-sbom)")
}
