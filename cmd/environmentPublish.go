package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

type environmentPublishResult struct {
	ArtifactDigest      environmentartifact.Digest `json:"artifactDigest"`
	SpecificationDigest environmentartifact.Digest `json:"specificationDigest"`
	LegacyBlueprintKey  string                     `json:"legacyBlueprintKey"`
	ObjectCount         int                        `json:"objectCount"`
	UploadedBytes       int64                      `json:"uploadedBytes"`
	ReusedBytes         int64                      `json:"reusedBytes"`
}

func newEnvironmentPublishCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var robotFile, providerURL string
	var jsonOutput bool
	command := &cobra.Command{
		Use:          "publish",
		Short:        "Build and publish a portable environment artifact.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if !jsonOutput {
				return fmt.Errorf("--json is required")
			}
			if robotFile == "" || providerURL == "" {
				return fmt.Errorf("--robot and --provider are required")
			}
			if dependencies.newProvider == nil || dependencies.publish == nil {
				return fmt.Errorf("environment publish dependencies are unavailable")
			}
			provider, err := dependencies.newProvider(providerURL)
			if err != nil {
				return fmt.Errorf("open environment provider: %w", err)
			}
			result, err := dependencies.publish(command.Context(), environmentlifecycle.PublishRequest{
				RobotFile: robotFile, Provider: provider, Builder: environmentlifecycle.CurrentRCCBuilder{},
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(environmentPublishResult{
				ArtifactDigest: result.ArtifactDigest, SpecificationDigest: result.SpecificationDigest,
				LegacyBlueprintKey: result.LegacyBlueprintKey, ObjectCount: result.ObjectCount,
				UploadedBytes: result.UploadedBytes, ReusedBytes: result.ReusedBytes,
			})
		},
	}
	command.Flags().StringVar(&robotFile, "robot", "", "Path to robot.yaml.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL.")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON result object to stdout.")
	return command
}
