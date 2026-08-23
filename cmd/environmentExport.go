package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

func newEnvironmentExportCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var artifact, providerURL, output string
	command := &cobra.Command{Use: "export", Short: "Export a verified environment artifact as an offline archive.", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if artifact == "" || providerURL == "" || output == "" {
				return fmt.Errorf("--artifact, --provider, and --output are required")
			}
			digest, err := environmentartifact.ParseDigest(artifact)
			if err != nil {
				return err
			}
			if dependencies.newProvider == nil {
				return fmt.Errorf("environment provider dependency is unavailable")
			}
			provider, err := dependencies.newProvider(providerURL)
			if err != nil {
				return err
			}
			manifest, err := environmentlifecycle.ExportArchive(command.Context(), environmentlifecycle.ExportArchiveRequest{ArtifactDigest: digest, Provider: provider, OutputPath: output})
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(map[string]any{"artifactDigest": manifest.ArtifactDigest, "archive": output})
		},
	}
	command.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 environment artifact digest.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL.")
	command.Flags().StringVar(&output, "output", "", "Output archive path.")
	return command
}
