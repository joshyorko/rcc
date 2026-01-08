package htfs

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/fail"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/klauspost/compress/zstd"
)

// ArchiveManifest represents metadata about files in the archive
type ArchiveManifest struct {
	Version string                  `json:"version"`
	Files   map[string]*ArchiveFile `json:"files"`
}

// ArchiveFile represents metadata for a single file in the archive
type ArchiveFile struct {
	Digest  string      `json:"digest"`
	Size    int64       `json:"size"`
	Mode    os.FileMode `json:"mode"`
	Rewrite []int64     `json:"rewrite,omitempty"`
}

// archiveBasePath returns the base path for archives
func archiveBasePath() string {
	return filepath.Join(common.HololibLocation(), "archives")
}

// ArchivePath returns the full path to an archive for a given blueprint
func ArchivePath(blueprint string) string {
	return filepath.Join(archiveBasePath(), fmt.Sprintf("%s.tar.zst", blueprint))
}

// ArchiveExists checks if an archive exists for a blueprint
func ArchiveExists(blueprint string) bool {
	return pathlib.IsFile(ArchivePath(blueprint))
}

// CreateArchive creates a tar.zst archive from files in the library
// The archive structure is:
//
//	archive.tar.zst
//	├── manifest.json     # File metadata
//	└── files/
//	    └── <digest>      # Uncompressed file content
//
// Archives are created atomically using temp file + rename to prevent
// corruption if the process crashes mid-write.
func CreateArchive(archivePath string, files map[string]*File, library Library) (err error) {
	defer fail.Around(&err)

	common.Timeline("creating archive at %s", archivePath)

	// Ensure archive directory exists
	archiveDir := filepath.Dir(archivePath)
	err = os.MkdirAll(archiveDir, 0o755)
	fail.On(err != nil, "Failed to create archive directory %q -> %v", archiveDir, err)

	// Create archive in temp file for atomic write
	tempPath := archivePath + ".tmp"
	archiveFile, err := os.Create(tempPath)
	fail.On(err != nil, "Failed to create temp archive file %q -> %v", tempPath, err)

	// Cleanup temp file on error
	success := false
	defer func() {
		archiveFile.Close()
		if !success {
			os.Remove(tempPath)
		}
	}()

	// Create zstd writer
	// Note: writers are closed explicitly before atomic rename, not via defer
	zw, err := zstd.NewWriter(archiveFile, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	fail.On(err != nil, "Failed to create zstd writer -> %v", err)

	// Create tar writer
	tw := tar.NewWriter(zw)

	// Write version marker as first entry for future compatibility.
	// This allows detection of archive format version without parsing the manifest.
	versionMarker := []byte("RCCARCHIVE/1.0\n")
	versionHeader := &tar.Header{
		Name:    "RCCARCHIVE",
		Mode:    0o644,
		Size:    int64(len(versionMarker)),
		ModTime: motherTime,
	}
	err = tw.WriteHeader(versionHeader)
	fail.On(err != nil, "Failed to write version marker header -> %v", err)
	_, err = tw.Write(versionMarker)
	fail.On(err != nil, "Failed to write version marker -> %v", err)

	// Build manifest
	manifest := &ArchiveManifest{
		Version: "1",
		Files:   make(map[string]*ArchiveFile),
	}

	// Track unique digests to avoid duplicates
	writtenDigests := make(map[string]bool)

	// Write each file to the archive
	for path, file := range files {
		// Skip symlinks (they don't need file content)
		if file.IsSymlink() {
			continue
		}

		digest := file.Digest
		if digest == "" || digest == "N/A" {
			common.Timeline("skipping file with no digest: %s", path)
			continue
		}

		// Add to manifest
		manifest.Files[path] = &ArchiveFile{
			Digest:  digest,
			Size:    file.Size,
			Mode:    file.Mode,
			Rewrite: file.Rewrite,
		}

		// Skip if already written
		if writtenDigests[digest] {
			continue
		}
		writtenDigests[digest] = true

		// Open source file from library
		reader, closer, err := library.Open(digest)
		if err != nil {
			common.Timeline("warning: failed to open digest %s -> %v", digest, err)
			continue
		}

		// Write tar header for the file
		tarPath := filepath.Join("files", digest)
		header := &tar.Header{
			Name:    tarPath,
			Mode:    int64(file.Mode),
			Size:    file.Size,
			ModTime: motherTime,
		}
		err = tw.WriteHeader(header)
		if err != nil {
			closer()
			fail.On(true, "Failed to write tar header for %s -> %v", digest, err)
		}

		// Copy file content to archive
		buf := GetCopyBuffer()
		_, err = io.CopyBuffer(tw, reader, *buf)
		PutCopyBuffer(buf)
		closer()
		fail.On(err != nil, "Failed to copy file %s to archive -> %v", digest, err)
	}

	// Write manifest.json
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	fail.On(err != nil, "Failed to marshal manifest -> %v", err)

	manifestHeader := &tar.Header{
		Name:    "manifest.json",
		Mode:    0o644,
		Size:    int64(len(manifestData)),
		ModTime: motherTime,
	}
	err = tw.WriteHeader(manifestHeader)
	fail.On(err != nil, "Failed to write manifest header -> %v", err)

	_, err = tw.Write(manifestData)
	fail.On(err != nil, "Failed to write manifest data -> %v", err)

	// Close writers in order before rename
	err = tw.Close()
	fail.On(err != nil, "Failed to close tar writer -> %v", err)
	err = zw.Close()
	fail.On(err != nil, "Failed to close zstd writer -> %v", err)
	err = archiveFile.Close()
	fail.On(err != nil, "Failed to close archive file -> %v", err)

	// Atomic rename to final location
	err = os.Rename(tempPath, archivePath)
	fail.On(err != nil, "Failed to rename temp archive to final location -> %v", err)

	success = true
	common.Timeline("archive created with %d unique files", len(writtenDigests))
	return nil
}

// ExtractArchive extracts files from a tar.zst archive to the target directory
// This function streams extraction without loading the entire archive into memory
func ExtractArchive(archivePath, targetDir string) (err error) {
	defer fail.Around(&err)

	common.Timeline("extracting archive from %s to %s", archivePath, targetDir)

	// Open archive file
	archiveFile, err := os.Open(archivePath)
	fail.On(err != nil, "Failed to open archive %q -> %v", archivePath, err)
	defer archiveFile.Close()

	// Create zstd reader with pooled decoder
	zr, cleanup, err := getPooledDecoder(archiveFile)
	fail.On(err != nil, "Failed to create zstd reader -> %v", err)
	defer cleanup()

	// Create tar reader
	tr := tar.NewReader(zr)

	var manifest *ArchiveManifest
	filesExtracted := 0

	// Read and extract each file from the archive
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		fail.On(err != nil, "Failed to read tar header -> %v", err)

		// Handle version marker (first entry in archive)
		if header.Name == "RCCARCHIVE" {
			versionData, err := io.ReadAll(tr)
			fail.On(err != nil, "Failed to read version marker -> %v", err)
			version := strings.TrimSpace(string(versionData))
			if !strings.HasPrefix(version, "RCCARCHIVE/") {
				fail.On(true, "Invalid archive version marker: %q", version)
			}
			common.Timeline("archive version: %s", version)
			continue
		}

		// Handle manifest.json
		if header.Name == "manifest.json" {
			manifestData, err := io.ReadAll(tr)
			fail.On(err != nil, "Failed to read manifest -> %v", err)

			manifest = &ArchiveManifest{}
			err = json.Unmarshal(manifestData, manifest)
			fail.On(err != nil, "Failed to parse manifest -> %v", err)
			common.Timeline("manifest loaded with %d file entries", len(manifest.Files))
			continue
		}

		// Handle files
		if strings.HasPrefix(header.Name, "files/") {
			digest := strings.TrimPrefix(header.Name, "files/")

			// Security: validate digest to prevent path traversal attacks
			// Digests should be hex strings only, no path separators or parent references
			if strings.Contains(digest, "..") || strings.Contains(digest, "/") || strings.Contains(digest, "\\") {
				fail.On(true, "Invalid digest path in archive (possible path traversal): %q", digest)
			}

			// Determine target location for the file
			location := guessLocation(digest)
			targetPath := filepath.Join(common.HololibLibraryLocation(), location)

			// Ensure parent directory exists
			err = os.MkdirAll(targetPath, 0o755)
			fail.On(err != nil, "Failed to create directory %q -> %v", targetPath, err)

			// Full file path
			fullPath := filepath.Join(targetPath, digest)

			// Skip if file already exists
			if pathlib.IsFile(fullPath) {
				common.Timeline("file already exists, skipping: %s", digest)
				continue
			}

			// Create target file
			targetFile, err := pathlib.Create(fullPath)
			fail.On(err != nil, "Failed to create target file %q -> %v", fullPath, err)

			// Copy file content with buffered I/O
			buf := GetCopyBuffer()
			_, err = io.CopyBuffer(targetFile, tr, *buf)
			PutCopyBuffer(buf)
			targetFile.Close()
			fail.On(err != nil, "Failed to extract file %s -> %v", digest, err)

			// Set file mode
			err = os.Chmod(fullPath, os.FileMode(header.Mode))
			if err != nil {
				common.Timeline("warning: failed to set mode for %s -> %v", digest, err)
			}

			filesExtracted++
		}
	}

	fail.On(manifest == nil, "Archive does not contain a manifest.json")
	common.Timeline("extracted %d files from archive", filesExtracted)

	return nil
}

