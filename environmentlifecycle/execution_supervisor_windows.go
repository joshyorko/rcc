//go:build windows

package environmentlifecycle

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type executionSupervisor struct {
	job       windows.Handle
	closeOnce sync.Once
	closeErr  error
}

func newExecutionSupervisor() (*executionSupervisor, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create execution job object: %w", err)
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure execution job object: %w", err)
	}
	return &executionSupervisor{job: job}, nil
}

func (supervisor *executionSupervisor) attach(process *os.Process) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Pid))
	if err != nil {
		return fmt.Errorf("open execution process for job object: %w", err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.AssignProcessToJobObject(supervisor.job, handle); err != nil {
		return fmt.Errorf("assign execution process to job object: %w", err)
	}
	return nil
}

func (supervisor *executionSupervisor) terminate(process *os.Process) error {
	if err := supervisor.close(); err != nil {
		if killErr := process.Kill(); killErr != nil {
			return fmt.Errorf("close execution job object: %v; kill direct process: %w", err, killErr)
		}
		return err
	}
	return nil
}

func (supervisor *executionSupervisor) close() error {
	supervisor.closeOnce.Do(func() {
		supervisor.closeErr = windows.CloseHandle(supervisor.job)
	})
	return supervisor.closeErr
}
