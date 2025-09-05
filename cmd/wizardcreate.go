package cmd

import (
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/wizard"

	"github.com/spf13/cobra"
)

var wizardCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a directory structure for a robot interactively.",
	Long:  "Create a directory structure for a robot interactively.",
	Run: func(cmd *cobra.Command, args []string) {
		if !pretty.Interactive {
			pretty.Exit(1, "This is for interactive use only. Do not use in scripting/CI!")
		}
		if common.DebugFlag() {
			defer common.Stopwatch("Interactive create lasted").Report()
		}
		err := wizard.Create(args)
		if err != nil {
			pretty.Exit(2, "%v", err)
		}
	},
}

func init() {
	if common.Product.IsLegacy() {
		interactiveCmd.AddCommand(wizardCreateCmd)
		rootCmd.AddCommand(wizardCreateCmd)
	}
}
