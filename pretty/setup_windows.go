//go:build windows || !darwin || !linux
// +build windows !darwin !linux

package pretty

import (
	"syscall"

	"github.com/robocorp/rcc/common"
)

const (
	ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x4
)

func localSetup(interactive bool) {
	Iconic = false
	Disabled = true
	if !interactive {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	if kernel32 == nil {
		common.Trace("Error: Cannot use colors. Did not get kernel32.dll!")
		return
	}
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	if setConsoleMode == nil {
		common.Trace("Error: Cannot use colors. Did not get SetConsoleMode!")
		return
	}
	target := syscall.Stdout
	var mode uint32
	err := syscall.GetConsoleMode(target, &mode)
	if err != nil {
		common.Trace("Error: Cannot use colors. Got mode error '%v'!", err)
	}
	mode |= ENABLE_VIRTUAL_TERMINAL_PROCESSING
	success, _, err := setConsoleMode.Call(uintptr(target), uintptr(mode))
	Disabled = success == 0
	if Disabled && err != nil {
		common.Trace("Error: Cannot use colors. Got error '%v'!", err)
	}
}
