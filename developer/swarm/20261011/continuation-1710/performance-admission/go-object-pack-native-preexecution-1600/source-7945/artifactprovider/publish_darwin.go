//go:build darwin

package artifactprovider

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func canonicalProviderRootPath(path string, _ bool) (string, error) {
	clean := filepath.Clean(path)
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if clean == alias || strings.HasPrefix(clean, alias+"/") {
			return "/private" + clean, nil
		}
	}
	return clean, nil
}

func publishNoReplaceAt(directory int, temporary, destination string) error {
	if err := unix.Linkat(directory, temporary, directory, destination, 0); err != nil {
		return err
	}
	return unix.Unlinkat(directory, temporary, 0)
}
