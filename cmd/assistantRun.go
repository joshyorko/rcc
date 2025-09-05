package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/journal"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/robot"

	"github.com/spf13/cobra"
)

var assistantRunCmd = &cobra.Command{
	Use:     "run",
	Aliases: []string{"r"},
	Short:   "Robot Assistant run",
	Long:    "Robot Assistant run.",
	Run: func(cmd *cobra.Command, args []string) {
		common.Timeline("cmd/assistant run entered")
		defer conda.RemoveCurrentTemp()
		defer journal.BuildEventStats("assistant")
		var status, reason string
		status, reason = "ERROR", "UNKNOWN"
		elapser := common.Stopwatch("Robot Assistant startup lasted")
		if common.DebugFlag() {
			defer common.Stopwatch("Robot Assistant run lasted").Report()
		}
		account := operations.AccountByName(AccountName())
		if account == nil {
			pretty.Exit(1, "Could not find account by name: %q", AccountName())
		}
		common.Timeline("new cloud client to %q", account.Endpoint)
		client, err := cloud.NewClient(account.Endpoint)
		if err != nil {
			pretty.Exit(2, "Could not create client for endpoint: %v, reason: %v", account.Endpoint, err)
		}
		common.Timeline("new cloud client created")
		reason = "START_FAILURE"
		cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.assistant.run.start", elapser.Elapsed().String())
		defer func() {
			cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.assistant.run.stop", reason)
		}()
		common.Timeline("start assistant run cloud call started")
		assistant, err := operations.StartAssistantRun(client, account, workspaceId, assistantId)
		common.Timeline("start assistant run cloud call completed")
		if err != nil {
			pretty.Exit(3, "Could not run assistant, reason: %v", err)
		}
		cancel := make(chan bool)
		go operations.BackgroundAssistantHeartbeat(cancel, client, account, workspaceId, assistantId, assistant.RunId)
		if assistant != nil && len(assistant.RunId) > 0 {
			defer func() {
				close(cancel)
				common.Debug("Signaling cloud with status %v with reason %v.", status, reason)
				err := operations.StopAssistantRun(client, account, workspaceId, assistantId, assistant.RunId, status, reason)
				common.Error("Stop assistant", err)
			}()
		}
		common.Debug("Robot Assistant run-id is %v.", assistant.RunId)
		common.Debug("With task '%v' from zip %v.", assistant.TaskName, assistant.Zipfile)
		sentinelTime := time.Now()
		workarea := filepath.Join(pathlib.TempDir(), fmt.Sprintf("workarea%x", common.When))
		defer os.RemoveAll(workarea)
		common.Debug("Using temporary workarea: %v", workarea)
		reason = "UNZIP_FAILURE"
		err = operations.Unzip(workarea, assistant.Zipfile, false, true, true)
		if err != nil {
			pretty.Exit(4, "Error: %v", err)
		}
		reason = "SETUP_FAILURE"
		targetRobot := robot.DetectConfigurationName(workarea)
		simple, config, todo, label := operations.LoadTaskWithEnvironment(targetRobot, assistant.TaskName, forceFlag)
		artifactDir := config.ArtifactDirectory()
		if len(copyDirectory) > 0 && len(artifactDir) > 0 {
			err := os.MkdirAll(copyDirectory, 0o755)
			if err == nil {
				defer pathlib.Walk(artifactDir, pathlib.IgnoreOlder(sentinelTime).Ignore, TargetDir(copyDirectory).OverwriteBack)
			}
		}
		if common.DebugFlag() {
			elapser.Report()
		}

		defer func() {
			cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.assistant.run.timeline.uploaded", elapser.Elapsed().String())
		}()
		defer func() {
			if len(assistant.ArtifactURL) == 0 {
				pretty.Note("Pushing artifacts to Cloud skipped (disabled, no artifact URL given).")
				common.Timeline("skipping publishing artifacts (disabled, no artifact URL given)")
				return
			}
			common.Timeline("publish artifacts")
			publisher := operations.ArtifactPublisher{
				Client:          client,
				ArtifactPostURL: assistant.ArtifactURL,
				ErrorCount:      0,
			}
			common.Log("Pushing artifacts to Cloud.")
			pathlib.Walk(artifactDir, pathlib.IgnoreDirectories, publisher.Publish)
			if publisher.ErrorCount > 0 {
				reason = "UPLOAD_FAILURE"
				pretty.Exit(5, "Error: Some of uploads failed.")
			}
		}()

		cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.assistant.run.timeline.setup", elapser.Elapsed().String())
		defer func() {
			cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.assistant.run.timeline.executed", elapser.Elapsed().String())
		}()
		reason = "ROBOT_FAILURE"
		operations.SelectExecutionModel(captureRunFlags(true), simple, todo.Commandline(), config, todo, label, false, assistant.Environment)
		pretty.Ok()
		status, reason = "OK", "PASS"
	},
}

func init() {
	assistantCmd.AddCommand(assistantRunCmd)
	assistantRunCmd.Flags().StringVarP(&workspaceId, "workspace", "w", "", "Workspace id to get assistant information.")
	assistantRunCmd.MarkFlagRequired("workspace")
	assistantRunCmd.Flags().StringVarP(&assistantId, "assistant", "a", "", "Assistant id to execute.")
	assistantRunCmd.MarkFlagRequired("assistant")
	assistantRunCmd.Flags().StringVarP(&copyDirectory, "copy", "c", "", "Location to copy changed artifacts from run (optional).")
	assistantRunCmd.Flags().StringVarP(&common.HolotreeSpace, "space", "s", "user", "Client specific name to identify this environment.")
}
