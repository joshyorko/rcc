package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

type environmentAcquireResult struct {
	ArtifactDigest    environmentartifact.Digest           `json:"artifactDigest"`
	MaterializationID string                               `json:"materializationId"`
	Path              string                               `json:"path"`
	CacheHit          environmentlifecycle.CacheProvenance `json:"cacheHit"`
}

func newEnvironmentAcquireCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var artifact, providerURL string
	var strictRemote, permissiveLocal bool
	var jsonOutput bool
	command := &cobra.Command{
		Use:          "acquire",
		Short:        "Verify and materialize a portable environment artifact.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
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
			if dependencies.acquire == nil {
				return fmt.Errorf("environment acquire dependency is unavailable")
			}
			policy := artifacttrust.Policy{}
			if strictRemote {
				policy.Mode = artifacttrust.StrictRemote
			}
			if permissiveLocal {
				policy.Mode = artifacttrust.PermissiveLocal
			}
			var trustCarrier artifacttrust.Carrier
			if strictRemote && providerURL != "" {
				trustCarrier = &artifacttrust.HTTPCarrier{BaseURL: providerURL}
			}
			result, err := dependencies.acquire(command.Context(), environmentlifecycle.AcquireRequest{
				ArtifactDigest: digest, Provider: provider, TrustPolicy: &policy, TrustCarrier: trustCarrier,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(environmentAcquireResult{
				ArtifactDigest: result.ArtifactDigest, MaterializationID: result.MaterializationID,
				Path: result.Path, CacheHit: result.CacheHit,
			})
		},
	}
	command.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 environment artifact digest.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL; optional for local-ready artifacts.")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON result object to stdout.")
	command.Flags().BoolVar(&strictRemote, "strict-remote", false, "Require detached signatures before acquisition.")
	command.Flags().BoolVar(&permissiveLocal, "permissive-local", false, "Explicitly allow unsigned local artifacts.")
	return command
}

func optionalEnvironmentProvider(value string, constructor func(string) (artifactprovider.Provider, error)) (artifactprovider.Provider, error) {
	if value == "" {
		return nil, nil
	}
	if constructor == nil {
		return nil, fmt.Errorf("environment provider dependency is unavailable")
	}
	provider, err := constructor(value)
	if err != nil {
		return nil, fmt.Errorf("open environment provider: %w", err)
	}
	return provider, nil
}
