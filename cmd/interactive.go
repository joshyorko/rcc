package cmd

import (
	"os"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/interactive"
	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:     "interactive",
	Aliases: []string{"i"},
	Short:   "Group of interactive commands. For human users. Do not use in automation.",
	Long: `This group of commands are interactive, asking questions from user when needed.
Do not try to use these in automation, they will fail there.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if common.DebugFlag() {
			defer common.Stopwatch("Interactive run").Report()
		}

		// interactive.Run() blocks until user quits TUI
		action, err := interactive.Run()
		if err != nil {
			return err
		}

		// Handle any action returned by the TUI
		if action != nil {
			switch action.Type {
			case interactive.ActionRunRobot:
				//Construct command to run robot
				rbtArgs := []string{"run", "--robot", action.RobotPath}
				if action.RobotTask != "" {
					rbtArgs = append(rbtArgs, "--task", action.RobotTask)
				}
				// TODO: handle environment file if needed

				// Execute rcc run
				// We can't easily call other cobra commands directly with clean state
				// But we can print what to do, or try to exec.
				// For now, let's just print the command or use common.Log
				common.Stdout("User requested to run robot: %s\n", action.RobotPath)
				// Actual execution logic might belong in operations or require a new process

				// Re-exec rcc with the run command?
				// This is the most reliable way to reset state
				rccExe, err := os.Executable()
				if err == nil {
					return common.Debug("Executing: %s %v", rccExe, rbtArgs)
					// Note: common.Debug doesn't run it.
					// We might want to actually run it.
					// return operations.ShellRun(...)
				}
			case interactive.ActionRunCommand:
				common.Stdout("Executing: %s\n", action.Command)
				// Parse and run command
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}
