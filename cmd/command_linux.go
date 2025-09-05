package cmd

import (
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

func osSpecificHolotreeSharing(enable bool) {
	if !enable {
		return
	}
	pathlib.ForceShared()
	parent := filepath.Dir(common.Product.HoloLocation())
	_, err := pathlib.ForceSharedDir(parent)
	pretty.Guard(err == nil, 1, "Could not enable shared location at %q, reason: %v", parent, err)
	_, err = pathlib.ForceSharedDir(common.Product.HoloLocation())
	pretty.Guard(err == nil, 2, "Could not enable shared location at %q, reason: %v", common.Product.HoloLocation(), err)
	err = os.WriteFile(common.SharedMarkerLocation(), []byte(common.Version), 0644)
	pretty.Guard(err == nil, 3, "Could not write %q, reason: %v", common.SharedMarkerLocation(), err)
}