// CreateArchiveFromCatalog creates a tar.zst archive from a catalog file
// This is a convenience function that loads the catalog and creates an archive
func CreateArchiveFromCatalog(catalogPath, archivePath string, library Library) (err error) {
	defer fail.Around(&err)

	// Load catalog
	root, err := NewRoot(".")
	fail.On(err != nil, "Failed to create root -> %v", err)

	err = root.LoadFrom(catalogPath)
	fail.On(err != nil, "Failed to load catalog %q -> %v", catalogPath, err)

	// Collect all files from the catalog tree
	files := make(map[string]*File)
	collectFiles := func(path string, dir *Dir) error {
		for name, file := range dir.Files {
			fullPath := filepath.Join(path, name)
			files[fullPath] = file
		}
		return nil
	}

	err = root.Treetop(collectFiles)
	fail.On(err != nil, "Failed to collect files from catalog -> %v", err)

	common.Timeline("collected %d files from catalog", len(files))

	// Create the archive
	return CreateArchive(archivePath, files, library)
}

// ExtractArchiveToLibrary is an alias for ExtractArchive since extraction
// always goes to the library location
func ExtractArchiveToLibrary(archivePath string) error {
	// Extract directly to library (targetDir parameter is unused in current impl)
	return ExtractArchive(archivePath, common.HololibLibraryLocation())
}

// Note: guessLocation is defined in functions.go

// ArchiveInfo returns information about an archive without extracting it
type ArchiveInfo struct {
	Manifest    *ArchiveManifest
	FileCount   int
	TotalSize   int64
	ArchiveSize int64
}

// GetArchiveInfo reads and returns information about an archive
func GetArchiveInfo(archivePath string) (*ArchiveInfo, error) {
	// Get archive file size
	archiveStat, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}

	// Open archive
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer archiveFile.Close()

	// Create zstd reader
	zr, cleanup, err := getPooledDecoder(archiveFile)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Create tar reader
	tr := tar.NewReader(zr)

	info := &ArchiveInfo{
		ArchiveSize: archiveStat.Size(),
	}

	// Read archive to find manifest
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if header.Name == "manifest.json" {
			manifestData, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}

			info.Manifest = &ArchiveManifest{}
			err = json.Unmarshal(manifestData, info.Manifest)
			if err != nil {
				return nil, err
			}

			// Calculate total size from manifest
			for _, file := range info.Manifest.Files {
				info.TotalSize += file.Size
				info.FileCount++
			}
			break
		}
	}

	if info.Manifest == nil {
		return nil, fmt.Errorf("archive does not contain a manifest.json")
	}

	return info, nil
}
