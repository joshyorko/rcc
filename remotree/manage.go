package remotree

import (
	"path/filepath"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
)

func cleanupHoldStorage(storage string) error {
	if !pathlib.IsDir(storage) {
		return nil
	}
	filenames, err := filepath.Glob(filepath.Join(storage, "*.hld"))
	if err != nil {
		return err
	}
	for _, filename := range filenames {
		err = pathlib.TryRemove("hold", filename)
		if err != nil {
			return err
		}
		common.Debug("Old hold file %q removed.", filename)
	}
	return nil
}
