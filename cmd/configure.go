package cmd

import (
	"fmt"

	"github.com/robocorp/rcc/common"
	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:     "configuration",
	Aliases: []string{"conf", "config", "configure"},
	Short:   "Group of commands related to `rcc configuration`.",
	Long:    "Group of commands to configure rcc with your settings.",
}

func init() {
	rootCmd.AddCommand(configureCmd)

	configureCmd.PersistentFlags().StringVarP(&accountName, "account", "a", "", fmt.Sprintf("Account used for %s Control Room task.", common.Product.Name()))
}
