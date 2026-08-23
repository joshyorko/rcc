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
	ArchiveSpecificationDir = ArchiveRoot + "/specifications/"
	ArchiveBlueprintDir     = ArchiveRoot + "/legacy-blueprints/"
)

const maxArchiveMemberSize int64 = 16 << 20
const maxArchiveSize int64 = 256 << 20
const maxArchiveMembers = 4096
const maxArchiveCompressionRatio uint64 = 1000
const maxArchiveUncompressedSize uint64 = 256 << 20

// MaxArchiveSize is the largest encoded archive accepted by ReadArchive.
const MaxArchiveSize = maxArchiveSize

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
	var total uint64
	for _, name := range names {
		total += uint64(len(entries[name]))
	}
	if err := validateArchiveUncompressedBudget(len(names), total); err != nil {
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

// ValidateArchive verifies the complete content closure of a Manifest. It is
// deliberately independent of installation and does not execute any content.
func ValidateArchive(entries map[string][]byte) (Manifest, error) {
	manifestBytes, ok := entries[ArchiveManifest]
	if !ok {
		return Manifest{}, fmt.Errorf("environment archive is missing %s", ArchiveManifest)
	}
	manifest, err := DecodeManifest(manifestBytes)
	if err != nil {
		return Manifest{}, fmt.Errorf("decode environment manifest: %w", err)
	}
	indexBytes, ok := entries[ArchiveObjectIndex]
	if !ok {
		return Manifest{}, fmt.Errorf("environment archive is missing %s", ArchiveObjectIndex)
	}
	if err := VerifyDescriptor(manifest.ObjectIndex, indexBytes); err != nil {
		return Manifest{}, fmt.Errorf("verify object index: %w", err)
	}
	index, err := DecodeObjectIndex(indexBytes)
	if err != nil {
		return Manifest{}, fmt.Errorf("decode object index: %w", err)
	}
	for _, content := range []struct {
		name string
		desc Descriptor
	}{
		{ArchiveSpecificationDir + manifest.Specification.Digest.Hex(), manifest.Specification.Descriptor},
		{ArchiveBlueprintDir + manifest.LegacyBlueprint.Digest.Hex(), manifest.LegacyBlueprint.Descriptor},
		{ArchiveCatalogDirectory + manifest.Catalogs[0].Digest.Hex(), manifest.Catalogs[0].Descriptor},
	} {
		value, found := entries[content.name]
		if !found {
			return Manifest{}, fmt.Errorf("environment archive is missing %s", content.name)
		}
		if err := VerifyDescriptor(content.desc, value); err != nil {
			return Manifest{}, fmt.Errorf("verify %s: %w", content.name, err)
		}
	}
	for _, object := range index.Entries {
		name := ArchiveObjectDirectory + object.StoredDigest.Hex()
		value, found := entries[name]
		if !found {
			return Manifest{}, fmt.Errorf("environment archive is missing %s", name)
		}
		if err := VerifyDescriptor(Descriptor{Digest: object.StoredDigest, Size: object.StoredSize}, value); err != nil {
			return Manifest{}, fmt.Errorf("verify object %s: %w", object.LegacyObjectID, err)
		}
	}
	return manifest, nil
}

// ArchiveEntries returns the regular-file entries in an offline archive. ZIP
// metadata is not part of the returned content or of Manifest identity.
func ArchiveEntries(r *zip.Reader) (map[string][]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("nil environment archive")
	}
	entries := make(map[string][]byte, len(r.File))
	var uncompressedSize uint64
	if err := validateArchiveUncompressedBudget(len(r.File), 0); err != nil {
		return nil, err
	}
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
		if file.UncompressedSize64 > maxArchiveUncompressedSize-uncompressedSize {
			return nil, fmt.Errorf("environment archive cumulative uncompressed size exceeds %d bytes", maxArchiveUncompressedSize)
		}
		uncompressedSize += file.UncompressedSize64
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > maxArchiveCompressionRatio {
			return nil, fmt.Errorf("environment archive member %q has an unsafe compression ratio", file.Name)
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

func validateArchiveUncompressedBudget(memberCount int, total uint64) error {
	if memberCount > maxArchiveMembers {
		return fmt.Errorf("environment archive contains too many members (maximum %d)", maxArchiveMembers)
	}
	if total > maxArchiveUncompressedSize {
		return fmt.Errorf("environment archive cumulative uncompressed size exceeds %d bytes", maxArchiveUncompressedSize)
	}
	return nil
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
