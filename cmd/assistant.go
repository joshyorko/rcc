package cmd

import (
	"fmt"

	"github.com/robocorp/rcc/common"
	"github.com/spf13/cobra"
)

var assistantCmd = &cobra.Command{
	Use:     "assistant",
	Aliases: []string{"assist", "a"},
	Short:   "Group of commands related to `robot assistant`.",
	Long: fmt.Sprintf(`This set of commands relate to %s Robot Assistant related tasks.
They are either local, or in relation to %s Control Room and tooling.`, common.Product.Name(), common.Product.Name()),
}

func init() {
	if common.Product.IsLegacy() {
		rootCmd.AddCommand(assistantCmd)

		assistantCmd.PersistentFlags().StringVarP(&accountName, "account", "", "", fmt.Sprintf("Account used for %s Control Room operations.", common.Product.Name()))
	}
}
