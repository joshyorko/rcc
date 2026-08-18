package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

type environmentExecResult struct {
	ArtifactDigest    environmentartifact.Digest           `json:"artifactDigest"`
	MaterializationID string                               `json:"materializationId"`
	Path              string                               `json:"path"`
	CacheHit          environmentlifecycle.CacheProvenance `json:"cacheHit"`
	ExitCode          int                                  `json:"exitCode"`
}

func newEnvironmentExecCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var artifact, providerURL string
	var jsonOutput bool
	command := &cobra.Command{
		Use:          "exec -- <command> [args...]",
		Short:        "Execute a command with a process-scoped environment lease.",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(command *cobra.Command, arguments []string) error {
			if command.ArgsLenAtDash() != 0 {
				return fmt.Errorf("env exec requires command arguments after --")
			}
			if !jsonOutput {
				return fmt.Errorf("--json is required")
			}
			digest, err := environmentartifact.ParseDigest(artifact)
			if err != nil {
				return err
			}
			provider, err := optionalEnvironmentProvider(providerURL, dependencies.newProvider)
			if err != nil {
				return err
			}
			if dependencies.acquire == nil || dependencies.execute == nil || dependencies.materializer == nil {
				return fmt.Errorf("environment execution dependencies are unavailable")
			}
			acquired, err := dependencies.acquire(command.Context(), environmentlifecycle.AcquireRequest{
				ArtifactDigest: digest, Provider: provider,
			})
			if err != nil {
				return err
			}
			materialization := environmentlifecycle.Materialization{
				ArtifactDigest: acquired.ArtifactDigest, ID: acquired.MaterializationID,
				Path: acquired.Path, CacheHit: acquired.CacheHit,
			}
			_, child, err := dependencies.execute(command.Context(), dependencies.materializer(), materialization, arguments)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(command.OutOrStdout()).Encode(environmentExecResult{
				ArtifactDigest: acquired.ArtifactDigest, MaterializationID: acquired.MaterializationID,
				Path: acquired.Path, CacheHit: acquired.CacheHit, ExitCode: child.ExitCode,
			}); err != nil {
				return err
			}
			if child.ExitCode != 0 {
				panic(common.ExitCode{Code: child.ExitCode})
			}
			return nil
		},
	}
	command.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 environment artifact digest.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL; optional for local-ready artifacts.")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON result object to stdout.")
	return command
}
