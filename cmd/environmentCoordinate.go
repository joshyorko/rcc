package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshyorko/rcc/buildcoord"
	"github.com/spf13/cobra"
)

type coordinationResult struct {
	Key      buildcoord.BuildKey      `json:"key"`
	Claim    buildcoord.Claim         `json:"claim,omitempty"`
	Outcome  buildcoord.Outcome       `json:"outcome,omitempty"`
	Artifact buildcoord.Artifact      `json:"artifact,omitempty"`
	Items    []buildcoord.PrewarmItem `json:"items,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

func newEnvironmentCoordinateCommand() *cobra.Command {
	var root, spec, platform, builder, resolution, trust, schema, owner string
	var ttl, interval time.Duration
	var epoch uint64
	var artifactDigest, closureDigest, provider, authorization string
	var jsonOut bool
	var keys []string
	key := func() buildcoord.BuildKey {
		return buildcoord.BuildKey{SpecificationDigest: spec, Platform: platform, BuilderCompatibility: builder, ResolutionPolicy: resolution, TrustPolicy: trust, ArtifactSchema: schema}
	}
	coord := func() *buildcoord.Filesystem {
		c := buildcoord.NewFilesystem(root, nil)
		c.RequireArtifactProof = true
		c.Verifier = buildcoord.ArtifactVerifierFunc(buildcoord.VerifyArtifactProof)
		return c
	}
	write := func(cmd *cobra.Command, result coordinationResult, err error) error {
		if err != nil {
			result.Error = err.Error()
		}
		if !jsonOut {
			return fmt.Errorf("--json is required")
		}
		if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); encodeErr != nil {
			return encodeErr
		}
		return err
	}
	claimFromFlags := func() buildcoord.Claim { return buildcoord.Claim{Key: key(), Owner: owner, Epoch: epoch} }
	claim := &cobra.Command{Use: "claim", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		cl, out, err := coord().ClaimContext(cmd.Context(), key(), owner, ttl)
		if err == nil && artifactDigest != "" && out == buildcoord.Claimed {
			err = coord().Publish(cl, buildcoord.Artifact{Digest: artifactDigest, Verified: true, ClosureDigest: closureDigest, Provider: provider, ProviderAuthorization: authorization, Source: "cli"})
		}
		return write(cmd, coordinationResult{Key: key(), Claim: cl, Outcome: out}, err)
	}}
	heartbeat := &cobra.Command{Use: "heartbeat", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		return write(cmd, coordinationResult{Key: key(), Claim: claimFromFlags()}, coord().Heartbeat(claimFromFlags(), ttl))
	}}
	wait := &cobra.Command{Use: "wait", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		artifact, out, err := coord().Wait(cmd.Context(), key(), interval)
		return write(cmd, coordinationResult{Key: key(), Outcome: out, Artifact: artifact}, err)
	}}
	release := &cobra.Command{Use: "release", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		return write(cmd, coordinationResult{Key: key(), Claim: claimFromFlags()}, coord().Release(claimFromFlags()))
	}}
	prewarm := &cobra.Command{Use: "prewarm", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		request := buildcoord.PrewarmRequest{Capacity: len(keys), Wait: true}
		for _, encoded := range keys {
			request.Keys = append(request.Keys, buildcoord.BuildKey{SpecificationDigest: encoded, Platform: platform, BuilderCompatibility: builder, ResolutionPolicy: resolution, TrustPolicy: trust, ArtifactSchema: schema})
		}
		items, err := coord().Prewarm(cmd.Context(), request, func(context.Context, buildcoord.Claim) (buildcoord.Artifact, error) {
			if artifactDigest == "" {
				return buildcoord.Artifact{}, fmt.Errorf("--artifact-digest is required for CLI prewarm")
			}
			return buildcoord.Artifact{Digest: artifactDigest, Verified: true, ClosureDigest: closureDigest, Provider: provider, ProviderAuthorization: authorization, Source: "cli-prewarm"}, nil
		})
		return write(cmd, coordinationResult{Items: items}, err)
	}}
	for _, c := range []*cobra.Command{claim, heartbeat, wait, release, prewarm} {
		c.Flags().StringVar(&root, "root", "", "Coordinator state root")
		c.Flags().StringVar(&spec, "specification", "", "Environment specification digest")
		c.Flags().StringVar(&platform, "platform", "", "Target platform compatibility")
		c.Flags().StringVar(&builder, "builder", "", "Builder compatibility")
		c.Flags().StringVar(&resolution, "resolution-policy", "", "Dependency resolution policy revision")
		c.Flags().StringVar(&trust, "trust-policy", "", "Trust/build policy revision")
		c.Flags().StringVar(&schema, "artifact-schema", "", "Artifact schema and encoding features")
		c.Flags().StringVar(&owner, "owner", "", "Build owner identity")
		c.Flags().Uint64Var(&epoch, "epoch", 0, "Claim epoch")
		c.Flags().DurationVar(&ttl, "ttl", time.Minute, "Claim lease duration")
		c.Flags().DurationVar(&interval, "interval", 25*time.Millisecond, "Wait polling interval")
		c.Flags().StringVar(&artifactDigest, "artifact-digest", "", "Verified artifact digest")
		c.Flags().StringVar(&closureDigest, "closure-digest", "", "Complete artifact closure digest")
		c.Flags().StringVar(&provider, "provider", "", "Artifact provider identity")
		c.Flags().StringVar(&authorization, "provider-authorization", "", "Provider authorization proof")
		c.Flags().BoolVar(&jsonOut, "json", false, "Write JSON result")
	}
	prewarm.Flags().StringSliceVar(&keys, "key", nil, "Specification digest to prewarm (repeatable)")
	rootCmd := &cobra.Command{Use: "coordinate", Short: "Coordinate optional cold environment builds."}
	rootCmd.AddCommand(claim, heartbeat, wait, release, prewarm)
	return rootCmd
}
