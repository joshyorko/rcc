package conda

import (
	"os"
	"path/filepath"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

func safeRemove(hint, pathling string) error {
	var err error
	if !pathlib.Exists(pathling) {
		common.Debug("[%s] Missing %v, no need to remove.", hint, pathling)
		return nil
	}
	if pathlib.IsDir(pathling) {
		err = renameRemove(pathling)
	} else {
		err = os.Remove(pathling)
	}
	if err != nil {
		pretty.Warning("[%s] %s -> %v", hint, pathling, err)
		pretty.Warning("Make sure that you have rights to %q, and that nothing has locks in there.", pathling)
	} else {
		common.Debug("[%s] Removed %v.", hint, pathling)
	}
	return err
}

func doCleanup(fullpath string, dryrun bool) error {
	if !pathlib.Exists(fullpath) {
		return nil
	}
	if dryrun {
		common.Log("Would be removing: %s", fullpath)
		return nil
	}
	return safeRemove("path", fullpath)
}

func bugsCleanup(dryrun bool) {
	if dryrun {
		common.Log("- %v", common.BadHololibSitePackagesLocation())
		common.Log("- %v", common.BadHololibScriptsLocation())
		return
	}
	safeRemove("bugs", common.BadHololibSitePackagesLocation())
	safeRemove("bugs", common.BadHololibScriptsLocation())
}

func alwaysCleanup(dryrun bool) {
	base := filepath.Join(common.Product.Home(), "base")
	live := filepath.Join(common.Product.Home(), "live")
	miniconda3 := filepath.Join(common.Product.Home(), "miniconda3")
	if dryrun {
		common.Log("Would be removing:")
		common.Log("- %v", base)
		common.Log("- %v", live)
		common.Log("- %v", miniconda3)
		return
	}
	safeRemove("legacy", base)
	safeRemove("legacy", live)
	safeRemove("legacy", miniconda3)
}

func downloadCleanup(dryrun bool) (err error) {
	defer fail.Around(&err)
	if dryrun {
		common.Log("- %v", common.TemplateLocation())
		common.Log("- %v", common.PipCache())
		common.Log("- %v", common.UvCache())
		common.Log("- %v", common.MambaPackages())
	} else {
		fail.Fast(safeRemove("templates", common.TemplateLocation()))
		fail.Fast(safeRemove("cache", common.PipCache()))
		fail.Fast(safeRemove("cache", common.UvCache()))
		fail.Fast(safeRemove("cache", common.MambaPackages()))
	}
	return nil
}

func quickCleanup(dryrun bool) error {
	downloadCleanup(dryrun)
	if dryrun {
		common.Log("- %v", common.HolotreeLocation())
		common.Log("- %v", common.ProductTempRoot())
		return nil
	}
	err := safeRemove("cache", common.HolotreeLocation())
	if err != nil {
		return err
	}
	return safeRemove("temp", common.ProductTempRoot())
}

func cleanupAllCaches(dryrun bool) error {
	downloadCleanup(dryrun)
	if dryrun {
		common.Log("- %v", common.HololibLocation())
		return nil
	}
	fail.Fast(safeRemove("cache", common.HololibLocation()))
	return nil
}

func spotlessCleanup(dryrun, noCompress bool) (err error) {
	defer fail.Around(&err)

	fail.Fast(quickCleanup(dryrun))
	rcccache := filepath.Join(common.Product.Home(), "rcccache.yaml")
	if dryrun {
		common.Log("- %v", common.BinLocation())
		common.Log("- %v", common.MicromambaLocation())
		common.Log("- %v", common.RobotCache())
		common.Log("- %v", rcccache)
		common.Log("- %v", common.OldEventJournal())
		common.Log("- %v", common.JournalLocation())
		common.Log("- %v", common.HololibCatalogLocation())
		common.Log("- %v", common.HololibLocation())
		return nil
	}
	fail.Fast(safeRemove("executables", common.BinLocation()))
	fail.Fast(safeRemove("micromamba", common.MicromambaLocation()))
	fail.Fast(safeRemove("cache", common.RobotCache()))
	fail.Fast(safeRemove("cache", rcccache))
	fail.Fast(safeRemove("old", common.OldEventJournal()))
	fail.Fast(safeRemove("journals", common.JournalLocation()))
	fail.Fast(safeRemove("catalogs", common.HololibCatalogLocation()))
	fail.Fast(safeRemove("cache", common.HololibLocation()))
	if noCompress {
		return pathlib.WriteFile(common.HololibCompressMarker(), []byte("present"), 0o666)
	}
	return nil
}

func cleanupTemp(deadline time.Time, dryrun bool) error {
	basedir := common.ProductTempRoot()
	handle, err := os.Open(basedir)
	if err != nil {
		return err
	}
	entries, err := handle.Readdir(-1)
	handle.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.ModTime().After(deadline) {
			continue
		}
		fullpath := filepath.Join(basedir, entry.Name())
		if dryrun {
			common.Log("Would remove temp %v.", fullpath)
			continue
		}
		if entry.IsDir() {
			err = os.RemoveAll(fullpath)
			if err != nil {
				common.Log("Warning[%q]: %v", fullpath, err)
			}
		} else {
			os.Remove(fullpath)
		}
		common.Debug("Removed %v.", fullpath)
	}
	return nil
}

func BugsCleanup() {
	bugsCleanup(false)
}

func Cleanup(daylimit int, dryrun, quick, all, micromamba, downloads, noCompress, caches bool) (err error) {
	defer fail.Around(&err)

	lockfile := common.ProductLock()
	completed := pathlib.LockWaitMessage(lockfile, "Serialized environment cleanup [robocorp lock]")
	locker, err := pathlib.Locker(lockfile, 30000, false)
	completed()
	if err != nil {
		common.Log("Could not get lock on live environment. Quitting!")
		return err
	}
	defer locker.Release()

	alwaysCleanup(dryrun)
	bugsCleanup(dryrun)

	if downloads {
		return downloadCleanup(dryrun)
	}

	if quick {
		return quickCleanup(dryrun)
	}

	if caches {
		fail.Fast(cleanupAllCaches(dryrun))
	}

	if all {
		return spotlessCleanup(dryrun, noCompress)
	}

	deadline := time.Now().Add(-24 * time.Duration(daylimit) * time.Hour)
	cleanupTemp(deadline, dryrun)

	if micromamba && err == nil {
		err = doCleanup(common.MambaPackages(), dryrun)
	}
	if micromamba && err == nil {
		err = doCleanup(BinMicromamba(), dryrun)
	}
	if micromamba && err == nil {
		err = doCleanup(common.MicromambaLocation(), dryrun)
	}
	return err
}

func RemoveCurrentTemp() {
	target := common.ProductTempName()
	common.Debug("removing current temp %v", target)
	common.Timeline("removing current temp: %v", target)
	err := safeRemove("temp", target)
	if err != nil {
		common.Timeline("removing current temp failed, reason: %v", err)
	}
}
