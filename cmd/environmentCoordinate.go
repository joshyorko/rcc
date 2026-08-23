package cmd

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
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
	var backoff, buildTimeout time.Duration
	var capacity, priority, cpuLimit int
	var diskBytes, memoryBytes int64
	var network, independent, waitForClaim bool
	var quarantineRoot string
	var epoch uint64
	var artifactDigest, closureDigest, provider, authorization string
	var trustKeyID, trustPublicKey, trustSignature string
	var jsonOut bool
	var keys []string
	key := func() buildcoord.BuildKey {
		return buildcoord.BuildKey{SpecificationDigest: spec, Platform: platform, BuilderCompatibility: builder, ResolutionPolicy: resolution, TrustPolicy: trust, ArtifactSchema: schema}
	}
	coord := func() *buildcoord.Filesystem {
		c := buildcoord.NewFilesystem(root, nil)
		c.RequireArtifactProof = true
		c.Verifier = buildcoord.ArtifactVerifierFunc(func(a buildcoord.Artifact) error {
			if err := buildcoord.VerifyArtifactProof(a); err != nil {
				return err
			}
			keyBytes, err := base64.RawStdEncoding.DecodeString(trustPublicKey)
			if err != nil || len(keyBytes) != ed25519.PublicKeySize {
				return fmt.Errorf("trusted public key is required")
			}
			if trustKeyID == "" || trustSignature == "" {
				return fmt.Errorf("trusted signature is required")
			}
			sig := artifacttrust.Signature{MediaType: artifacttrust.SignatureMediaType, ArtifactDigest: a.Digest, KeyID: trustKeyID, Algorithm: "Ed25519", Signature: trustSignature}
			k := key()
			return artifacttrust.Policy{RequireSignature: true, AcceptedKeys: []string{trustKeyID}}.Evaluate(false, a.Digest, k.Platform, k.BuilderCompatibility, []artifacttrust.Signature{sig}, nil, map[string]ed25519.PublicKey{trustKeyID: keyBytes})
		})
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
		request := buildcoord.PrewarmRequest{Capacity: capacity, Priority: priority, Wait: waitForClaim, IndependentBuild: independent, DiskReservationBytes: diskBytes, Backoff: backoff, LeaseTTL: ttl, Owner: owner, Build: buildcoord.BuildRequest{Root: root, DiskBytes: diskBytes, Network: network, QuarantineRoot: quarantineRoot, CPULimit: cpuLimit, MemoryBytes: memoryBytes, Timeout: buildTimeout}}
		if request.Capacity == 0 {
			request.Capacity = len(keys)
		}
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
		c.Flags().StringVar(&trustKeyID, "trust-key-id", "", "Trusted artifact signing key ID")
		c.Flags().StringVar(&trustPublicKey, "trust-public-key", "", "Trusted Ed25519 public key (base64)")
		c.Flags().StringVar(&trustSignature, "trust-signature", "", "Artifact signature (base64)")
		c.Flags().BoolVar(&jsonOut, "json", false, "Write JSON result")
	}
	prewarm.Flags().StringSliceVar(&keys, "key", nil, "Specification digest to prewarm (repeatable)")
	prewarm.Flags().IntVar(&capacity, "capacity", 0, "Maximum concurrent builders")
	prewarm.Flags().IntVar(&priority, "priority", 0, "Prewarm priority")
	prewarm.Flags().BoolVar(&waitForClaim, "wait", true, "Wait for an existing claim")
	prewarm.Flags().BoolVar(&independent, "independent-build", false, "Build independently when another owner is active")
	prewarm.Flags().DurationVar(&backoff, "backoff", 25*time.Millisecond, "Claim retry backoff")
	prewarm.Flags().Int64Var(&diskBytes, "disk-bytes", 0, "Per-build disk reservation")
	prewarm.Flags().BoolVar(&network, "network", false, "Allow package network access")
	prewarm.Flags().IntVar(&cpuLimit, "cpu-limit", 0, "Builder CPU limit")
	prewarm.Flags().Int64Var(&memoryBytes, "memory-bytes", 0, "Builder memory limit")
	prewarm.Flags().DurationVar(&buildTimeout, "build-timeout", 0, "Builder timeout")
	prewarm.Flags().StringVar(&quarantineRoot, "quarantine-root", "", "Failed staging quarantine root")
	rootCmd := &cobra.Command{Use: "coordinate", Short: "Coordinate optional cold environment builds."}
	rootCmd.AddCommand(claim, heartbeat, wait, release, prewarm)
	return rootCmd
}
