//go:build darwin

package environmentlifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinSystemAliasPathsPreserveNoFollow(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "rcc-lifecycle-alias-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	lifecycleRoot := filepath.Join(root, "home", "artifacts")
	fd, err := openAbsoluteDirectory(lifecycleRoot, true)
	if err != nil {
		t.Fatalf("create lifecycle root beneath /tmp: %v", err)
	}
	if err := unix.Close(fd); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	redirect := filepath.Join(root, "redirect")
	if err := os.Symlink(outside, redirect); err != nil {
		t.Fatal(err)
	}
	if fd, err := openAbsoluteDirectory(filepath.Join(redirect, "artifacts"), true); err == nil {
		_ = unix.Close(fd)
		t.Fatal("lifecycle traversal followed a user-created symlink beneath /tmp")
	}
	if _, err := os.Stat(filepath.Join(outside, "artifacts")); !os.IsNotExist(err) {
		t.Fatalf("lifecycle traversal mutated symlink destination: %v", err)
	}
}
