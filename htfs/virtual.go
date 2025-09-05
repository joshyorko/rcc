package htfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/journal"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

type virtual struct {
	identity uint64
	root     *Root
	registry map[string]string
	key      string
}

func Virtual() MutableLibrary {
	return &virtual{
		identity: common.Sipit([]byte(common.Product.Home())),
	}
}

func (it *virtual) Compress() bool {
	return true
}

func (it *virtual) Identity() string {
	suffix := fmt.Sprintf("%016x", it.identity)
	return fmt.Sprintf("v%s_%sh", common.UserHomeIdentity(), suffix[:14])
}

func (it *virtual) Stage() string {
	stage := filepath.Join(common.HolotreeLocation(), it.Identity())
	err := os.MkdirAll(stage, 0o755)
	if err != nil {
		panic(err)
	}
	return stage
}

func (it *virtual) CatalogPath(key string) string {
	return "Virtual Does Not Support Catalog Path Request"
}

func (it *virtual) Remove([]string) error {
	return fmt.Errorf("Not supported yet on virtual holotree.")
}

func (it *virtual) Export([]string, []string, string) error {
	return fmt.Errorf("Not supported yet on virtual holotree.")
}

func (it *virtual) WriteIdentity([]byte) error {
	return fmt.Errorf("Not supported yet on virtual holotree.")
}

func (it *virtual) Record(blueprint []byte) (err error) {
	defer fail.Around(&err)
	defer common.Stopwatch("Holotree recording took:").Debug()
	key := common.BlueprintHash(blueprint)
	common.Timeline("holotree record start %s (virtual)", key)
	fs, err := NewRoot(it.Stage())
	fail.On(err != nil, "Failed to create stage root: %v", err)
	err = fs.Lift()
	fail.On(err != nil, "Failed to lift structure out of stage: %v", err)
	common.Timeline("holotree (re)locator start (virtual)")
	err = fs.AllFiles(Locator(it.Identity()))
	fail.On(err != nil, "Failed to apply relocate to stage: %v", err)
	common.Timeline("holotree (re)locator done (virtual)")
	it.registry = make(map[string]string)
	fs.Treetop(DigestMapper(it.registry))
	fs.Blueprint = key
	it.root = fs
	it.key = key
	return nil
}

func (it *virtual) WarrantyVoidedDir(controller, space []byte) string {
	pretty.Exit(13, "hololib.zip does not support `--warranty-voided` running")
	return ""
}

func (it *virtual) TargetDir(blueprint, client, tag []byte) (string, error) {
	name := ControllerSpaceName(client, tag)
	return filepath.Join(common.HolotreeLocation(), name), nil
}

func (it *virtual) Restore(blueprint, client, tag []byte) (result string, err error) {
	return it.RestoreTo(blueprint, ControllerSpaceName(client, tag), string(client), string(tag), false)
}

func (it *virtual) RestoreTo(blueprint []byte, label, controller, space string, partial bool) (result string, err error) {
	defer common.Stopwatch("Holotree restore took:").Debug()
	key := common.BlueprintHash(blueprint)
	common.Timeline("holotree restore start %s (virtual)", key)
	targetdir := filepath.Join(common.HolotreeLocation(), label)
	metafile := fmt.Sprintf("%s.meta", targetdir)
	lockfile := fmt.Sprintf("%s.lck", targetdir)
	completed := pathlib.LockWaitMessage(lockfile, "Serialized holotree restore [holotree virtual lock]")
	locker, err := pathlib.Locker(lockfile, 30000, common.SharedHolotree)
	completed()
	fail.On(err != nil, "Could not get lock for %s. Quiting.", targetdir)
	defer locker.Release()
	journal.Post("space-used", metafile, "virutal holotree with blueprint %s", key)
	currentstate := make(map[string]string)
	shadow, err := NewRoot(targetdir)
	if err == nil {
		err = shadow.LoadFrom(metafile)
	}
	if err == nil {
		common.Timeline("holotree digest start (virtual)")
		shadow.Treetop(DigestRecorder(currentstate))
		common.Timeline("holotree digest done (virtual)")
	}
	fs := it.root
	err = fs.Relocate(targetdir)
	if err != nil {
		return "", err
	}
	common.Timeline("holotree make branches start (virtual)")
	err = fs.Treetop(MakeBranches)
	common.Timeline("holotree make branches done (virtual)")
	if err != nil {
		return "", err
	}
	score := &stats{}
	common.Timeline("holotree restore start (virtual)")
	err = fs.AllDirs(RestoreDirectory(it, fs, currentstate, score))
	if err != nil {
		return "", err
	}
	common.Timeline("holotree restore done (virtual)")
	defer common.Timeline("- dirty %d/%d", score.dirty, score.total)
	common.Debug("Holotree dirty workload: %d/%d\n", score.dirty, score.total)
	journal.CurrentBuildEvent().Dirty(score.Dirtyness())
	fs.Controller = controller
	fs.Space = space
	err = fs.SaveAs(metafile)
	if err != nil {
		return "", err
	}
	return targetdir, nil
}

func (it *virtual) Open(digest string) (readable io.Reader, closer Closer, err error) {
	return delegateOpen(it, digest, false)
}

func (it *virtual) ExactLocation(key string) string {
	return it.registry[key]
}

func (it *virtual) Location(key string) string {
	panic("Location is not supported on virtual holotree.")
}

func (it *virtual) ValidateBlueprint(blueprint []byte) error {
	return nil
}

func (it *virtual) HasBlueprint(blueprint []byte) bool {
	return it.key == common.BlueprintHash(blueprint)
}
