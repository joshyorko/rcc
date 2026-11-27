//go:build windows

package conda

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func openExecutableLock(path string) (*os.File, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(absolute, `\\?\`):
	case strings.HasPrefix(absolute, `\\.\`):
		return nil, errors.New("device namespace is not an executable install path")
	case strings.HasPrefix(absolute, `\\`):
		absolute = `\\?\UNC\` + strings.TrimPrefix(absolute, `\\`)
	default:
		absolute = `\\?\` + absolute
	}
	name, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(h)
		return nil, errors.New("executable install lock is a reparse point")
	}
	return os.NewFile(uintptr(h), path), nil
}

func acquireExecutableLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if err == windows.ERROR_LOCK_VIOLATION {
		return false, nil
	}
	return err == nil, err
}

func releaseExecutableLock(f *os.File) error {
	uerr := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
	cerr := f.Close()
	if uerr != nil {
		return uerr
	}
	return cerr
}
