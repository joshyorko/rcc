package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/robocorp/rcc/anywork"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

var (
	checkRetries int
)

func checkHolotreeIntegrity() (err error) {
	defer fail.Around(&err)

	common.Timeline("holotree integrity check start")
	defer common.Timeline("holotree integrity check done")
	fs, err := htfs.NewRoot(common.HololibLibraryLocation())
	fail.On(err != nil, "%s", err)
	common.Timeline("holotree integrity lift")
	err = fs.Lift()
	fail.On(err != nil, "%s", err)
	common.Timeline("holotree integrity hasher")
	known, needed := htfs.LoadHololibHashes()
	err = fs.AllFiles(htfs.CheckHasher(known))
	fail.On(err != nil, "%s", err)
	collector := make(map[string]string)
	common.Timeline("holotree integrity collector")
	err = fs.Treetop(htfs.IntegrityCheck(collector, needed))
	common.Timeline("holotree integrity report")
	fail.On(err != nil, "%s", err)
	purge := make(map[string]bool)
	for k, _ := range collector {
		found, ok := known[filepath.Base(k)]
		if !ok {
			continue
		}
		for catalog, _ := range found {
			purge[catalog] = true
		}
	}
	for _, v := range needed {
		for catalog, _ := range v {
			purge[catalog] = true
		}
	}
	redo := false
	for k, _ := range purge {
		fmt.Println("Purge catalog:", k)
		redo = true
		anywork.Backlog(htfs.RemoveFile(k))
	}
	err = anywork.Sync()
	fail.On(err != nil, "%s", err)
	fail.On(redo, "Some catalogs were purged. Run this check command again, please!")
	fail.On(len(collector) > 0, "Size: %d", len(collector))
	err = pathlib.RemoveEmptyDirectores(common.HololibLibraryLocation())
	fail.On(err != nil, "%s", err)
	return nil
}

func checkLoop(retryCount int) {
	var err error
loop:
	for retryCount > 0 {
		retryCount--
		err = checkHolotreeIntegrity()
		if err == nil {
			break loop
		}
		common.Timeline("!!! holotree integrity retry needed [remaining: %d]", retryCount)
	}
	pretty.Guard(err == nil, 1, "%s", err)
}

var holotreeCheckCmd = &cobra.Command{
	Use:     "check",
	Short:   "Check holotree library integrity.",
	Long:    "Check holotree library integrity.",
	Aliases: []string{"chk"},
	Run: func(cmd *cobra.Command, args []string) {
		repeat := 1
		if checkRetries > 0 {
			repeat += checkRetries
		}
		checkLoop(repeat)
		pretty.Ok()
	},
}

func init() {
	holotreeCheckCmd.Flags().IntVarP(&checkRetries, "retries", "r", 1, "How many retries to do in case of failures.")
	holotreeCmd.AddCommand(holotreeCheckCmd)
}
