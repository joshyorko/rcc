package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var userinfoCmd = &cobra.Command{
	Use:     "userinfo",
	Aliases: []string{"user"},
	Short:   fmt.Sprintf("Query user information from %s Control Room.", common.Product.Name()),
	Long:    fmt.Sprintf("Query user information from %s Control Room.", common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Userinfo query lasted").Report()
		}
		account := operations.AccountByName(AccountName())
		if account == nil {
			pretty.Exit(1, "Error: Could not find account by name: %q", AccountName())
		}
		client, err := cloud.NewClient(account.Endpoint)
		if err != nil {
			pretty.Exit(2, "Error: Could not create client for endpoint: %v, reason: %v", account.Endpoint, err)
		}
		data, err := operations.UserinfoCommand(client, account)
		if err != nil {
			pretty.Exit(3, "Error: Could not get user information: %v", err)
		}
		nice, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			pretty.Exit(4, "Error: Could not format reply: %v", err)
		}
		common.Stdout("%s\n", nice)
	},
}

func init() {
	cloudCmd.AddCommand(userinfoCmd)
}
