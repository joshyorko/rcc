package cmd

import (
	"github.com/robocorp/rcc/settings"
	"github.com/spf13/cobra"
)

var ociCmd = &cobra.Command{
	Use:   "oci",
	Short: "Group of OCI image building commands.",
	Long: `Group of commands for building and managing OCI (Open Container Initiative) images.

These commands allow you to package your robot automation, along with a resolved
Holotree environment and the RCC runtime, into a self-contained container image
that can be run without requiring Python or RCC to be installed on the target machine.

Subcommands:
  build       Build an OCI image from a robot
  dockerfile  Generate a Dockerfile for external build pipelines`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		settings.CriticalEnvironmentSettingsCheck()
	},
}

func init() {
	rootCmd.AddCommand(ociCmd)
}
