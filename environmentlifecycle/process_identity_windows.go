//go:build windows

package environmentlifecycle

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func lookupProcessIdentity(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", err
	}
	if created.HighDateTime == 0 && created.LowDateTime == 0 {
		return "", fmt.Errorf("process start identity is unavailable")
	}
	return fmt.Sprintf("%08x%08x", uint32(created.HighDateTime), created.LowDateTime), nil
}
