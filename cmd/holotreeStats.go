package cmd

import (
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/journal"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	onlyAssistantStats bool
	onlyRobotStats     bool
	onlyPrepareStats   bool
	onlyVariablesStats bool
	statsWeeks         uint
)

var holotreeStatsCmd = &cobra.Command{
	Use:     "statistics",
	Short:   "Show holotree environment build and runtime statistics.",
	Long:    "Show holotree environment build and runtime statistics.",
	Aliases: []string{"statistic", "stats", "stat", "st"},
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Holotree stats calculation lasted").Report()
		}
		journal.ShowStatistics(statsWeeks, onlyAssistantStats, onlyRobotStats, onlyPrepareStats, onlyVariablesStats)
		pretty.Ok()
	},
}

func init() {
	holotreeCmd.AddCommand(holotreeStatsCmd)
	holotreeStatsCmd.Flags().BoolVarP(&onlyAssistantStats, "--assistants", "a", false, "Include 'assistant run' into stats.")
	holotreeStatsCmd.Flags().BoolVarP(&onlyRobotStats, "--robots", "r", false, "Include 'robot run' into stats.")
	holotreeStatsCmd.Flags().BoolVarP(&onlyPrepareStats, "--prepares", "p", false, "Include 'cloud prepare' into stats.")
	holotreeStatsCmd.Flags().BoolVarP(&onlyVariablesStats, "--variables", "v", false, "Include 'holotree variables' into stats.")
	holotreeStatsCmd.Flags().UintVarP(&statsWeeks, "--weeks", "w", 12, "Number of previous weeks to include into stats.")
}
