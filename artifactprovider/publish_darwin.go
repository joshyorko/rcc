//go:build darwin

package artifactprovider

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func canonicalProviderRootPath(path string, create bool) (string, error) {
	clean := filepath.Clean(path)
	if create {
		if err := os.MkdirAll(clean, 0o750); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("provider root must be a real directory")
	}
	return filepath.EvalSymlinks(clean)
}

func publishNoReplaceAt(directory int, temporary, destination string) error {
	if err := unix.Linkat(directory, temporary, directory, destination, 0); err != nil {
		return err
	}
	return unix.Unlinkat(directory, temporary, 0)
}
