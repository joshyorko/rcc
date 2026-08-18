//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package settings

import "os"

func syncSettingsParent(parent string) error {
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
