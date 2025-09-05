package cmd

import (
	"fmt"

	"github.com/robocorp/rcc/common"
	"github.com/spf13/cobra"
)

var robotCmd = &cobra.Command{
	Use:     "robot",
	Aliases: []string{"r"},
	Short:   "Group of commands related to `robot`.",
	Long: fmt.Sprintf(`This set of commands relate to %s Control Room related tasks. They are
executed either locally, or in connection to %s Control Room and tooling.`, common.Product.Name(), common.Product.Name()),
}

func init() {
	rootCmd.AddCommand(robotCmd)
}
