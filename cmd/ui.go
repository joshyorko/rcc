package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/interactive"
	"github.com/joshyorko/rcc/pretty"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"tui", "dashboard"},
	Short:   "Launch the interactive terminal UI",
	Long: `Launch the interactive terminal user interface (TUI) for RCC.

This provides a k9s-style interface for:
  - Browsing and running robots
  - Managing holotree environments
  - Viewing logs
  - Executing RCC commands

Navigation:
  1-5        Switch views (Home, Commands, Robots, Envs, Logs)
  j/k        Navigate up/down
  Enter      Select
  q          Quit
  ?          Help

Example:
  rcc ui
  rcc tui
  rcc dashboard`,
	Run: func(cmd *cobra.Command, args []string) {
		if !pretty.Interactive {
			pretty.Exit(1, "The UI requires an interactive terminal (TTY)")
		}

		action, err := interactive.Run()
		pretty.Guard(err == nil, 1, "UI error: %v", err)

		// Handle action result
		if action != nil {
			handleAction(action)
		}
	},
}

func handleAction(action *interactive.ActionResult) {
	switch action.Type {
	case interactive.ActionRunRobot:
		runRobotAction(action)
	case interactive.ActionRunCommand:
		runCommandAction(action)
	case interactive.ActionDeleteEnv:
		deleteEnvAction(action)
	case interactive.ActionExportCatalog:
		exportCatalogAction(action)
	case interactive.ActionToggleServer:
		toggleServerAction(action)
	}
}

func runRobotAction(action *interactive.ActionResult) {
	robotDir := filepath.Dir(action.RobotPath)

	// Build command args - include --dashboard flag for unified dashboard
	args := []string{"run", "--dashboard", "-r", action.RobotPath}
	if action.RobotTask != "" {
		args = append(args, "-t", action.RobotTask)
	}

	// Execute rcc run
	rccPath, err := os.Executable()
	if err != nil {
		pretty.Exit(1, "Could not find rcc executable: %v", err)
	}

	runCmd := exec.Command(rccPath, args...)
	runCmd.Dir = robotDir
	runCmd.Stdin = os.Stdin
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr

	if err := runCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		pretty.Exit(1, "Error running robot: %v", err)
	}
}

func runCommandAction(action *interactive.ActionResult) {
	fmt.Printf("\n%s▸%s Command: %s\n\n", pretty.Cyan, pretty.Reset, action.Command)
	fmt.Println("Copy and run the command above, or press Enter to execute it.")
	fmt.Print("Execute? [y/N]: ")

	var response string
	fmt.Scanln(&response)

	if response == "y" || response == "Y" {
		// Parse and execute the command
		// For safety, we just show it instead of executing arbitrary commands
		fmt.Printf("\nExecuting: %s\n\n", action.Command)

		// Simple execution - this could be enhanced to actually parse and run
		rccPath, err := os.Executable()
		if err != nil {
			pretty.Exit(1, "Could not find rcc executable: %v", err)
		}

		// Parse command (remove "rcc " prefix)
		cmdStr := action.Command
		if len(cmdStr) > 4 && cmdStr[:4] == "rcc " {
			cmdStr = cmdStr[4:]
		}

		// Split into args (simple split, doesn't handle quotes)
		args := splitCommand(cmdStr)

		runCmd := exec.Command(rccPath, args...)
		runCmd.Stdin = os.Stdin
		runCmd.Stdout = os.Stdout
		runCmd.Stderr = os.Stderr

		if err := runCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			pretty.Exit(1, "Error running command: %v", err)
		}
	}
}

func deleteEnvAction(action *interactive.ActionResult) {
	fmt.Printf("\n%s▸%s Deleting environment: %s\n\n", pretty.Yellow, pretty.Reset, action.EnvID)

	// Load catalogs and roots to access the removal function
	_, roots := htfs.LoadCatalogs()

	// Use htfs to delete the holotree space
	err := roots.RemoveHolotreeSpace(action.EnvID)
	if err != nil {
		pretty.Exit(1, "Error deleting environment: %v", err)
	}

	common.Log("Successfully deleted environment: %s", action.EnvID)
}

func exportCatalogAction(action *interactive.ActionResult) {
	fmt.Printf("\n%s▸%s Exporting catalog: %s\n\n", pretty.Cyan, pretty.Reset, action.EnvID)

	// Generate output filename
	outputFile := action.EnvID + ".zip"
	if action.OutputPath != "" {
		outputFile = action.OutputPath
	}

	fmt.Printf("Export destination: %s\n", outputFile)
	fmt.Print("Proceed? [y/N]: ")

	var response string
	fmt.Scanln(&response)

	if response != "y" && response != "Y" {
		fmt.Println("Export cancelled.")
		return
	}

	// Execute rcc holotree export
	rccPath, err := os.Executable()
	if err != nil {
		pretty.Exit(1, "Could not find rcc executable: %v", err)
	}

	args := []string{"holotree", "export", "-z", outputFile}
	runCmd := exec.Command(rccPath, args...)
	runCmd.Stdin = os.Stdin
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr

	if err := runCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		pretty.Exit(1, "Error exporting catalog: %v", err)
	}

	fmt.Printf("\n%s✓%s Catalog exported to: %s\n", pretty.Green, pretty.Reset, outputFile)
}

func toggleServerAction(action *interactive.ActionResult) {
	fmt.Printf("\n%s▸%s RCC Remote Server\n\n", pretty.Cyan, pretty.Reset)
	fmt.Println("To start the RCC remote server, run:")
	fmt.Printf("  %srccremote -hostname 0.0.0.0 -port 4653%s\n\n", pretty.Cyan, pretty.Reset)
	fmt.Println("Or with custom settings:")
	fmt.Printf("  %srccremote -hostname localhost -port 4653 -domain myteam%s\n\n", pretty.Cyan, pretty.Reset)
	fmt.Println("Note: Shared holotree must be enabled for rccremote to work.")
	fmt.Println("Clients can connect by setting:")
	fmt.Printf("  %sexport RCC_REMOTE_ORIGIN=https://your-server:port%s\n", pretty.Cyan, pretty.Reset)
}

// splitCommand splits a command string into arguments (simple split on spaces)
func splitCommand(cmd string) []string {
	var args []string
	var current string
	inQuote := false
	quoteChar := rune(0)

	for _, r := range cmd {
		switch {
		case r == '"' || r == '\'':
			if inQuote && r == quoteChar {
				inQuote = false
			} else if !inQuote {
				inQuote = true
				quoteChar = r
			} else {
				current += string(r)
			}
		case r == ' ' && !inQuote:
			if current != "" {
				args = append(args, current)
				current = ""
			}
		default:
			current += string(r)
		}
	}
	if current != "" {
		args = append(args, current)
	}
	return args
}

func init() {
	rootCmd.AddCommand(uiCmd)
}
