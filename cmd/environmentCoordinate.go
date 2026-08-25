package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/buildcoord"
	"github.com/spf13/cobra"
)

type coordinationResult = buildcoord.MachineContract

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
	var priorityMap []string
	var buildCommand []string
	var readOnlyInputs []string
	key := func() buildcoord.BuildKey {
		return buildcoord.BuildKey{SpecificationDigest: spec, Platform: platform, BuilderCompatibility: builder, ResolutionPolicy: resolution, TrustPolicy: trust, ArtifactSchema: schema}
	}
	coord := func() (*buildcoord.Filesystem, error) {
		keyBytes, err := decodePublicKey(trustPublicKey)
		if err != nil {
			return nil, err
		}
		if trustKeyID == "" {
			return nil, fmt.Errorf("trusted key ID is required")
		}
		k := key()
		verifier, err := buildcoord.NewTrustVerifier(artifacttrust.Policy{RequireSignature: true, AcceptedKeys: []string{trustKeyID}, AcceptedPlatforms: []string{k.Platform}, AcceptedBuilders: []string{k.BuilderCompatibility}}, map[string]ed25519.PublicKey{trustKeyID: keyBytes}, nil, false, k.Platform, k.BuilderCompatibility)
		if err != nil {
			return nil, err
		}
		return buildcoord.NewFilesystem(root, nil, verifier)
	}
	artifactFromFlags := func(source string) (buildcoord.Artifact, error) {
		artifact := buildcoord.Artifact{Digest: artifactDigest, Verified: true, ClosureDigest: closureDigest, Provider: provider, ProviderAuthorization: authorization, Source: source}
		if trustKeyID == "" || trustSignature == "" {
			return artifact, fmt.Errorf("trusted signature is required")
		}
		artifact.Signatures = []artifacttrust.Signature{{MediaType: artifacttrust.SignatureMediaType, ArtifactDigest: buildcoord.ArtifactTrustDigest(artifact), KeyID: trustKeyID, Algorithm: "Ed25519", Signature: trustSignature}}
		return artifact, nil
	}
	write := func(cmd *cobra.Command, result coordinationResult, err error) error {
		result.SchemaVersion = buildcoord.MachineContractSchemaVersion
		result.Operation = cmd.Name()
		if err != nil {
			result.Status = "failed"
		} else if result.Status == "" {
			if result.Outcome != "" {
				result.Status = string(result.Outcome)
			} else if len(result.Items) > 0 {
				result.Status = string(result.Items[0].Status)
			} else {
				result.Status = "ok"
			}
		}
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
		coordinator, err := coord()
		if err != nil {
			return write(cmd, coordinationResult{Key: key()}, err)
		}
		cl, out, err := coordinator.ClaimContext(cmd.Context(), key(), owner, ttl)
		if err == nil && artifactDigest != "" && out == buildcoord.Claimed {
			artifact, artifactErr := artifactFromFlags("cli")
			if artifactErr != nil {
				err = artifactErr
			} else {
				err = coordinator.Publish(cl, artifact)
			}
		}
		return write(cmd, coordinationResult{Key: key(), Claim: cl, Outcome: out}, err)
	}}
	heartbeat := &cobra.Command{Use: "heartbeat", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		coordinator, err := coord()
		if err != nil {
			return write(cmd, coordinationResult{Key: key(), Claim: claimFromFlags()}, err)
		}
		return write(cmd, coordinationResult{Key: key(), Claim: claimFromFlags()}, coordinator.Heartbeat(claimFromFlags(), ttl))
	}}
	wait := &cobra.Command{Use: "wait", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		coordinator, err := coord()
		if err != nil {
			return write(cmd, coordinationResult{Key: key()}, err)
		}
		artifact, out, err := coordinator.Wait(cmd.Context(), key(), interval)
		return write(cmd, coordinationResult{Key: key(), Outcome: out, Artifact: artifact}, err)
	}}
	release := &cobra.Command{Use: "release", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		coordinator, err := coord()
		if err != nil {
			return write(cmd, coordinationResult{Key: key(), Claim: claimFromFlags()}, err)
		}
		return write(cmd, coordinationResult{Key: key(), Claim: claimFromFlags()}, coordinator.Release(claimFromFlags()))
	}}
	prewarm := &cobra.Command{Use: "prewarm", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		coordinator, err := coord()
		if err != nil {
			return write(cmd, coordinationResult{Key: key()}, err)
		}
		request := buildcoord.PrewarmRequest{Capacity: capacity, Priority: priority, Wait: waitForClaim, IndependentBuild: independent, DiskReservationBytes: diskBytes, Backoff: backoff, LeaseTTL: ttl, Owner: owner, Build: buildcoord.BuildRequest{Root: root, DiskBytes: diskBytes, Network: network, QuarantineRoot: quarantineRoot, CPULimit: cpuLimit, MemoryBytes: memoryBytes, Timeout: buildTimeout}}
		if request.Capacity == 0 {
			request.Capacity = len(keys)
		}
		for _, encoded := range keys {
			request.Keys = append(request.Keys, buildcoord.BuildKey{SpecificationDigest: encoded, Platform: platform, BuilderCompatibility: builder, ResolutionPolicy: resolution, TrustPolicy: trust, ArtifactSchema: schema})
		}
		request.Priorities, err = parsePriorityMap(priorityMap, request.Keys, priority)
		if err != nil {
			return write(cmd, coordinationResult{Key: key()}, err)
		}
		executor, err := buildcoord.NewCommandExecutor(buildCommand)
		if err != nil {
			return write(cmd, coordinationResult{Items: nil}, err)
		}
		executor.ReadOnlyInputs = append([]string(nil), readOnlyInputs...)
		items, err := coordinator.PrewarmWithExecutor(cmd.Context(), request, executor)
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
	prewarm.Flags().StringSliceVar(&buildCommand, "build-command", nil, "RCC-owned staged build command that writes one Artifact JSON record")
	prewarm.Flags().StringSliceVar(&readOnlyInputs, "read-only-input", nil, "Explicit RCC cache/provider path to mount read-only in the build namespace")
	prewarm.Flags().IntVar(&capacity, "capacity", 0, "Maximum concurrent builders")
	prewarm.Flags().IntVar(&priority, "priority", 0, "Prewarm priority")
	prewarm.Flags().StringSliceVar(&priorityMap, "priority-map", nil, "Per-key priority entries in key=priority form")
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

func decodePublicKey(value string) (ed25519.PublicKey, error) {
	if value == "" {
		return nil, fmt.Errorf("trusted public key is required")
	}
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.StdEncoding.DecodeString(value)
	}
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("trusted public key must be a base64 Ed25519 key")
	}
	return ed25519.PublicKey(decoded), nil
}

func parsePriorityMap(entries []string, keys []buildcoord.BuildKey, fallback int) (map[string]int, error) {
	priorities := make(map[string]int, len(keys))
	for _, key := range keys {
		priorities[key.ID()] = fallback
	}
	for _, entry := range entries {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("priority entry %q must be key=integer", entry)
		}
		value, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("priority entry %q: %w", entry, err)
		}
		name := strings.TrimSpace(parts[0])
		matched := false
		for _, key := range keys {
			if name == key.ID() || name == key.SpecificationDigest {
				priorities[key.ID()] = value
				matched = true
			}
		}
		if !matched {
			return nil, fmt.Errorf("priority entry %q does not identify a requested build key", entry)
		}
	}
	return priorities, nil
}
