//go:build linux

package environmentlifecycle

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

func canonicalLifecycleRootPath(path string, _ bool) (string, error) {
	return filepath.Abs(path)
}

func publishLifecycleNoReplaceAt(directory int, temporary, destination string) error {
	return unix.Renameat2(directory, temporary, directory, destination, unix.RENAME_NOREPLACE)
}
