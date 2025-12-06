package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/joshyorko/rcc/interactive"
	"github.com/joshyorko/rcc/pretty"
	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:     "interactive",
	Aliases: []string{"i"},
	Short:   "Launch interactive TUI for managing robots and environments.",
	Long: `Launch the interactive terminal user interface for RCC.
Browse robots, run tasks, manage environments, and more.

This is an alias for 'rcc ui'.`,
	Run: func(cmd *cobra.Command, args []string) {
		if !pretty.Interactive {
			pretty.Exit(1, "The interactive mode requires a terminal (TTY)")
		}

		// Main UI loop - keep returning to UI after actions
		for {
			action, err := interactive.Run()
			pretty.Guard(err == nil, 1, "UI error: %v", err)

			// No action means user quit
			if action == nil {
				break
			}

			// Handle action result
			handleInteractiveAction(action)

			// Pause to let user see output, then return to UI
			fmt.Print("\n\033[36m▸\033[0m Press Enter to return to UI (or Ctrl+C to exit)...")
			fmt.Scanln()
		}
	},
}

func handleInteractiveAction(action *interactive.ActionResult) {
	switch action.Type {
	case interactive.ActionRunRobot:
		runInteractiveRobot(action)
	case interactive.ActionRunCommand:
		fmt.Printf("\n\033[36m▸\033[0m Command: %s\n", action.Command)
	}
}

func runInteractiveRobot(action *interactive.ActionResult) {
	robotDir := filepath.Dir(action.RobotPath)

	// Build command args with dashboard for unified output
	args := []string{"run", "-r", action.RobotPath, "--dashboard"}
	if action.RobotTask != "" {
		args = append(args, "-t", action.RobotTask)
	}
	if action.EnvFile != "" {
		args = append(args, "-e", action.EnvFile)
	}

	// Get rcc executable path
	rccPath, err := os.Executable()
	if err != nil {
		pretty.Exit(1, "Cannot find rcc executable: %v", err)
	}

	// Print what we're running
	fmt.Printf("\n\033[36m▸\033[0m Running: rcc %s\n\n", args)

	// Execute
	runCmd := exec.Command(rccPath, args...)
	runCmd.Dir = robotDir
	runCmd.Stdin = os.Stdin
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr

	if err := runCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Printf("\n\033[31m✗\033[0m Robot exited with code %d\n", exitErr.ExitCode())
		} else {
			fmt.Printf("\n\033[31m✗\033[0m Error running robot: %v\n", err)
		}
	} else {
		fmt.Printf("\n\033[32m✓\033[0m Robot completed successfully\n")
	}
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}
