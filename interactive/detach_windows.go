//go:build windows

package interactive

import (
	"os/exec"
	"syscall"
)

// detachProcess configures cmd to run as a detached process on Windows
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
