//go:build darwin || linux || !windows
// +build darwin linux !windows

package pathlib

import (
	"os"
	"syscall"

	"github.com/robocorp/rcc/common"
)

func Locker(filename string, trycount int, sharedLocation bool) (Releaser, error) {
	if common.WarrantyVoided() || Lockless {
		return Fake(), nil
	}
	if common.TraceFlag() {
		defer common.Stopwatch("LOCKER: Got lock on %v in", filename).Report()
	}
	common.Trace("LOCKER: Want lock on: %v", filename)
	if sharedLocation {
		_, err := EnsureSharedParentDirectory(filename)
		if err != nil {
			return nil, err
		}
	} else {
		_, err := EnsureParentDirectory(filename)
		if err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return nil, err
	}
	_, err = shared.MakeSharedFile(filename)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(file.Fd()), int(syscall.LOCK_EX))
	if err != nil {
		return nil, err
	}
	lockpid := LockpidFor(filename)
	latch := lockpid.Keepalive()
	common.Trace("LOCKER: make marker %v", lockpid.Location())
	return &Locked{file, latch}, nil
}

func (it Locked) Release() error {
	defer it.Close()
	err := syscall.Flock(int(it.Fd()), int(syscall.LOCK_UN))
	common.Trace("LOCKER: release %v with err: %v", it.Name(), err)
	close(it.Latch)
	return err
}
