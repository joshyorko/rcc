//go:build darwin

package artifactprovider

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func canonicalProviderRootPath(path string, _ bool) (string, error) {
	clean := filepath.Clean(path)
	if clean == "/var" || strings.HasPrefix(clean, "/var/") {
		clean = "/private" + clean
	}
	return clean, nil
}

func publishNoReplaceAt(directory int, temporary, destination string) error {
	if err := unix.Linkat(directory, temporary, directory, destination, 0); err != nil {
		return err
	}
	return unix.Unlinkat(directory, temporary, 0)
}
