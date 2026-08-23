//go:build darwin

package environmentlifecycle

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lookupProcessIdentity(pid int) (string, error) {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.ENOENT) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	start := process.Proc.P_starttime
	if start.Sec == 0 && start.Usec == 0 {
		return "", fmt.Errorf("process start identity is unavailable")
	}
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), nil
}
