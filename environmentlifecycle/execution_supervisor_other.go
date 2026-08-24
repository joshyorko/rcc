//go:build !windows

package environmentlifecycle

import "os"

type executionSupervisor struct{}

func newExecutionSupervisor() (*executionSupervisor, error) {
	return &executionSupervisor{}, nil
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
