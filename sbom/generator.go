package sbom

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
)

// FormatType represents the SBOM format type.
type FormatType string

const (
	// FormatCycloneDX represents the CycloneDX SBOM format.
	FormatCycloneDX FormatType = "cyclonedx"
	// FormatSPDX represents the SPDX SBOM format.
	FormatSPDX FormatType = "spdx"
)

// CycloneDXMediaType is the media type for CycloneDX JSON format.
const CycloneDXMediaType = "application/vnd.cyclonedx+json"

// SPDXMediaType is the media type for SPDX JSON format.
const SPDXMediaType = "application/spdx+json"

// Generator generates SBOMs from holotree catalogs.
type Generator struct {
	library       htfs.MutableLibrary
	blueprintHash string
	platform      string
}

// NewGenerator creates a new SBOM generator.
func NewGenerator(library htfs.MutableLibrary, blueprintHash, platform string) *Generator {
	return &Generator{
		library:       library,
		blueprintHash: blueprintHash,
		platform:      platform,
	}
}

// Generate generates an SBOM from the given catalog root.
func (g *Generator) Generate(root *htfs.Root, format FormatType) ([]byte, error) {
	components, err := ExtractComponents(g.library, root)
	if err != nil {
		return nil, fmt.Errorf("failed to extract components: %w", err)
	}

	sbom := &SBOM{
		Format:        string(format),
		ToolName:      "rcc",
		ToolVersion:   common.Version,
		BlueprintHash: g.blueprintHash,
		Platform:      g.platform,
		CreatedAt:     time.Now().UTC(),
		Components:    components,
	}

	switch format {
	case FormatCycloneDX:
		return g.generateCycloneDX(sbom)
	case FormatSPDX:
		return g.generateSPDX(sbom)
	default:
		return nil, fmt.Errorf("unsupported SBOM format: %s", format)
	}
}

// generateCycloneDX generates a CycloneDX format SBOM.
func (g *Generator) generateCycloneDX(sbom *SBOM) ([]byte, error) {
	cdx := CycloneDX{
		BomFormat:   "CycloneDX",
		SpecVersion: "1.4",
		SerialNum:   fmt.Sprintf("urn:uuid:%s", g.blueprintHash),
		Version:     1,
		Metadata: CycloneDXMetadata{
			Timestamp: sbom.CreatedAt.Format(time.RFC3339),
			Tools: []CycloneDXTool{
				{
					Vendor:  "Robocorp",
					Name:    sbom.ToolName,
					Version: sbom.ToolVersion,
				},
			},
		},
		Components: make([]CycloneDXComponent, 0, len(sbom.Components)),
	}

	for _, comp := range sbom.Components {
		cdxComp := CycloneDXComponent{
			Type:    comp.Type,
			Name:    comp.Name,
			Version: comp.Version,
			Purl:    comp.Purl,
		}

		if comp.License != "" {
			cdxComp.Licenses = []CycloneDXLicense{
				{
					License: struct {
						ID   string `json:"id,omitempty"`
						Name string `json:"name,omitempty"`
					}{
						Name: comp.License,
					},
				},
			}
		}

		if comp.Supplier != "" {
			cdxComp.Supplier = &struct {
				Name string `json:"name,omitempty"`
			}{
				Name: comp.Supplier,
			}
		}

		cdx.Components = append(cdx.Components, cdxComp)
	}

	return json.MarshalIndent(cdx, "", "  ")
}

// generateSPDX generates an SPDX format SBOM.
func (g *Generator) generateSPDX(sbom *SBOM) ([]byte, error) {
	spdx := SPDX{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              fmt.Sprintf("holotree-sbom-%s", g.blueprintHash),
		DocumentNamespace: fmt.Sprintf("https://rcc.robocorp.com/spdx/%s", g.blueprintHash),
		CreationInfo: SPDXCreationInfo{
			Created: sbom.CreatedAt.Format(time.RFC3339),
			Creators: []string{
				fmt.Sprintf("Tool: %s-%s", sbom.ToolName, sbom.ToolVersion),
			},
		},
		Packages:      make([]SPDXPackage, 0, len(sbom.Components)),
		Relationships: make([]SPDXRelationship, 0, len(sbom.Components)),
	}

	for i, comp := range sbom.Components {
		spdxID := fmt.Sprintf("SPDXRef-Package-%d", i+1)

		pkg := SPDXPackage{
			SPDXID:            spdxID,
			Name:              comp.Name,
			VersionInfo:       comp.Version,
			DownloadLocation:  "NOASSERTION",
			FilesAnalyzed:     false,
			CopyrightText:     "NOASSERTION",
			PrimaryPackagePurpose: "LIBRARY",
		}

		if comp.License != "" {
			pkg.LicenseConcluded = comp.License
			pkg.LicenseDeclared = comp.License
		} else {
			pkg.LicenseConcluded = "NOASSERTION"
			pkg.LicenseDeclared = "NOASSERTION"
		}

		if comp.Supplier != "" {
			pkg.Supplier = fmt.Sprintf("Organization: %s", comp.Supplier)
		}

		if comp.Purl != "" {
			pkg.ExternalRefs = []SPDXExternalRef{
				{
					ReferenceCategory: "PACKAGE-MANAGER",
					ReferenceType:     "purl",
					ReferenceLocator:  comp.Purl,
				},
			}
		}

		spdx.Packages = append(spdx.Packages, pkg)

		// Add relationship: DOCUMENT DESCRIBES PACKAGE
		spdx.Relationships = append(spdx.Relationships, SPDXRelationship{
			SpdxElementId:      "SPDXRef-DOCUMENT",
			RelationshipType:   "DESCRIBES",
			RelatedSpdxElement: spdxID,
		})
	}

	return json.MarshalIndent(spdx, "", "  ")
}

// GetMediaType returns the appropriate media type for the given format.
func GetMediaType(format FormatType) string {
	switch format {
	case FormatCycloneDX:
		return CycloneDXMediaType
	case FormatSPDX:
		return SPDXMediaType
	default:
		return "application/json"
	}
}

// ParseFormat parses a format string into a FormatType.
func ParseFormat(format string) (FormatType, error) {
	switch format {
	case "cyclonedx", "CycloneDX", "CYCLONEDX":
		return FormatCycloneDX, nil
	case "spdx", "SPDX":
		return FormatSPDX, nil
	default:
		return "", fmt.Errorf("unsupported SBOM format: %s (supported: cyclonedx, spdx)", format)
	}
}
