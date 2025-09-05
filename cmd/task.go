package cmd

import (
	"fmt"

	"github.com/robocorp/rcc/common"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:     "task",
	Aliases: []string{"t"},
	Short:   "Group of commands related to `task`.",
	Long: fmt.Sprintf(`This set of commands relate to %s Control Room related tasks. They are
executed either locally, or in connection to %s Control Room and tooling.`, common.Product.Name(), common.Product.Name()),
}

func init() {
	rootCmd.AddCommand(taskCmd)

	taskCmd.PersistentFlags().BoolVarP(&common.ExternallyManaged, "externally-managed", "", false, "mark created Python environments as EXTERNALLY-MANAGED (PEP 668)")
}
