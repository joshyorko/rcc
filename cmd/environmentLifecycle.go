package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

type environmentLifecycleCommandDependencies struct {
	newProvider func(string) (artifactprovider.Provider, error)
	repair      func(context.Context, environmentartifact.Digest, artifactprovider.Provider) (environmentlifecycle.RepairReport, error)
}

func newEnvironmentLifecycleCommand() *cobra.Command {
	return newEnvironmentLifecycleCommandWithDependencies(environmentLifecycleCommandDependencies{newProvider: newProviderReference, repair: environmentlifecycle.RepairFromProvider})
}

func newEnvironmentLifecycleCommandWithDependencies(deps environmentLifecycleCommandDependencies) *cobra.Command {
	command := &cobra.Command{Use: "lifecycle", Short: "Inspect and repair local environment lifecycle state.", Args: cobra.NoArgs}
	for _, name := range []string{"inspect", "verify", "repair"} {
		n := name
		var artifact, providerRef string
		var jsonOutput bool
		c := &cobra.Command{Use: n, Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
			if !jsonOutput {
				return fmt.Errorf("--json is required")
			}
			d, e := environmentartifact.ParseDigest(artifact)
			if e != nil {
				return e
			}
			var v any
			switch n {
			case "inspect":
				v, e = environmentlifecycle.Inspect(cmd.Context(), d)
			case "verify":
				v, e = environmentlifecycle.Verify(cmd.Context(), d)
			default:
				var p artifactprovider.Provider
				if providerRef != "" {
					p, e = deps.newProvider(providerRef)
					if e != nil {
						return e
					}
				}
				if p == nil {
					v, e = environmentlifecycle.Repair(cmd.Context(), d)
				} else {
					v, e = deps.repair(cmd.Context(), d, p)
				}
			}
			if e != nil {
				return e
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(v)
		}}
		c.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 artifact digest.")
		c.Flags().StringVar(&providerRef, "provider", "", "Environment artifact provider reference for provider-backed repair.")
		c.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON result object.")
		command.AddCommand(c)
	}
	return command
}
