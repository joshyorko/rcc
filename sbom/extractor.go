package sbom

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
)

// CondaPackageMeta represents the metadata from a conda package JSON file.
type CondaPackageMeta struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Channel      string   `json:"channel"`
	Subdir       string   `json:"subdir"`
	License      string   `json:"license,omitempty"`
	LicenseFamily string   `json:"license_family,omitempty"`
	Depends      []string `json:"depends,omitempty"`
}

// PipPackageMeta represents the metadata from a pip package.
type PipPackageMeta struct {
	Name     string
	Version  string
	License  string
	Author   string
	Homepage string
}

// extractCondaPackages extracts conda package metadata from conda-meta/*.json files.
func extractCondaPackages(library htfs.MutableLibrary, root *htfs.Root) ([]*Component, error) {
	components := make([]*Component, 0, 50)
	
	// Find conda-meta directory and process JSON files
	err := root.Treetop(func(path string, dir *htfs.Dir) error {
		return extractCondaFromDir(library, path, dir, &components)
	})
	if err != nil {
		return nil, err
	}
	
	return components, nil
}

// extractCondaFromDir recursively searches for conda-meta directory and extracts packages.
func extractCondaFromDir(library htfs.MutableLibrary, path string, dir *htfs.Dir, components *[]*Component) error {
	// Check if this is the conda-meta directory
	if filepath.Base(path) == "conda-meta" {
		for name, file := range dir.Files {
			if !strings.HasSuffix(name, ".json") || name == "history" {
				continue
			}
			
			reader, closer, err := library.Open(file.Digest)
			if err != nil {
				common.Debug("Failed to open conda meta file %s: %v", name, err)
				continue
			}
			
			content, err := io.ReadAll(reader)
			closer()
			if err != nil {
				common.Debug("Failed to read conda meta file %s: %v", name, err)
				continue
			}
			
			var meta CondaPackageMeta
			if err := json.Unmarshal(content, &meta); err != nil {
				common.Debug("Failed to parse conda meta file %s: %v", name, err)
				continue
			}
			
			component := &Component{
				Name:    meta.Name,
				Version: meta.Version,
				Type:    "library",
				Purl:    condaToPurl(meta.Name, meta.Version, meta.Channel, meta.Subdir),
				License: meta.License,
				Origin:  "conda",
			}
			if meta.Channel != "" {
				component.Supplier = meta.Channel
			}
			*components = append(*components, component)
		}
		return nil
	}
	
	// Recurse into subdirectories
	for name, subdir := range dir.Dirs {
		if err := extractCondaFromDir(library, filepath.Join(path, name), subdir, components); err != nil {
			return err
		}
	}
	
	return nil
}

// condaToPurl creates a Package URL for a conda package.
func condaToPurl(name, version, channel, subdir string) string {
	// Format: pkg:conda/channel/name@version?subdir=linux-64
	channel = sanitizeChannel(channel)
	purl := fmt.Sprintf("pkg:conda/%s@%s", url.PathEscape(name), url.PathEscape(version))
	if channel != "" {
		purl = fmt.Sprintf("pkg:conda/%s/%s@%s", url.PathEscape(channel), url.PathEscape(name), url.PathEscape(version))
	}
	return purl
}

// sanitizeChannel extracts channel name from full URL if necessary.
func sanitizeChannel(channel string) string {
	if channel == "" {
		return ""
	}
	// Handle URLs like https://conda.anaconda.org/conda-forge
	if strings.Contains(channel, "/") {
		parts := strings.Split(channel, "/")
		// Get the last non-empty part
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				return parts[i]
			}
		}
	}
	return channel
}

// extractPipPackages extracts pip package metadata from site-packages/*.dist-info/METADATA files.
func extractPipPackages(library htfs.MutableLibrary, root *htfs.Root) ([]*Component, error) {
	components := make([]*Component, 0, 50)
	
	err := root.Treetop(func(path string, dir *htfs.Dir) error {
		return extractPipFromDir(library, path, dir, &components)
	})
	if err != nil {
		return nil, err
	}
	
	return components, nil
}

// extractPipFromDir recursively searches for .dist-info directories and extracts METADATA.
func extractPipFromDir(library htfs.MutableLibrary, path string, dir *htfs.Dir, components *[]*Component) error {
	// Check if this is a site-packages directory with dist-info subdirs
	if strings.HasSuffix(filepath.Base(path), ".dist-info") {
		// Look for METADATA file
		for name, file := range dir.Files {
			if name != "METADATA" {
				continue
			}
			
			reader, closer, err := library.Open(file.Digest)
			if err != nil {
				common.Debug("Failed to open METADATA file in %s: %v", path, err)
				continue
			}
			
			meta, err := parsePipMetadata(reader)
			closer()
			if err != nil {
				common.Debug("Failed to parse METADATA file in %s: %v", path, err)
				continue
			}
			
			component := &Component{
				Name:     meta.Name,
				Version:  meta.Version,
				Type:     "library",
				Purl:     pipToPurl(meta.Name, meta.Version),
				License:  meta.License,
				Supplier: meta.Author,
				Origin:   "pip",
			}
			*components = append(*components, component)
		}
		return nil
	}
	
	// Recurse into subdirectories
	for name, subdir := range dir.Dirs {
		if err := extractPipFromDir(library, filepath.Join(path, name), subdir, components); err != nil {
			return err
		}
	}
	
	return nil
}

// parsePipMetadata parses a pip METADATA file.
func parsePipMetadata(reader io.Reader) (*PipPackageMeta, error) {
	meta := &PipPackageMeta{}
	scanner := bufio.NewScanner(reader)
	
	for scanner.Scan() {
		line := scanner.Text()
		
		// Empty line marks end of headers
		if line == "" {
			break
		}
		
		// Parse key: value format
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		
		key, value := parts[0], parts[1]
		switch key {
		case "Name":
			meta.Name = value
		case "Version":
			meta.Version = value
		case "License":
			meta.License = value
		case "Author":
			meta.Author = value
		case "Author-email":
			if meta.Author == "" {
				meta.Author = value
			}
		case "Home-page":
			meta.Homepage = value
		}
	}
	
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	
	if meta.Name == "" || meta.Version == "" {
		return nil, fmt.Errorf("incomplete metadata: name=%q, version=%q", meta.Name, meta.Version)
	}
	
	return meta, nil
}

// pipToPurl creates a Package URL for a pip package.
func pipToPurl(name, version string) string {
	// Format: pkg:pypi/name@version
	return fmt.Sprintf("pkg:pypi/%s@%s", url.PathEscape(strings.ToLower(name)), url.PathEscape(version))
}

// ExtractComponents extracts all software components from a holotree catalog.
func ExtractComponents(library htfs.MutableLibrary, root *htfs.Root) ([]*Component, error) {
	components := make([]*Component, 0, 100)
	
	// Extract conda packages
	condaComponents, err := extractCondaPackages(library, root)
	if err != nil {
		return nil, fmt.Errorf("failed to extract conda packages: %w", err)
	}
	components = append(components, condaComponents...)
	
	// Extract pip packages
	pipComponents, err := extractPipPackages(library, root)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pip packages: %w", err)
	}
	components = append(components, pipComponents...)
	
	return components, nil
}
