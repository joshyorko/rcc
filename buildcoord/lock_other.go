//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package buildcoord

import (
	"errors"
	"os"
)

var errPlatformLockUnsupported = errors.New("filesystem lock is unsupported on this platform")

func openPlatformLock(string) (*os.File, error) { return nil, errPlatformLockUnsupported }

func acquirePlatformLock(*os.File) (bool, error) {
	return false, errPlatformLockUnsupported
}

func releasePlatformLock(file *os.File) error { return file.Close() }
