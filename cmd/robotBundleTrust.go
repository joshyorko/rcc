package cmd

import (
	"context"
	"fmt"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

// bundleTrustOptions defines the deployment-owned trust inputs for an embedded
// Environment Artifact. Strict signature verification is the default; unsigned
// local artifacts require the explicit --permissive-local migration option.
type bundleTrustOptions struct {
	metadata         metadataOptions
	trustRootsPath   string
	trustCarrierPath string
	trustCarrierType string
	strictRemote     bool
	permissiveLocal  bool
}

func (options *bundleTrustOptions) addFlags(command *cobra.Command) {
	options.metadata.addFlags(command)
	command.Flags().StringVar(&options.trustRootsPath, "trust-roots", "", "JSON map of trusted key IDs to base64 Ed25519 public keys.")
	command.Flags().StringVar(&options.trustCarrierPath, "trust-carrier", "", "Deployment-owned detached trust carrier path or URL.")
	command.Flags().StringVar(&options.trustCarrierType, "trust-carrier-type", "auto", "Trust carrier type: auto, filesystem, archive, or http.")
	command.Flags().BoolVar(&options.strictRemote, "strict-remote", false, "Require detached signature verification (the default for embedded artifacts).")
	command.Flags().BoolVar(&options.permissiveLocal, "permissive-local", false, "Explicitly allow unsigned local artifacts for controlled migration.")
}

type bundleArtifactAcquirer interface {
	Acquire(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error)
}

func (options bundleTrustOptions) acquire(ctx context.Context, digest environmentartifact.Digest) (environmentlifecycle.AcquireResult, error) {
	return options.acquireWith(ctx, digest, environmentlifecycle.NewAcquirer())
}

func (options bundleTrustOptions) acquireWith(ctx context.Context, digest environmentartifact.Digest, acquirer bundleArtifactAcquirer) (environmentlifecycle.AcquireResult, error) {
	if digest.Hex() == "" {
		return environmentlifecycle.AcquireResult{}, fmt.Errorf("bundled artifact digest is empty")
	}
	if acquirer == nil {
		return environmentlifecycle.AcquireResult{}, fmt.Errorf("bundled artifact acquirer is unavailable")
	}
	policy, err := trustPolicyForCommand(options.strictRemote, options.permissiveLocal)
	if err != nil {
		return environmentlifecycle.AcquireResult{}, err
	}
	trustRequest, acceptedKeys, err := runtimeTrustRoots(options.trustRootsPath)
	if err != nil {
		return environmentlifecycle.AcquireResult{}, err
	}
	policy.AcceptedKeys = acceptedKeys
	carrier, err := optionalEnvironmentTrustCarrier(options.trustCarrierPath, options.trustCarrierType, "")
	if err != nil {
		return environmentlifecycle.AcquireResult{}, err
	}
	if err := options.metadata.apply(&trustRequest, carrier); err != nil {
		return environmentlifecycle.AcquireResult{}, err
	}
	acquired, err := acquirer.Acquire(ctx, environmentlifecycle.AcquireRequest{
		ArtifactDigest: digest,
		TrustPolicy:    &policy,
		TrustRequest:   trustRequest,
		TrustCarrier:   carrier,
	})
	if err != nil {
		return environmentlifecycle.AcquireResult{}, err
	}
	if !acquired.Verification.Valid || acquired.Verification.Code != artifacttrust.CodeValid || acquired.Verification.ArtifactDigest != digest.String() {
		return environmentlifecycle.AcquireResult{}, fmt.Errorf("bundled artifact acquisition returned no valid trust receipt")
	}
	acquired.TrustPolicy = &policy
	acquired.TrustRequest = trustRequest
	acquired.TrustCarrier = carrier
	return acquired, nil
}

// materializationForBundledArtifact preserves the acquisition decision and all
// trust inputs so Lease can refresh policy evidence immediately before execution.
func materializationForBundledArtifact(acquired environmentlifecycle.AcquireResult) environmentlifecycle.Materialization {
	return environmentlifecycle.Materialization{
		ArtifactDigest: acquired.ArtifactDigest,
		ID:             acquired.MaterializationID,
		Path:           acquired.Path,
		CacheHit:       acquired.CacheHit,
		Verification:   acquired.Verification,
		TrustPolicy:    acquired.TrustPolicy,
		TrustRequest:   acquired.TrustRequest,
		TrustCarrier:   acquired.TrustCarrier,
	}
}
