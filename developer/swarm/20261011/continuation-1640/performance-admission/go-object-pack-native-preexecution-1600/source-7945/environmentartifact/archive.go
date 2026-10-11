package environmentartifact

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	ArchiveRoot             = "rcc-environment"
	ArchiveManifest         = ArchiveRoot + "/manifest.json"
	ArchiveObjectIndex      = ArchiveRoot + "/object-index.json"
	ArchivePlatformIndex    = ArchiveRoot + "/platform-index.json"
	ArchiveCatalogDirectory = ArchiveRoot + "/catalogs/"
	ArchiveObjectDirectory  = ArchiveRoot + "/objects/"
	ArchiveAttestationDir   = ArchiveRoot + "/attestations/"
	ArchiveSpecificationDir = ArchiveRoot + "/specifications/"
	ArchiveBlueprintDir     = ArchiveRoot + "/legacy-blueprints/"
)

const maxArchiveSize int64 = 256 << 20
const maxArchiveMemberSize int64 = maxArchiveSize
const maxArchiveMembers = 32768
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
		if err := validateArchiveMemberSize(name, uint64(len(entries[name]))); err != nil {
			return err
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.Modified = time.Unix(0, 0).UTC()
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

// ReadArchiveFile opens an archive directly from disk. The ZIP directory is
// read from the file and individual members remain bounded by ArchiveEntries;
// the encoded archive is never copied into one whole-archive []byte.
func ReadArchiveFile(filePath string) (map[string][]byte, error) {
	archive, err := OpenArchiveFile(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archive.Close() }()
	return archive.Entries()
}

// ArchiveReader keeps only the ZIP directory resident. Members are opened and
// consumed independently, so large object closures never become one map of
// byte slices in memory.
type ArchiveReader struct {
	file  *os.File
	files map[string]*zip.File
}

func OpenArchiveFile(filePath string) (*ArchiveReader, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open environment archive: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat environment archive: %w", err)
	}
	if info.Size() > maxArchiveSize {
		_ = file.Close()
		return nil, fmt.Errorf("environment archive exceeds %d bytes", maxArchiveSize)
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open environment archive: %w", err)
	}
	files, err := validateArchiveFiles(reader.File)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &ArchiveReader{file: file, files: files}, nil
}

func (a *ArchiveReader) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	err := a.file.Close()
	a.file = nil
	return err
}

func (a *ArchiveReader) HasMember(name string) bool {
	_, ok := a.files[name]
	return ok
}

func (a *ArchiveReader) MemberSize(name string) (int64, bool) {
	file, ok := a.files[name]
	if !ok {
		return 0, false
	}
	return int64(file.UncompressedSize64), true
}

func (a *ArchiveReader) OpenMember(name string) (io.ReadCloser, error) {
	file, ok := a.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return file.Open()
}

func (a *ArchiveReader) ReadMember(name string) ([]byte, error) {
	reader, err := a.OpenMember(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	content, err := io.ReadAll(io.LimitReader(reader, maxArchiveMemberSize+1))
	if err != nil {
		return nil, err
	}
	if err := validateArchiveMemberSize(name, uint64(len(content))); err != nil {
		return nil, err
	}
	return content, nil
}

func (a *ArchiveReader) Entries() (map[string][]byte, error) {
	if !a.HasMember(ArchiveManifest) {
		return nil, fmt.Errorf("environment archive is missing %s", ArchiveManifest)
	}
	if !a.HasMember(ArchiveObjectIndex) {
		return nil, fmt.Errorf("environment archive is missing %s", ArchiveObjectIndex)
	}
	entries := make(map[string][]byte, len(a.files))
	for name := range a.files {
		content, err := a.ReadMember(name)
		if err != nil {
			return nil, fmt.Errorf("read environment archive member %q: %w", name, err)
		}
		entries[name] = content
	}
	return entries, nil
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
	if platformIndexBytes, found := entries[ArchivePlatformIndex]; found {
		platformIndex, indexErr := DecodePlatformIndex(platformIndexBytes)
		if indexErr != nil {
			return Manifest{}, fmt.Errorf("decode platform index: %w", indexErr)
		}
		if platformIndex.Specification != manifest.Specification.Digest {
			return Manifest{}, fmt.Errorf("platform index specification does not match manifest")
		}
		selected, selectErr := platformIndex.Select(CurrentPlatform())
		if selectErr != nil {
			return Manifest{}, fmt.Errorf("select current platform artifact: %w", selectErr)
		}
		if selected != manifest.ArtifactDigest {
			return Manifest{}, fmt.Errorf("platform index selected artifact %s, archive contains %s", selected, manifest.ArtifactDigest)
		}
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
	files, err := validateArchiveFiles(r.File)
	if err != nil {
		return nil, err
	}
	entries := make(map[string][]byte, len(files))
	for name, file := range files {
		reader, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open environment archive member %q: %w", file.Name, err)
		}
		content, err := io.ReadAll(io.LimitReader(reader, maxArchiveMemberSize+1))
		closeErr := reader.Close()
		if err != nil {
			return nil, fmt.Errorf("read environment archive member %q: %w", file.Name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close environment archive member %q: %w", file.Name, closeErr)
		}
		if err := validateArchiveMemberSize(file.Name, uint64(len(content))); err != nil {
			return nil, err
		}
		entries[name] = content
	}
	if _, ok := entries[ArchiveManifest]; !ok {
		return nil, fmt.Errorf("environment archive is missing %s", ArchiveManifest)
	}
	if _, ok := entries[ArchiveObjectIndex]; !ok {
		return nil, fmt.Errorf("environment archive is missing %s", ArchiveObjectIndex)
	}
	return entries, nil
}

func validateArchiveFiles(files []*zip.File) (map[string]*zip.File, error) {
	if err := validateArchiveUncompressedBudget(len(files), 0); err != nil {
		return nil, err
	}
	validated := make(map[string]*zip.File, len(files))
	var uncompressedSize uint64
	for _, file := range files {
		if err := validateArchivePath(file.Name); err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() || file.Mode()&0o170000 == 0o120000 {
			return nil, fmt.Errorf("environment archive contains non-regular entry %q", file.Name)
		}
		if err := validateArchiveMemberSize(file.Name, file.UncompressedSize64); err != nil {
			return nil, err
		}
		if file.UncompressedSize64 > maxArchiveUncompressedSize-uncompressedSize {
			return nil, fmt.Errorf("environment archive cumulative uncompressed size exceeds %d bytes", maxArchiveUncompressedSize)
		}
		uncompressedSize += file.UncompressedSize64
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > maxArchiveCompressionRatio {
			return nil, fmt.Errorf("environment archive member %q has an unsafe compression ratio", file.Name)
		}
		if _, exists := validated[file.Name]; exists {
			return nil, fmt.Errorf("environment archive contains duplicate entry %q", file.Name)
		}
		validated[file.Name] = file
	}
	return validated, nil
}

func validateArchiveMemberSize(name string, size uint64) error {
	if size > uint64(maxArchiveMemberSize) {
		return fmt.Errorf("environment archive member %q exceeds %d bytes", name, maxArchiveMemberSize)
	}
	return nil
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
