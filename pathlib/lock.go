package pathlib

import (
	"os"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pretty"
)

type Releaser interface {
	Release() error
}

type Locked struct {
	*os.File
	Latch chan bool
}

type fake bool

func (it fake) Release() error {
	return common.Trace("LOCKER: lockless mode release.")
}

func Fake() Releaser {
	common.Trace("LOCKER: lockless mode.")
	return fake(true)
}

func waitingLockNotification(lockfile, message string, latch chan bool) {
	delay := 5 * time.Second
	counter := 0
waiting:
	for {
		select {
		case <-latch:
			return
		case <-time.After(delay):
			counter += 1
			delay *= 3
			pretty.Warning("#%d: %s (rcc lock wait)", counter, message)
			common.Timeline("waiting for lock")
			candidates, err := LockHoldersBy(lockfile)
			if err != nil {
				continue waiting
			}
			for _, candidate := range candidates {
				message := candidate.Message()
				pretty.Note("     : %s", message)
				common.Timeline("+ %s", message)
			}
		}
	}
}

func LockWaitMessage(lockfile, message string) func() {
	latch := make(chan bool)
	go waitingLockNotification(lockfile, message, latch)
	return func() {
		latch <- true
	}
}
