package environmentartifact

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
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
const maxArchiveSize int64 = 256 << 20

// WriteArchive writes the canonical offline carrier. ZIP metadata is fixed so
// the same entries always produce the same bytes, regardless of filename or
// map iteration order.
func WriteArchive(w io.Writer, entries map[string][]byte) error {
	if w == nil {
		return fmt.Errorf("nil environment archive writer")
	}
	names, err := OrderedArchiveNames(entries)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	for _, name := range names {
		if int64(len(entries[name])) > maxArchiveMemberSize {
			return fmt.Errorf("environment archive member %q exceeds %d bytes", name, maxArchiveMemberSize)
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetModTime(time.Unix(0, 0).UTC())
		header.SetMode(0o644)
		part, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			return fmt.Errorf("create environment archive member %q: %w", name, err)
		}
		if _, err := part.Write(entries[name]); err != nil {
			_ = zw.Close()
			return fmt.Errorf("write environment archive member %q: %w", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close environment archive: %w", err)
	}
	return nil
}

// ReadArchive reads an archive with bounded memory and applies the same path
// and regular-file checks as ArchiveEntries.
func ReadArchive(content []byte) (map[string][]byte, error) {
	if int64(len(content)) > maxArchiveSize {
		return nil, fmt.Errorf("environment archive exceeds %d bytes", maxArchiveSize)
	}
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open environment archive: %w", err)
	}
	return ArchiveEntries(reader)
}

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
