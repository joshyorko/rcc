package sbom

import (
	"time"
)

// Component represents a software component in the SBOM.
type Component struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Type     string `json:"type"`     // "library" or "application"
	Purl     string `json:"purl"`     // Package URL
	License  string `json:"license"`  // License identifier
	Supplier string `json:"supplier"` // Supplier/author
	Origin   string `json:"origin"`   // "conda" or "pip"
}

// SBOM represents the common data structure for an SBOM.
type SBOM struct {
	Format        string       `json:"format"`        // "cyclonedx" or "spdx"
	ToolName      string       `json:"toolName"`      // "rcc"
	ToolVersion   string       `json:"toolVersion"`   // rcc version
	BlueprintHash string       `json:"blueprintHash"` // Unique identifier
	Platform      string       `json:"platform"`      // Operating system/architecture
	CreatedAt     time.Time    `json:"createdAt"`
	Components    []*Component `json:"components"`
}

// CycloneDXLicense represents a license in CycloneDX format.
type CycloneDXLicense struct {
	License struct {
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"license"`
}

// CycloneDXComponent represents a component in CycloneDX format.
type CycloneDXComponent struct {
	Type     string             `json:"type"`
	Name     string             `json:"name"`
	Version  string             `json:"version"`
	Purl     string             `json:"purl,omitempty"`
	Licenses []CycloneDXLicense `json:"licenses,omitempty"`
	Supplier *struct {
		Name string `json:"name,omitempty"`
	} `json:"supplier,omitempty"`
}

// CycloneDXTool represents a tool in CycloneDX format.
type CycloneDXTool struct {
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// CycloneDXMetadata represents metadata in CycloneDX format.
type CycloneDXMetadata struct {
	Timestamp string          `json:"timestamp"`
	Tools     []CycloneDXTool `json:"tools"`
	Component *struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"component,omitempty"`
}

// CycloneDX represents the complete CycloneDX SBOM document.
type CycloneDX struct {
	BomFormat   string               `json:"bomFormat"`
	SpecVersion string               `json:"specVersion"`
	SerialNum   string               `json:"serialNumber"`
	Version     int                  `json:"version"`
	Metadata    CycloneDXMetadata    `json:"metadata"`
	Components  []CycloneDXComponent `json:"components"`
}

// SPDXCreationInfo represents creation info in SPDX format.
type SPDXCreationInfo struct {
	Created            string   `json:"created"`
	Creators           []string `json:"creators"`
	LicenseListVersion string   `json:"licenseListVersion,omitempty"`
}

// SPDXPackage represents a package in SPDX format.
type SPDXPackage struct {
	SPDXID                   string `json:"SPDXID"`
	Name                     string `json:"name"`
	VersionInfo              string `json:"versionInfo"`
	DownloadLocation         string `json:"downloadLocation"`
	FilesAnalyzed            bool   `json:"filesAnalyzed"`
	LicenseConcluded         string `json:"licenseConcluded,omitempty"`
	LicenseDeclared          string `json:"licenseDeclared,omitempty"`
	CopyrightText            string `json:"copyrightText"`
	ExternalRefs             []SPDXExternalRef `json:"externalRefs,omitempty"`
	PrimaryPackagePurpose    string `json:"primaryPackagePurpose,omitempty"`
	Supplier                 string `json:"supplier,omitempty"`
}

// SPDXExternalRef represents an external reference in SPDX format.
type SPDXExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

// SPDXRelationship represents a relationship between packages in SPDX.
type SPDXRelationship struct {
	SpdxElementId      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSpdxElement string `json:"relatedSpdxElement"`
}

// SPDX represents the complete SPDX SBOM document.
type SPDX struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      SPDXCreationInfo   `json:"creationInfo"`
	Packages          []SPDXPackage      `json:"packages"`
	Relationships     []SPDXRelationship `json:"relationships,omitempty"`
}
