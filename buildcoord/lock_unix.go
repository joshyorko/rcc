//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package buildcoord

import (
	"os"

	"golang.org/x/sys/unix"
)

func openPlatformLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		if err == unix.ELOOP {
			return nil, ErrUnsafeState
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func acquirePlatformLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
		return false, nil
	}
	return err == nil, err
}

func releasePlatformLock(file *os.File) error {
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
