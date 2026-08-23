//go:build linux

package artifactprovider

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

func canonicalProviderRootPath(path string, _ bool) (string, error) {
	return filepath.Clean(path), nil
}

func publishNoReplaceAt(directory int, temporary, destination string) error {
	return unix.Renameat2(directory, temporary, directory, destination, unix.RENAME_NOREPLACE)
}
