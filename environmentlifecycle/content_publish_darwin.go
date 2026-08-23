//go:build darwin

package environmentlifecycle

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func canonicalLifecycleRootPath(path string, create bool) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if create {
		if err := os.MkdirAll(absolute, 0o750); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("environment artifact root must be a real directory")
	}
	return filepath.EvalSymlinks(absolute)
}

func publishLifecycleNoReplaceAt(directory int, temporary, destination string) error {
	if err := unix.Linkat(directory, temporary, directory, destination, 0); err != nil {
		return err
	}
	return unix.Unlinkat(directory, temporary, 0)
}
