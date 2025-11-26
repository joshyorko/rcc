package sbom

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected FormatType
		wantErr  bool
	}{
		{"cyclonedx", FormatCycloneDX, false},
		{"CycloneDX", FormatCycloneDX, false},
		{"CYCLONEDX", FormatCycloneDX, false},
		{"spdx", FormatSPDX, false},
		{"SPDX", FormatSPDX, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("ParseFormat(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetMediaType(t *testing.T) {
	tests := []struct {
		format   FormatType
		expected string
	}{
		{FormatCycloneDX, CycloneDXMediaType},
		{FormatSPDX, SPDXMediaType},
		{"unknown", "application/json"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := GetMediaType(tt.format)
			if got != tt.expected {
				t.Errorf("GetMediaType(%q) = %v, want %v", tt.format, got, tt.expected)
			}
		})
	}
}

func TestCondaToPurl(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		channel  string
		subdir   string
		expected string
	}{
		{"numpy", "1.24.0", "conda-forge", "linux-64", "pkg:conda/conda-forge/numpy@1.24.0"},
		{"pandas", "2.0.0", "", "", "pkg:conda/pandas@2.0.0"},
		{"requests", "2.28.0", "https://conda.anaconda.org/conda-forge", "", "pkg:conda/conda-forge/requests@2.28.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := condaToPurl(tt.name, tt.version, tt.channel, tt.subdir)
			if got != tt.expected {
				t.Errorf("condaToPurl(%q, %q, %q, %q) = %q, want %q", tt.name, tt.version, tt.channel, tt.subdir, got, tt.expected)
			}
		})
	}
}

func TestPipToPurl(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{"requests", "2.28.0", "pkg:pypi/requests@2.28.0"},
		{"Flask", "2.0.0", "pkg:pypi/flask@2.0.0"},
		{"my-package", "1.0.0", "pkg:pypi/my-package@1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pipToPurl(tt.name, tt.version)
			if got != tt.expected {
				t.Errorf("pipToPurl(%q, %q) = %q, want %q", tt.name, tt.version, got, tt.expected)
			}
		})
	}
}

func TestParsePipMetadata(t *testing.T) {
	metadata := `Metadata-Version: 2.1
Name: requests
Version: 2.28.0
License: Apache 2.0
Author: Kenneth Reitz
Author-email: me@kennethreitz.org
Home-page: https://requests.readthedocs.io

Some description here.
`
	reader := strings.NewReader(metadata)
	meta, err := parsePipMetadata(reader)
	if err != nil {
		t.Fatalf("parsePipMetadata() error = %v", err)
	}

	if meta.Name != "requests" {
		t.Errorf("Name = %q, want %q", meta.Name, "requests")
	}
	if meta.Version != "2.28.0" {
		t.Errorf("Version = %q, want %q", meta.Version, "2.28.0")
	}
	if meta.License != "Apache 2.0" {
		t.Errorf("License = %q, want %q", meta.License, "Apache 2.0")
	}
	if meta.Author != "Kenneth Reitz" {
		t.Errorf("Author = %q, want %q", meta.Author, "Kenneth Reitz")
	}
}

func TestParsePipMetadataIncomplete(t *testing.T) {
	metadata := `Metadata-Version: 2.1
Name: incomplete
`
	reader := strings.NewReader(metadata)
	_, err := parsePipMetadata(reader)
	if err == nil {
		t.Error("parsePipMetadata() expected error for incomplete metadata")
	}
}

func TestSanitizeChannel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"conda-forge", "conda-forge"},
		{"https://conda.anaconda.org/conda-forge", "conda-forge"},
		{"https://conda.anaconda.org/conda-forge/", "conda-forge"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeChannel(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeChannel(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCycloneDXStructure(t *testing.T) {
	cdx := CycloneDX{
		BomFormat:   "CycloneDX",
		SpecVersion: "1.4",
		SerialNum:   "urn:uuid:test",
		Version:     1,
		Metadata: CycloneDXMetadata{
			Timestamp: "2024-01-01T00:00:00Z",
			Tools: []CycloneDXTool{
				{Vendor: "Robocorp", Name: "rcc", Version: "1.0.0"},
			},
		},
		Components: []CycloneDXComponent{
			{Type: "library", Name: "test", Version: "1.0.0"},
		},
	}

	data, err := json.Marshal(cdx)
	if err != nil {
		t.Fatalf("Failed to marshal CycloneDX: %v", err)
	}

	var parsed CycloneDX
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal CycloneDX: %v", err)
	}

	if parsed.BomFormat != "CycloneDX" {
		t.Errorf("BomFormat = %q, want %q", parsed.BomFormat, "CycloneDX")
	}
	if len(parsed.Components) != 1 {
		t.Errorf("Components length = %d, want 1", len(parsed.Components))
	}
}

func TestSPDXStructure(t *testing.T) {
	spdx := SPDX{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              "test-sbom",
		DocumentNamespace: "https://example.com/test",
		CreationInfo: SPDXCreationInfo{
			Created:  "2024-01-01T00:00:00Z",
			Creators: []string{"Tool: rcc-1.0.0"},
		},
		Packages: []SPDXPackage{
			{
				SPDXID:           "SPDXRef-Package-1",
				Name:             "test",
				VersionInfo:      "1.0.0",
				DownloadLocation: "NOASSERTION",
				FilesAnalyzed:    false,
				CopyrightText:    "NOASSERTION",
			},
		},
	}

	data, err := json.Marshal(spdx)
	if err != nil {
		t.Fatalf("Failed to marshal SPDX: %v", err)
	}

	var parsed SPDX
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SPDX: %v", err)
	}

	if parsed.SPDXVersion != "SPDX-2.3" {
		t.Errorf("SPDXVersion = %q, want %q", parsed.SPDXVersion, "SPDX-2.3")
	}
	if len(parsed.Packages) != 1 {
		t.Errorf("Packages length = %d, want 1", len(parsed.Packages))
	}
}
