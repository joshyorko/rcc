//go:build linux

package environmentlifecycle

import (
	"os"
	"os/exec"
	"syscall"
)

type executionSupervisor struct{}

func newExecutionSupervisor() (*executionSupervisor, error) {
	return &executionSupervisor{}, nil
}

func (supervisor *executionSupervisor) prepare(command *exec.Cmd) error {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Pdeathsig = syscall.SIGKILL
	return nil
}

func (supervisor *executionSupervisor) attach(process *os.Process) error {
	return nil
}

func (supervisor *executionSupervisor) terminate(process *os.Process) error {
	return process.Kill()
}

func (supervisor *executionSupervisor) close() error {
	return nil
}
