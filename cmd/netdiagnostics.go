package cmd

import (
	"os"

	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	netConfigFilename string
	netConfigShow     bool
)

func summonNetworkDiagConfig(filename string) ([]byte, error) {
	if len(filename) == 0 {
		return blobs.Asset("assets/netdiag.yaml")
	}
	return os.ReadFile(filename)
}

var netDiagnosticsCmd = &cobra.Command{
	Use:     "netdiagnostics",
	Aliases: []string{"netdiagnostic", "netdiag"},
	Short:   "Run additional diagnostics to help resolve network issues.",
	Long:    "Run additional diagnostics to help resolve network issues.",
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Netdiagnostic run lasted").Report()
		}
		config, err := summonNetworkDiagConfig(netConfigFilename)
		if err != nil {
			pretty.Exit(1, "Problem loading configuration file, reason: %v", err)
		}
		if netConfigShow {
			common.Stdout("%s", string(config))
			os.Exit(0)
		}
		_, err = operations.ProduceNetDiagnostics(config, jsonFlag)
		if err != nil {
			pretty.Exit(1, "Error: %v", err)
		}
		pretty.Ok()
	},
}

func init() {
	configureCmd.AddCommand(netDiagnosticsCmd)
	rootCmd.AddCommand(netDiagnosticsCmd)

	netDiagnosticsCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output in JSON format")
	netDiagnosticsCmd.Flags().BoolVarP(&netConfigShow, "show", "s", false, "Show configuration instead of running diagnostics.")
	netDiagnosticsCmd.Flags().StringVarP(&netConfigFilename, "checks", "c", "", "Network checks configuration file. [optional]")
}
