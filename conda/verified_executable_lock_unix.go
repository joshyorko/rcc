//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package conda

import (
	"os"

	"golang.org/x/sys/unix"
)

func openExecutableLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func acquireExecutableLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
		return false, nil
	}
	return err == nil, err
}

func releaseExecutableLock(f *os.File) error {
	uerr := unix.Flock(int(f.Fd()), unix.LOCK_UN)
	cerr := f.Close()
	if uerr != nil {
		return uerr
	}
	return cerr
}
