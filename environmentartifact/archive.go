package environmentartifact

import (
	"archive/zip"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

const (
	ArchiveRoot             = "rcc-environment"
	ArchiveManifest         = ArchiveRoot + "/manifest.json"
	ArchiveObjectIndex      = ArchiveRoot + "/object-index.json"
	ArchiveCatalogDirectory = ArchiveRoot + "/catalogs/"
	ArchiveObjectDirectory  = ArchiveRoot + "/objects/"
	ArchiveAttestationDir   = ArchiveRoot + "/attestations/"
)

const maxArchiveMemberSize int64 = 16 << 20

// ArchiveEntries returns the regular-file entries in an offline archive. ZIP
// metadata is not part of the returned content or of Manifest identity.
func ArchiveEntries(r *zip.Reader) (map[string][]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("nil environment archive")
	}
	entries := make(map[string][]byte, len(r.File))
	for _, file := range r.File {
		if err := validateArchivePath(file.Name); err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() || file.Mode()&0o170000 == 0o120000 {
			return nil, fmt.Errorf("environment archive contains non-regular entry %q", file.Name)
		}
		if file.UncompressedSize64 > uint64(maxArchiveMemberSize) {
			return nil, fmt.Errorf("environment archive member %q exceeds %d bytes", file.Name, maxArchiveMemberSize)
		}
		if _, exists := entries[file.Name]; exists {
			return nil, fmt.Errorf("environment archive contains duplicate entry %q", file.Name)
		}
		reader, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open environment archive member %q: %w", file.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(reader, maxArchiveMemberSize+1))
		closeErr := reader.Close()
		if err != nil {
			return nil, fmt.Errorf("read environment archive member %q: %w", file.Name, err)
		}
		if closeErr != nil || int64(len(content)) > maxArchiveMemberSize {
			return nil, fmt.Errorf("environment archive member %q exceeds %d bytes", file.Name, maxArchiveMemberSize)
		}
		entries[file.Name] = content
	}
	if _, ok := entries[ArchiveManifest]; !ok {
		return nil, fmt.Errorf("environment archive is missing %s", ArchiveManifest)
	}
	if _, ok := entries[ArchiveObjectIndex]; !ok {
		return nil, fmt.Errorf("environment archive is missing %s", ArchiveObjectIndex)
	}
	return entries, nil
}

func validateArchivePath(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || path.Clean(name) != name || strings.HasPrefix(name, "../") || name == ".." {
		return fmt.Errorf("unsafe environment archive path %q", name)
	}
	if !strings.HasPrefix(name, ArchiveRoot+"/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("environment archive path outside %s: %q", ArchiveRoot, name)
	}
	return nil
}

// OrderedArchiveNames provides the canonical member order for archive writers.
func OrderedArchiveNames(entries map[string][]byte) ([]string, error) {
	names := make([]string, 0, len(entries))
	for name := range entries {
		if err := validateArchivePath(name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
