package conda

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

// writeTarGz creates a tar.gz archive at path with the given entries.
// Each entry is [name, content]; a content of "" signals a directory.
// Symlink entries are [name, "", linkTarget].
func writeTarGz(path string, entries [][3]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()
	for _, e := range entries {
		name, content, linkname := e[0], e[1], e[2]
		if linkname != "" {
			hdr := &tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: linkname}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			continue
		}
		if content == "" {
			hdr := &tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0755}
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			continue
		}
		hdr := &tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(content)), Mode: 0644}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return err
		}
	}
	return nil
}

// writeZip creates a zip archive at path with the given entries (name → content).
func writeZip(path string, entries map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return err
		}
	}
	return nil
}

func TestExtractTarGzBlocksPathTraversal(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "evil.tar.gz")
	destDir := filepath.Join(tmp, "dest")

	// Archive with a path-traversal entry (../../etc/passwd style).
	err := writeTarGz(archive, [][3]string{
		{"../../outside.txt", "should not land here", ""},
	})
	must_be.Nil(err)

	err = extractTarGz(archive, destDir)
	// extractTarGz must return an error; the file must NOT be created outside destDir.
	wont_be.Nil(err)

	// The traversal target must not exist.
	escaped := filepath.Join(tmp, "outside.txt")
	_, statErr := os.Stat(escaped)
	must_be.True(os.IsNotExist(statErr))
}

func TestExtractTarGzBlocksSiblingPrefixAttack(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "sibling.tar.gz")
	destDir := filepath.Join(tmp, "dest")

	// Archive whose entry resolves to a sibling directory that shares the destDir
	// prefix (e.g. "/tmp/dest" vs "/tmp/destevil").
	siblingFile := "../destevil/evil.txt"
	err := writeTarGz(archive, [][3]string{
		{siblingFile, "should be blocked", ""},
	})
	must_be.Nil(err)

	err = extractTarGz(archive, destDir)
	wont_be.Nil(err)
}

func TestExtractTarGzBlocksAbsoluteSymlink(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "abssym.tar.gz")
	destDir := filepath.Join(tmp, "dest")

	// Symlink whose target is an absolute path outside destDir.
	err := writeTarGz(archive, [][3]string{
		{"link", "", "/etc/passwd"},
	})
	must_be.Nil(err)

	err = extractTarGz(archive, destDir)
	wont_be.Nil(err)
}

func TestExtractTarGzBlocksEscapingSymlink(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "relsym.tar.gz")
	destDir := filepath.Join(tmp, "dest")

	// Relative symlink that traverses out of destDir.
	err := writeTarGz(archive, [][3]string{
		{"link", "", "../../outside"},
	})
	must_be.Nil(err)

	err = extractTarGz(archive, destDir)
	wont_be.Nil(err)
}

func TestExtractTarGzAllowsSafeSymlink(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "safesym.tar.gz")
	destDir := filepath.Join(tmp, "dest")

	// A legitimate relative symlink within the extraction directory.
	err := writeTarGz(archive, [][3]string{
		{"real.txt", "hello", ""},
		{"link.txt", "", "real.txt"},
	})
	must_be.Nil(err)

	err = extractTarGz(archive, destDir)
	must_be.Nil(err)

	// The symlink should exist and point to real.txt.
	linkPath := filepath.Join(destDir, "link.txt")
	fi, err := os.Lstat(linkPath)
	must_be.Nil(err)
	must_be.True(fi.Mode()&os.ModeSymlink != 0)
}

func TestExtractZipBlocksPathTraversal(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "evil.zip")
	destDir := filepath.Join(tmp, "dest")

	err := writeZip(archive, map[string]string{
		"../../outside.txt": "should not land here",
	})
	must_be.Nil(err)

	err = extractZip(archive, destDir)
	wont_be.Nil(err)

	escaped := filepath.Join(tmp, "outside.txt")
	_, statErr := os.Stat(escaped)
	must_be.True(os.IsNotExist(statErr))
}

func TestExtractZipBlocksSiblingPrefixAttack(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "sibling.zip")
	destDir := filepath.Join(tmp, "dest")

	err := writeZip(archive, map[string]string{
		"../destevil/evil.txt": "should be blocked",
	})
	must_be.Nil(err)

	err = extractZip(archive, destDir)
	wont_be.Nil(err)
}

func TestExtractZipAllowsNormalFile(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "ok.zip")
	destDir := filepath.Join(tmp, "dest")

	err := writeZip(archive, map[string]string{
		"subdir/hello.txt": "world",
	})
	must_be.Nil(err)

	err = extractZip(archive, destDir)
	must_be.Nil(err)

	content, err := os.ReadFile(filepath.Join(destDir, "subdir", "hello.txt"))
	must_be.Nil(err)
	must_be.Equal("world", string(content))
}
