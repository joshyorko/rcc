package operations

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("unable to create zip: %v", err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("unable to create zip entry: %v", err)
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			t.Fatalf("unable to write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("unable to close zip writer: %v", err)
	}
}

func TestUnzipRejectsPathTraversal(t *testing.T) {
	must, _ := hamlet.Specifications(t)
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "malicious.zip")
	writeTestZip(t, zipPath, map[string]string{
		"../../outside.txt": "owned",
	})

	dest := filepath.Join(tmpDir, "dest")
	err := Unzip(dest, zipPath, true, true, false)
	must.NotEqual(nil, err)
	must.Equal(false, filepath.IsAbs("../../outside.txt"))
	_, statErr := os.Stat(filepath.Join(tmpDir, "outside.txt"))
	must.Equal(true, os.IsNotExist(statErr))
}

func TestUnzipExtractsRegularFile(t *testing.T) {
	must, _ := hamlet.Specifications(t)
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "good.zip")
	writeTestZip(t, zipPath, map[string]string{
		"safe/file.txt": "ok",
	})

	dest := filepath.Join(tmpDir, "dest")
	err := Unzip(dest, zipPath, true, true, false)
	must.Equal(nil, err)
	content, err := os.ReadFile(filepath.Join(dest, "safe", "file.txt"))
	must.Equal(nil, err)
	must.Equal("ok", string(content))
}
