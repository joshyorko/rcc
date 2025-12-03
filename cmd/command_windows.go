package cmd

import (
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/shell"
)

func osSpecificHolotreeSharing(enable bool) {
	if !enable {
		return
	}
	pathlib.ForceShared()
	parent := filepath.Dir(common.Product.HoloLocation())
	_, err := pathlib.ForceSharedDir(parent)
	pretty.Guard(err == nil, 1, "Could not enable shared location at %q, reason: %v", parent, err)
	task := shell.New(nil, ".", "icacls", parent, "/grant", "*S-1-5-32-545:(OI)(CI)M", "/T", "/Q")
	_, err = task.Execute(false)
	pretty.Guard(err == nil, 2, "Could not set 'icacls' settings, reason: %v", err)
	err = os.WriteFile(common.SharedMarkerLocation(), []byte(common.Version), 0644)
	pretty.Guard(err == nil, 3, "Could not write %q, reason: %v", common.SharedMarkerLocation(), err)
}
