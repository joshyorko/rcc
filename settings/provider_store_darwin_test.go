//go:build darwin

package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDarwinSystemAliasPathsPreserveNoFollow(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "rcc-settings-alias-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	filename := filepath.Join(root, "home", "rcc.yaml")
	content := []byte("providers: {}\n")
	if err := writeCustomSettingsAtomically(filename, content); err != nil {
		t.Fatalf("write settings beneath /tmp: %v", err)
	}
	got, err := readCustomSettingsFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("settings content = %q", got)
	}
}
