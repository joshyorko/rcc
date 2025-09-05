package cmd

import (
	"os"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

func osSpecificHolotreeSharing(enable bool) {
	if !enable {
		return
	}
	pathlib.ForceShared()
	err := os.WriteFile(common.SharedMarkerLocation(), []byte(common.Version), 0644)
	pretty.Guard(err == nil, 3, "Could not write %q, reason: %v", common.SharedMarkerLocation(), err)
}
