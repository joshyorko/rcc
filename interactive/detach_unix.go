//go:build !windows

package interactive

import (
	"os/exec"
	"syscall"
)

// detachProcess configures cmd to run as a detached process on Unix
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session, detach from terminal
	}
}
