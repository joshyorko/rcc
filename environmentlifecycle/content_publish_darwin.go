//go:build darwin

package environmentlifecycle

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func canonicalLifecycleRootPath(path string, _ bool) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if absolute == alias || strings.HasPrefix(absolute, alias+"/") {
			return "/private" + absolute, nil
		}
	}
	return absolute, nil
}

func publishLifecycleNoReplaceAt(directory int, temporary, destination string) error {
	if err := unix.Linkat(directory, temporary, directory, destination, 0); err != nil {
		return err
	}
	return unix.Unlinkat(directory, temporary, 0)
}
