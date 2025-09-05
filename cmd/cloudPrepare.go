package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/journal"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/robot"

	"github.com/spf13/cobra"
)

var prepareCloudCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Prepare cloud robot for fast startup time in local computer.",
	Long:  "Prepare cloud robot for fast startup time in local computer.",
	Run: func(cmd *cobra.Command, args []string) {
		defer journal.BuildEventStats("prepare")
		if common.DebugFlag() {
			defer common.Stopwatch("Cloud prepare lasted").Report()
		}

		zipfile := filepath.Join(pathlib.TempDir(), fmt.Sprintf("summon%x.zip", common.When))
		defer os.Remove(zipfile)

		workarea := filepath.Join(pathlib.TempDir(), fmt.Sprintf("workarea%x", common.When))
		defer os.RemoveAll(workarea)

		account := operations.AccountByName(AccountName())
		pretty.Guard(account != nil, 2, "Could not find account by name: %q", AccountName())

		client, err := cloud.NewClient(account.Endpoint)
		pretty.Guard(err == nil, 3, "Could not create client for endpoint: %v, reason: %v", account.Endpoint, err)

		err = operations.DownloadCommand(client, account, workspaceId, robotId, zipfile, common.DebugFlag())
		pretty.Guard(err == nil, 4, "Error: %v", err)

		common.Debug("Using temporary workarea: %v", workarea)
		err = operations.Unzip(workarea, zipfile, false, true, true)
		pretty.Guard(err == nil, 5, "Error: %v", err)

		robotfile, err := pathlib.FindNamedPath(workarea, "robot.yaml")
		pretty.Guard(err == nil, 6, "Error: %v", err)

		config, err := robot.LoadRobotYaml(robotfile, false)
		pretty.Guard(err == nil, 7, "Error: %v", err)
		pretty.Guard(config.UsesConda(), 0, "Ok.")

		var label string
		condafile := config.CondaConfigFile()
		label, _, err = htfs.NewEnvironment(condafile, config.Holozip(), true, false, operations.PullCatalog)
		pretty.Guard(err == nil, 8, "Error: %v", err)

		common.Log("Prepared %q.", label)
		pretty.Ok()
	},
}

func init() {
	cloudCmd.AddCommand(prepareCloudCmd)
	prepareCloudCmd.Flags().StringVarP(&workspaceId, "workspace", "w", "", "The workspace id to use as the download source.")
	prepareCloudCmd.MarkFlagRequired("workspace")
	prepareCloudCmd.Flags().StringVarP(&robotId, "robot", "r", "", "The robot id to use as the download source.")
	prepareCloudCmd.MarkFlagRequired("robot")
	prepareCloudCmd.Flags().StringVarP(&common.HolotreeSpace, "space", "s", "user", "Client specific name to identify this environment.")
}
