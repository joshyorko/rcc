package htfs

import (
	"testing"
)

// TestArchiveTypes verifies that archive types compile correctly
func TestArchiveTypes(t *testing.T) {
	// Test ArchiveManifest
	manifest := &ArchiveManifest{
		Version: "1",
		Files:   make(map[string]*ArchiveFile),
	}
	if manifest.Version != "1" {
		t.Errorf("Expected version 1, got %s", manifest.Version)
	}

	// Test ArchiveFile
	archiveFile := &ArchiveFile{
		Digest:  "test123",
		Size:    1024,
		Mode:    0644,
		Rewrite: []int64{0, 10},
	}
	if archiveFile.Digest != "test123" {
		t.Errorf("Expected digest test123, got %s", archiveFile.Digest)
	}

	// Test ArchiveExists function signature
	_ = ArchiveExists("test")

	// Test ArchivePath function signature
	_ = ArchivePath("test")
}

// TestArchiveInfo verifies ArchiveInfo structure
func TestArchiveInfo(t *testing.T) {
	info := &ArchiveInfo{
		Manifest:    &ArchiveManifest{Version: "1"},
		FileCount:   10,
		TotalSize:   1024,
		ArchiveSize: 512,
	}
	if info.FileCount != 10 {
		t.Errorf("Expected 10 files, got %d", info.FileCount)
	}
}
