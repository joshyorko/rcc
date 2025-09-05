package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/robot"

	"github.com/spf13/cobra"
)

var (
	carrierFile  string
	carrierBuild bool
	carrierRun   bool
)

func buildCarrier() error {
	err := operations.SelfCopy(carrierFile)
	if err != nil {
		return err
	}
	return operations.SelfAppend(carrierFile, zipfile)
}

func runCarrier() error {
	ok, err := operations.IsCarrier()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("This executable is not carrier!")
	}
	if common.DebugFlag() {
		defer common.Stopwatch("Task testrun lasted").Report()
	}
	now := time.Now()
	testrunDir := filepath.Join(".", now.Format("2006-01-02_15_04_05"))
	err = os.MkdirAll(testrunDir, 0o755)
	if err != nil {
		return err
	}
	sentinelTime := time.Now()
	workarea := filepath.Join(pathlib.TempDir(), fmt.Sprintf("workarea%x", common.When))
	defer os.RemoveAll(workarea)
	common.Debug("Using temporary workarea: %v", workarea)
	carrier, err := operations.FindExecutable()
	if err != nil {
		return err
	}
	err = operations.CarrierUnzip(workarea, carrier, false, true)
	if err != nil {
		return err
	}
	defer pathlib.Walk(workarea, pathlib.IgnoreOlder(sentinelTime).Ignore, TargetDir(testrunDir).CopyBack)
	targetRobot := robot.DetectConfigurationName(workarea)
	simple, config, todo, label := operations.LoadTaskWithEnvironment(targetRobot, runTask, forceFlag)
	defer common.Log("Moving outputs to %v directory.", testrunDir)
	cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.cli.testrun", common.Version)
	operations.SelectExecutionModel(captureRunFlags(false), simple, todo.Commandline(), config, todo, label, false, nil)
	return nil
}

var carrierCmd = &cobra.Command{
	Use:   "carrier",
	Short: "Create carrier rcc with payload.",
	Long:  "Create carrier rcc with payload.",
	Run: func(cmd *cobra.Command, args []string) {
		defer common.Stopwatch("rcc carrier lasted").Report()
		if carrierBuild {
			err := buildCarrier()
			if err != nil {
				pretty.Exit(1, "Error: %v", err)
			}
			pretty.Ok()
		}
		if carrierRun {
			err := runCarrier()
			if err != nil {
				pretty.Exit(1, "Error: %v", err)
			}
		}
	},
}

func init() {
	internalCmd.AddCommand(carrierCmd)
	carrierCmd.Flags().StringVarP(&zipfile, "zipfile", "z", "robot.zip", "The filename for the carrier payload.")
	carrierCmd.Flags().StringVarP(&carrierFile, "carrier", "c", "carrier.exe", "The filename for the resulting carrier executable.")
	carrierCmd.Flags().BoolVarP(&carrierBuild, "build", "b", false, "Build actual carrier executable.")
	carrierCmd.Flags().BoolVarP(&carrierRun, "run", "r", false, "Run this executable as robot carrier.")
}
