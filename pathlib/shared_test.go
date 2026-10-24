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

func TestMakeSharedDirEnforcesModeOnExistingTarget(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup Shared
		mode  os.FileMode
	}{
		{name: "private", setup: privateSetup(1), mode: 0o750},
		{name: "shared", setup: sharedSetup(9), mode: 0o777},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "existing")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}

			if _, err := test.setup.MakeSharedDir(target); err != nil {
				t.Fatal(err)
			}
			assertDirectoryMode(t, target, test.mode)
		})
	}
}

func TestMakeSharedDirRejectsNonDirectoryParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "file")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := (privateSetup(1)).MakeSharedDir(filepath.Join(parent, "child")); err == nil {
		t.Fatal("MakeSharedDir succeeded with a regular file as parent")
	}
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
