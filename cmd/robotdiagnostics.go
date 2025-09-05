package cmd

import (
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var robotDiagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Run system diagnostics to help resolve rcc issues.",
	Long:  "Run system diagnostics to help resolve rcc issues.",
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Diagnostic run lasted").Report()
		}
		err := operations.PrintRobotDiagnostics(robotFile, jsonFlag, productionFlag)
		if err != nil {
			pretty.Exit(1, "Error: %v", err)
		}
		pretty.Ok()
	},
}

func init() {
	robotCmd.AddCommand(robotDiagnosticsCmd)
	robotDiagnosticsCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output in JSON format")
	robotDiagnosticsCmd.Flags().BoolVarP(&productionFlag, "production", "p", false, "Checks for production level robots.")
	robotDiagnosticsCmd.Flags().StringVarP(&robotFile, "robot", "r", "robot.yaml", "Full path to the 'robot.yaml' configuration file.")
}
