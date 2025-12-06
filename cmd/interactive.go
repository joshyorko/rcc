package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshyorko/rcc/common"
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
		retryCount := 0
		maxRetries := 3
		nextView := interactive.ViewHome // Track which view to return to
		for {
			action, err := interactive.RunWithStartView(nextView)
			if err != nil {
				retryCount++
				fmt.Printf("\n\033[31m✗\033[0m UI error: %v\n", err)
				if retryCount >= maxRetries {
					fmt.Printf("\033[31m✗\033[0m Too many UI errors, exiting.\n")
					pretty.Exit(1, "UI failed after %d attempts", maxRetries)
				}
				fmt.Printf("\033[33m▸\033[0m Retrying... (attempt %d/%d)\n", retryCount, maxRetries)
				nextView = interactive.ViewHome // Reset to home on error
				continue
			}
			retryCount = 0 // Reset on success

			// No action means user quit
			if action == nil {
				break
			}

			// Handle action result
			handleInteractiveAction(action)

			// For robot runs, the dashboard shows the run complete view
			// User presses Esc there to return, so no need to pause here
			if action.Type != interactive.ActionRunRobot {
				// Pause to let user see output, then return to UI
				fmt.Print("\n\033[36m▸\033[0m Press Enter to return to UI (or Ctrl+C to exit)...")
				fmt.Scanln()
				nextView = interactive.ViewHome // Default return to home
			} else {
				// Return to the view specified by the action
				nextView = action.ReturnToView
			}
		}
	},
}

func handleInteractiveAction(action *interactive.ActionResult) {
	switch action.Type {
	case interactive.ActionRunRobot:
		runInteractiveRobot(action)
	case interactive.ActionRunCommand:
		runInteractiveCommand(action.Command)
	case interactive.ActionExportCatalog:
		runExportCatalog(action)
	case interactive.ActionImportCatalog:
		runImportCatalog(action)
	case interactive.ActionCheckIntegrity:
		runCheckIntegrity()
	}
}

func runExportCatalog(action *interactive.ActionResult) {
	rccPath, err := os.Executable()
	if err != nil {
		fmt.Printf("\n\033[31m✗\033[0m Cannot find rcc executable: %v\n", err)
		return
	}

	outputPath := action.OutputPath
	if outputPath == "" {
		outputPath = "hololib.zip"
	}

	fmt.Printf("\n\033[36m▸\033[0m Exporting catalog: %s\n", action.EnvID)
	fmt.Printf("   Output: %s\n\n", outputPath)

	cmd := exec.Command(rccPath, "holotree", "export", action.EnvID, "-z", outputPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("\n\033[31m✗\033[0m Export failed: %v\n", err)
	} else {
		fmt.Printf("\n\033[32m✓\033[0m Export completed: %s\n", outputPath)
	}
}

func runImportCatalog(action *interactive.ActionResult) {
	rccPath, err := os.Executable()
	if err != nil {
		fmt.Printf("\n\033[31m✗\033[0m Cannot find rcc executable: %v\n", err)
		return
	}

	fmt.Printf("\n\033[36m▸\033[0m Importing catalog from: %s\n\n", action.InputPath)

	cmd := exec.Command(rccPath, "holotree", "import", action.InputPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("\n\033[31m✗\033[0m Import failed: %v\n", err)
	} else {
		fmt.Printf("\n\033[32m✓\033[0m Import completed successfully\n")
	}
}

func runCheckIntegrity() {
	rccPath, err := os.Executable()
	if err != nil {
		fmt.Printf("\n\033[31m✗\033[0m Cannot find rcc executable: %v\n", err)
		return
	}

	fmt.Printf("\n\033[36m▸\033[0m Checking holotree integrity...\n\n")

	cmd := exec.Command(rccPath, "holotree", "check", "--retries", "3")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("\n\033[31m✗\033[0m Integrity check found issues (see above)\n")
	} else {
		fmt.Printf("\n\033[32m✓\033[0m Holotree integrity verified\n")
	}
}

func runInteractiveCommand(command string) {
	if command == "" {
		fmt.Printf("\n\033[31m✗\033[0m No command specified\n")
		return
	}

	rccPath, err := os.Executable()
	if err != nil {
		fmt.Printf("\n\033[31m✗\033[0m Cannot find rcc executable: %v\n", err)
		return
	}

	fmt.Printf("\n\033[36m▸\033[0m Running: %s\n\n", command)

	// Parse the command - expects "rcc <subcommand> [args...]"
	// Split by spaces (simple parsing)
	parts := strings.Fields(command)
	if len(parts) == 0 {
		fmt.Printf("\n\033[31m✗\033[0m Invalid command\n")
		return
	}

	// Skip "rcc" if present at start
	args := parts
	if parts[0] == "rcc" {
		args = parts[1:]
	}

	cmd := exec.Command(rccPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Printf("\n\033[31m✗\033[0m Command exited with code %d\n", exitErr.ExitCode())
		} else {
			fmt.Printf("\n\033[31m✗\033[0m Command failed: %v\n", err)
		}
	} else {
		fmt.Printf("\n\033[32m✓\033[0m Command completed successfully\n")
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

	// Extract robot name from path
	robotName := filepath.Base(robotDir)

	// Record run start in history
	history := interactive.GetRunHistory()
	entry := interactive.RunHistoryEntry{
		RobotPath:  action.RobotPath,
		RobotName:  robotName,
		Task:       action.RobotTask,
		StartTime:  time.Now(),
		Status:     interactive.RunUnknown,
		Controller: common.ControllerIdentity(),
	}
	history.AddEntry(entry)
	entryID := history.Entries[0].ID // Get the ID that was assigned
	history.Save()

	// Execute - the dashboard handles all output, so we don't print anything here
	runCmd := exec.Command(rccPath, args...)
	runCmd.Dir = robotDir
	runCmd.Stdin = os.Stdin
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr

	runErr := runCmd.Run()
	endTime := time.Now()

	// Update history silently - the dashboard already showed results
	if runErr != nil {
		exitCode := 1
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		history.UpdateEntry(entryID, endTime, interactive.RunFailed, exitCode)
	} else {
		history.UpdateEntry(entryID, endTime, interactive.RunSuccess, 0)
	}
	history.Save()
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}
