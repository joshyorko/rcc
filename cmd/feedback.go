package cmd

import (
	"fmt"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/xviper"
	"github.com/spf13/cobra"
)

var (
	issueRobot       string
	issueMetafile    string
	issueAttachments []string

	metricType  string
	metricName  string
	metricValue string
)

var feedbackCmd = &cobra.Command{
	Use:     "feedback",
	Aliases: []string{"f"},
	Short:   "Group of commands related to `rcc feedback`.",
	Long:    "Command group related to user feedback.",
	Hidden:  true,
}

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: fmt.Sprintf("Send an issue to %s Control Room via rcc.", common.Product.Name()),
	Long:  fmt.Sprintf("Send an issue to %s Control Room via rcc.", common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Feedback issue lasted").Report()
		}
		accountEmail := "unknown"
		account := operations.AccountByName(AccountName())
		if account != nil && account.Details != nil {
			email, ok := account.Details["email"].(string)
			if ok {
				accountEmail = email
			}
		}
		err := operations.ReportIssue(accountEmail, issueRobot, issueMetafile, issueAttachments, dryFlag)
		if err != nil {
			pretty.Exit(1, "Error: %s", err)
		}
		pretty.Exit(0, "OK")
	},
}

var metricCmd = &cobra.Command{
	Use:   "metric",
	Short: fmt.Sprintf("Send some metric to %s Control Room.", common.Product.Name()),
	Long:  fmt.Sprintf("Send some metric to %s Control Room.", common.Product.Name()),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Feedback metric lasted").Report()
		}
		if !xviper.CanTrack() {
			pretty.Exit(1, "Tracking is disabled. Quitting.")
		}
		cloud.BackgroundMetric(metricType, metricName, metricValue)
		pretty.Exit(0, "OK")
	},
}

var batchMetricCmd = &cobra.Command{
	Use:   "batch <metrics.json>",
	Short: fmt.Sprintf("Send batch metrics to %s Control Room. For applications only.", common.Product.Name()),
	Long:  fmt.Sprintf("Send batch metrics to %s Control Room. For applications only.", common.Product.Name()),
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Feedback batch lasted").Report()
		}
		if xviper.CanTrack() {
			cloud.BatchMetric(args[0])
		} else {
			pretty.Warning("Tracking is disabled. Quitting.")
		}
	},
}

func init() {
	rootCmd.AddCommand(feedbackCmd)

	feedbackCmd.AddCommand(issueCmd)
	issueCmd.Flags().StringVarP(&issueMetafile, "report", "r", "", "Report file in JSON form containing actual issue report details.")
	issueCmd.MarkFlagRequired("report")
	issueCmd.Flags().StringArrayVarP(&issueAttachments, "attachments", "a", []string{}, "Files to attach to issue report.")
	issueCmd.Flags().BoolVarP(&dryFlag, "dryrun", "d", false, "Don't send issue report, just show what would report be.")
	issueCmd.Flags().StringVarP(&issueRobot, "robot", "", "", "Full path to 'robot.yaml' configuration file. [optional]")

	feedbackCmd.AddCommand(metricCmd)
	metricCmd.Flags().StringVarP(&metricType, "type", "t", "", "Type for metric source to use.")
	metricCmd.MarkFlagRequired("type")
	metricCmd.Flags().StringVarP(&metricName, "name", "n", "", "Name for metric to report.")
	metricCmd.MarkFlagRequired("name")
	metricCmd.Flags().StringVarP(&metricValue, "value", "v", "", "Value for metric to report.")
	metricCmd.MarkFlagRequired("value")

	feedbackCmd.AddCommand(batchMetricCmd)
}
