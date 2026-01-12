package cmd

import (
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	deleteSpace      string
	deleteUnusedDays int
)

func spaceUsedStats() map[string]int {
	result := make(map[string]int)
	holotreeDir := common.HolotreeLocation()
	handle, err := os.Open(holotreeDir)
	if err != nil {
		return result
	}
	defer handle.Close()
	entries, err := handle.Readdir(-1)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		name := entry.Name()
		// Look for .use files
		if filepath.Ext(name) == ".use" {
			spaceName := name[:len(name)-4] // Remove .use extension
			days := common.DayCountSince(entry.ModTime())
			previous, ok := result[spaceName]
			if !ok || days < previous {
				result[spaceName] = days
			}
		}
	}
	return result
}

func allUnusedSpaces(limit int) []string {
	result := []string{}
	used := spaceUsedStats()
	for name, idle := range used {
		if idle > limit {
			result = append(result, name)
		}
	}
	return result
}

func deleteByPartialIdentity(partials []string) {
	_, roots := htfs.LoadCatalogs()
	var note string
	if dryFlag {
		note = "[dry run] "
	}
	for _, label := range roots.FindEnvironments(partials) {
		common.Log("%sRemoving %v", note, label)
		if dryFlag {
			continue
		}
		err := roots.RemoveHolotreeSpace(label)
		pretty.Guard(err == nil, 1, "Error: %v", err)
	}
}

var holotreeDeleteCmd = &cobra.Command{
	Use:     "delete <partial identity>*",
	Short:   "Delete one or more holotree controller spaces.",
	Long:    "Delete one or more holotree controller spaces.",
	Aliases: []string{"del"},
	Run: func(cmd *cobra.Command, args []string) {
		partials := make([]string, 0, len(args)+1)
		if len(args) > 0 {
			partials = append(partials, args...)
		}
		if len(deleteSpace) > 0 {
			partials = append(partials, htfs.ControllerSpaceName([]byte(common.ControllerIdentity()), []byte(deleteSpace)))
		}
		if deleteUnusedDays > 0 {
			partials = append(partials, allUnusedSpaces(deleteUnusedDays)...)
		}
		pretty.Guard(len(partials) > 0, 1, "Must provide either --space flag, --unused flag, or partial environment identity!")
		deleteByPartialIdentity(partials)
		pretty.Ok()
	},
}

func init() {
	holotreeCmd.AddCommand(holotreeDeleteCmd)
	holotreeDeleteCmd.Flags().BoolVarP(&dryFlag, "dryrun", "d", false, "Don't delete environments, just show what would happen.")
	holotreeDeleteCmd.Flags().StringVarP(&deleteSpace, "space", "s", "", "Client specific name to identify environment to delete.")
	holotreeDeleteCmd.Flags().IntVarP(&deleteUnusedDays, "unused", "", 0, "Delete idle/unused space entries based on idle days when value is above given limit.")
}
