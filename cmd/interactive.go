package cmd

import (
	"github.com/joshyorko/rcc/interactive"
	"github.com/joshyorko/rcc/pretty"
	"github.com/spf13/cobra"
)

var interactiveCmd = &cobra.Command{
	Use:     "interactive",
	Aliases: []string{"tui"},
	Short:   "Launch interactive TUI mode.",
	Long: `Launch an interactive terminal user interface for navigating
and managing RCC commands, robots, and environments.

Navigation:
  1-5        Switch between views (Home, Commands, Robots, Envs, Logs)
  j/k        Move down/up
  h/l        Collapse/expand (in tree views)
  Enter      Select item
  q          Quit

Views:
  1 - Home         System information and quick actions
  2 - Commands     Browse available RCC commands
  3 - Robots       Detect and manage robots in current directory
  4 - Environments View and manage holotree environments
  5 - Logs         View activity log`,
	Run: func(cmd *cobra.Command, args []string) {
		err := interactive.Run()
		if err != nil {
			pretty.Exit(1, "Interactive mode error: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(interactiveCmd)
}
