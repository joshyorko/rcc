//go:build linux

package environmentlifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableNoFollowAcceptsSameDirectoryLeafSymlink(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bin, "python3.11")
	if err := os.WriteFile(target, []byte("python"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("python3.11", filepath.Join(bin, "python")); err != nil {
		t.Fatal(err)
	}

	got, err := executableNoFollow(root, []string{"bin", "python"})
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("resolved executable = %q, want %q", got, target)
	}
}

func TestExecutableNoFollowRejectsUnsafeLeafSymlinks(t *testing.T) {
	for name, target := range map[string]string{
		"absolute": "/bin/sh",
		"parent":   "../python",
		"nested":   "safe/python",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(bin, "python")); err != nil {
				t.Fatal(err)
			}
			if got, err := executableNoFollow(root, []string{"bin", "python"}); err == nil {
				t.Fatalf("unsafe leaf symlink resolved to %q", got)
			}
		})
	}
}

func TestExecutableNoFollowRejectsChainedLeafSymlink(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "python3.11"), []byte("python"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("python3.11", filepath.Join(bin, "python3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("python3", filepath.Join(bin, "python")); err != nil {
		t.Fatal(err)
	}
	if got, err := executableNoFollow(root, []string{"bin", "python"}); err == nil {
		t.Fatalf("chained leaf symlink resolved to %q", got)
	}
}
