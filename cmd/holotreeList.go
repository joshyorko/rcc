package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

func whatUsage(space string) (string, string, int) {
	usefile := fmt.Sprintf("%s.use", space)
	stat, err := os.Stat(usefile)
	if err != nil {
		return "N/A", "N/A", 0
	}
	times := fmt.Sprintf("%d times", stat.Size())
	delta := time.Now().Sub(stat.ModTime()).Hours() / 24.0
	when := fmt.Sprintf("%1.0f days ago", delta)
	return when, times, int(delta)
}

func humaneHolotreeSpaceListing() {
	tabbed := tabwriter.NewWriter(os.Stderr, 2, 4, 2, ' ', 0)
	tabbed.Write([]byte("Identity\tController\tSpace\tBlueprint\tFull path\tLast used\tUse count\n"))
	tabbed.Write([]byte("--------\t----------\t-----\t---------\t---------\t---------\t---------\n"))
	_, roots := htfs.LoadCatalogs()
	for _, space := range roots.Spaces() {
		when, times, _ := whatUsage(space.Path)
		data := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n", space.Identity, space.Controller, space.Space, space.Blueprint, space.Path, when, times)
		tabbed.Write([]byte(data))
	}
	tabbed.Flush()
}

func jsonicHolotreeSpaceListing() {
	details := make(map[string]map[string]any)
	_, roots := htfs.LoadCatalogs()
	for _, space := range roots.Spaces() {
		hold, ok := details[space.Identity]
		if !ok {
			hold = make(map[string]any)
			details[space.Identity] = hold
			hold["id"] = space.Identity
			hold["controller"] = space.Controller
			hold["space"] = space.Space
			hold["blueprint"] = space.Blueprint
			hold["path"] = space.Path
			hold["meta"] = space.Path + ".meta"
			hold["spec"] = filepath.Join(space.Path, "identity.yaml")
			hold["plan"] = filepath.Join(space.Path, "rcc_plan.log")
			when, times, idle := whatUsage(space.Path)
			hold["last-used"] = when
			hold["idle-days"] = idle
			hold["use-count"] = times
		}
	}
	body, err := json.MarshalIndent(details, "", "  ")
	pretty.Guard(err == nil, 1, "Could not create json, reason: %w", err)
	fmt.Println(string(body))
}

var holotreeListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List holotree spaces.",
	Long:    "List holotree spaces.",
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Holotree list lasted").Report()
		}

		if jsonFlag {
			jsonicHolotreeSpaceListing()
		} else {
			humaneHolotreeSpaceListing()
		}

	},
}

func init() {
	holotreeCmd.AddCommand(holotreeListCmd)
	holotreeListCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output in JSON format")
}
