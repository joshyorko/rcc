package cmd

import (
	"fmt"

	"github.com/robocorp/rcc/common"
	"github.com/spf13/cobra"
)

var cloudCmd = &cobra.Command{
	Use:     "cloud",
	Aliases: []string{"robocorp", "c"},
	Short:   fmt.Sprintf("Group of commands related to `%s Control Room`.", common.Product.Name()),
	Long:    fmt.Sprintf(`This group of commands apply to communication with %s Control Room.`, common.Product.Name()),
}

func init() {
	rootCmd.AddCommand(cloudCmd)

	cloudCmd.PersistentFlags().StringVarP(&accountName, "account", "a", "", fmt.Sprintf("Account used for %s Control Room operations.", common.Product.Name()))
}
