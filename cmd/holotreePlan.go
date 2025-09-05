package cmd

import (
	"io"
	"os"

	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var holotreePlanCmd = &cobra.Command{
	Use:   "plan <plan+>",
	Short: "Show installation plans for given holotree spaces (or substrings)",
	Long:  "Show installation plans for given holotree spaces (or substrings)",
	Args:  cobra.MinimumNArgs(1),

	Run: func(cmd *cobra.Command, args []string) {
		found := false
		_, roots := htfs.LoadCatalogs()
		for _, label := range roots.FindEnvironments(args) {
			planfile, ok := roots.InstallationPlan(label)
			pretty.Guard(ok, 1, "Could not find plan for: %v", label)
			source, err := os.Open(planfile)
			pretty.Guard(err == nil, 2, "Could not read plan %q, reason: %v", planfile, err)
			defer source.Close()
			analyzer := conda.NewPlanAnalyzer(false)
			defer analyzer.Close()
			sink := io.MultiWriter(os.Stdout, analyzer)
			io.Copy(sink, source)
			found = true
		}
		pretty.Guard(found, 3, "Nothing matched given plans!")
		pretty.Ok()
	},
}

func init() {
	holotreeCmd.AddCommand(holotreePlanCmd)
}
