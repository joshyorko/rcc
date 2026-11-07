//go:build windows

package artifacttrust

import (
	"golang.org/x/sys/windows"
	"os"
)

func metadataStateLock(path string) (func(), error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, metadataError(CodeMetadataState)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlap windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlap); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlap); _ = f.Close() }, nil
}

// The append journal is flushed before admission. Native crash qualification
// must establish the supported Windows filesystem persistence contract.
func syncMetadataStateDirectory(string) error { return nil }
