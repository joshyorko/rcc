//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package buildcoord

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformLockOpenRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "lock")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	file, err := openPlatformLock(link)
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("symlink lock error = %v, want ErrUnsafeState", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "sentinel" {
		t.Fatalf("symlink target changed to %q", content)
	}
}
