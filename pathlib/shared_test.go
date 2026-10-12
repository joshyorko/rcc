//go:build !windows

package pathlib

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMakeSharedDirPreservesExistingParentMode(t *testing.T) {
	t.Run("creates missing parent without changing existing ancestor", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}

		target := filepath.Join(root, "hololib", "catalog")
		if _, err := (privateSetup(1)).MakeSharedDir(target); err != nil {
			t.Fatal(err)
		}

		assertDirectoryMode(t, root, 0o700)
		assertDirectoryMode(t, filepath.Dir(target), 0o750)
		assertDirectoryMode(t, target, 0o750)
	})

	t.Run("does not change existing descendant used as parent", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(root, "hololib")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}

		target := filepath.Join(parent, "catalog")
		if _, err := (privateSetup(1)).MakeSharedDir(target); err != nil {
			t.Fatal(err)
		}

		assertDirectoryMode(t, root, 0o700)
		assertDirectoryMode(t, parent, 0o700)
		assertDirectoryMode(t, target, 0o750)
	})
}

func assertDirectoryMode(t *testing.T, directory string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != expected {
		t.Errorf("directory %q mode = %#o, want %#o", directory, got, expected)
	}
}
