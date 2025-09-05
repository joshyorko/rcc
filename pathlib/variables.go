package pathlib

import "github.com/robocorp/rcc/common"

var (
	Lockless bool
	shared   Shared
)

func init() {
	if common.SharedHolotree {
		ForceShared()
	} else {
		shared = privateSetup(1)
	}
}

func ForceShared() {
	shared = sharedSetup(9)
}
