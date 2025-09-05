package cmd

import (
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

var (
	removeCheckRetries int
	unusedDays         int
)

func holotreeRemove(catalogs []string) {
	if len(catalogs) == 0 {
		pretty.Warning("No catalogs given, so nothing to do. Quitting!")
		return
	}
	common.Debug("Trying to remove following catalogs:")
	for _, catalog := range catalogs {
		common.Debug("- %s", catalog)
	}

	tree, err := htfs.New()
	pretty.Guard(err == nil, 2, "%s", err)

	err = tree.Remove(catalogs)
	pretty.Guard(err == nil, 3, "%s", err)
}

func allUnusedCatalogs(limit int) []string {
	result := []string{}
	used := catalogUsedStats()
	for name, idle := range used {
		if idle > limit {
			result = append(result, name)
		}
	}
	return result
}

var holotreeRemoveCmd = &cobra.Command{
	Use:     "remove catalogid*",
	Short:   "Remove existing holotree catalogs.",
	Long:    "Remove existing holotree catalogs. Partial identities are ok.",
	Aliases: []string{"rm"},
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Holotree remove command lasted").Report()
		}
		if unusedDays > 0 {
			args = append(args, allUnusedCatalogs(unusedDays)...)
		}
		holotreeRemove(selectCatalogs(args))
		if removeCheckRetries > 0 {
			checkLoop(removeCheckRetries)
		} else {
			pretty.Warning("Remember to run `rcc holotree check` after you have removed all desired catalogs!")
		}
		pretty.Ok()
	},
}

func init() {
	holotreeRemoveCmd.Flags().IntVarP(&removeCheckRetries, "check", "c", 0, "Additionally run holotree check with this many times.")
	holotreeRemoveCmd.Flags().IntVarP(&unusedDays, "unused", "", 0, "Remove idle/unused catalog entries based on idle days when value is above given limit.")
	holotreeCmd.AddCommand(holotreeRemoveCmd)
}
