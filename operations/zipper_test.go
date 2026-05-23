package operations

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

const (
	wintestpath = `a\b`
	nixtestpath = `a/b`
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

func TestCanConvertSlashes(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	wont.Equal(wintestpath, nixtestpath)
	must.Equal(3, len(wintestpath))
	must.Equal(slashed(wintestpath), nixtestpath)
}

func TestUnzipRejectsPathTraversal(t *testing.T) {
	must, wont := hamlet.Specifications(t)
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "malicious.zip")
	writeTestZip(t, zipPath, map[string]string{
		"../../outside.txt": "owned",
	})

	dest := filepath.Join(tmpDir, "dest")
	err := Unzip(dest, zipPath, true, true, false)
	wont.Nil(err)
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

func TestZipEntryTargetAcceptsNestedPath(t *testing.T) {
	must, _ := hamlet.Specifications(t)
	base := t.TempDir()

	target, err := zipEntryTarget(base, "repo/robot.yaml")

	must.Equal(nil, err)
	must.Equal(filepath.Join(base, "repo", "robot.yaml"), target)
}

func TestZipEntryTargetRejectsTraversalPath(t *testing.T) {
	_, wont := hamlet.Specifications(t)
	base := t.TempDir()

	_, err := zipEntryTarget(base, "../../outside.txt")

	wont.Equal(nil, err)
}
