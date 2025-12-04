package cmd

import (
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/xviper"

	"github.com/spf13/cobra"
)

var (
	doNotTrack     bool
	enableTracking bool
)

var identityCmd = &cobra.Command{
	Use:     "identity",
	Aliases: []string{"i", "id"},
	Short:   "Manage rcc instance identity related things.",
	Long:    "Manage rcc instance identity related things.",
	Run: func(cmd *cobra.Command, args []string) {
		common.Stdout("rcc instance identity is: %v\n", xviper.TrackingIdentity())
		// Telemetry is disabled in this fork regardless of user preference.
		if enableTracking || doNotTrack {
			pretty.Warning("Telemetry is disabled in this fork; --enable/--do-not-track flags have no effect.")
		}
		common.Stdout("and anonymous health tracking is: disabled\n")
	},
}

func init() {
	configureCmd.AddCommand(identityCmd)
	identityCmd.Flags().BoolVarP(&doNotTrack, "do-not-track", "t", false, "Do not send application metrics. (opt-in)")
	identityCmd.Flags().BoolVarP(&enableTracking, "enable", "e", false, "Enable sending application metrics. (opt-in)")
}
