//go:build darwin

package artifactprovider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDarwinSystemAliasPathsPreserveNoFollow(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "rcc-provider-alias-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	providerRoot := filepath.Join(root, "provider")
	if _, err := NewFilesystem(providerRoot); err != nil {
		t.Fatalf("create provider beneath /tmp: %v", err)
	}
	for _, directory := range []string{"objects", "manifests", "tmp"} {
		if info, err := os.Stat(filepath.Join(providerRoot, directory)); err != nil || !info.IsDir() {
			t.Fatalf("provider directory %s is unavailable: %v", directory, err)
		}
	}

	outside := t.TempDir()
	redirect := filepath.Join(root, "redirect")
	if err := os.Symlink(outside, redirect); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesystem(filepath.Join(redirect, "provider")); err == nil {
		t.Fatal("provider followed a user-created symlink beneath /tmp")
	}
	if _, err := os.Stat(filepath.Join(outside, "provider")); !os.IsNotExist(err) {
		t.Fatalf("provider mutated symlink destination: %v", err)
	}
}
