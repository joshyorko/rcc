package cmd

import (
	"encoding/json"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var authorizeCmd = &cobra.Command{
	Use:   "authorize",
	Short: "Convert an API key to a valid authorization JWT token.",
	Long:  "Convert an API key to a valid authorization JWT token.",
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Authorize query lasted").Report()
		}
		period := &operations.TokenPeriod{
			ValidityTime: validityTime,
			GracePeriod:  gracePeriod,
		}
		period.EnforceGracePeriod()
		var claims *operations.Claims
		if granularity == "user" {
			claims = operations.ViewWorkspacesClaims(period.RequestSeconds())
		} else {
			claims = operations.RunRobotClaims(period.RequestSeconds(), workspaceId)
		}
		data, err := operations.AuthorizeClaims(AccountName(), claims, period)
		if err != nil {
			pretty.Exit(3, "Error: %v", err)
		}
		nice, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			pretty.Exit(4, "Error: Could not format reply: %v", err)
		}
		common.Stdout("%s\n", nice)
	},
}

func init() {
	cloudCmd.AddCommand(authorizeCmd)
	authorizeCmd.Flags().IntVarP(&validityTime, "minutes", "m", 15, "How many minutes the authorization should be valid for (minimum 15 minutes).")
	authorizeCmd.Flags().IntVarP(&gracePeriod, "graceperiod", "", 5, "What is grace period buffer in minutes on top of validity minutes (minimum 5 minutes).")
	authorizeCmd.Flags().StringVarP(&granularity, "granularity", "g", "", "Authorization granularity (user/workspace) used in.")
	authorizeCmd.Flags().StringVarP(&workspaceId, "workspace", "w", "", "Workspace id to use with this command.")
}
