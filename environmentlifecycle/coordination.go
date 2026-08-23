package environmentlifecycle

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/buildcoord"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

// CoordinatedBuilder is the RCC-owned bridge between build coordination and
// the real Environment Artifact lifecycle. It deliberately accepts only a
// provider and Builder, never an Actions deployment object.
type CoordinatedBuilder struct {
	RobotFile          string
	Builder            Builder
	Provider           artifactprovider.Provider
	ProviderName       string
	LocalProviderRoot  string
	AllowLocalFallback bool
	Boundary           buildcoord.ProcessBoundary
	Signatures         func(buildcoord.Artifact) ([]artifacttrust.Signature, error)
}

func (it *CoordinatedBuilder) Build(ctx context.Context, claim buildcoord.Claim, policy buildcoord.ExecutionPolicy) (buildcoord.Artifact, error) {
	if it == nil || it.Builder == nil {
		return buildcoord.Artifact{}, fmt.Errorf("coordinated builder requires an environment builder")
	}
	coordinatorPublication := claim.Owner != "" || claim.Epoch != 0
	if coordinatorPublication {
		if err := buildcoord.ValidateExecutionStaging(claim, policy); err != nil {
			return buildcoord.Artifact{}, err
		}
		if it.Signatures == nil {
			return buildcoord.Artifact{}, buildcoord.ErrUnverifiedArtifact
		}
	} else if policy.RequiresBoundary() {
		if err := buildcoord.ValidateExecutionStaging(claim, policy); err != nil {
			return buildcoord.Artifact{}, err
		}
	}
	provider := it.Provider
	providerName := it.ProviderName
	if provider == nil {
		if !it.AllowLocalFallback {
			return buildcoord.Artifact{}, fmt.Errorf("coordinated artifact provider is unavailable")
		}
		root := it.LocalProviderRoot
		if root == "" {
			root = filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
		}
		var err error
		provider, err = artifactprovider.NewFilesystem(root)
		if err != nil {
			return buildcoord.Artifact{}, fmt.Errorf("initialize local fallback provider: %w", err)
		}
		providerName = "local-filesystem"
	}
	if providerName == "" {
		providerName = "configured-provider"
	}
	build := func(buildCtx context.Context) (buildcoord.Artifact, error) {
		publishProvider, publishName := provider, providerName
		result, err := Publish(buildCtx, PublishRequest{RobotFile: it.RobotFile, Provider: publishProvider, Builder: it.Builder})
		if err != nil && it.AllowLocalFallback && it.Provider != nil {
			fallbackRoot := it.LocalProviderRoot
			if fallbackRoot == "" {
				fallbackRoot = filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
			}
			fallback, fallbackErr := artifactprovider.NewFilesystem(fallbackRoot)
			if fallbackErr == nil {
				publishProvider, publishName = fallback, "local-filesystem"
				result, err = Publish(buildCtx, PublishRequest{RobotFile: it.RobotFile, Provider: publishProvider, Builder: it.Builder})
			}
		}
		if err != nil {
			return buildcoord.Artifact{}, err
		}
		artifact := buildcoord.Artifact{
			Digest:                result.ArtifactDigest.String(),
			Verified:              true,
			ClosureDigest:         result.ArtifactDigest.String(),
			Provider:              publishName,
			ProviderAuthorization: "lifecycle-commit:" + result.ArtifactDigest.String(),
			Source:                "environmentlifecycle",
			Completion:            &buildcoord.CompletionReceipt{ArtifactDigest: result.ArtifactDigest.String(), Provider: publishName, ManifestCommitted: true, ObjectsVerified: true, Lifecycle: "environmentlifecycle"},
		}
		return artifact, nil
	}
	if policy.RequiresBoundary() {
		if it.Boundary == nil {
			return buildcoord.Artifact{}, buildcoord.ErrUnenforcedBuildPolicy
		}
		artifact, err := it.Boundary.Run(ctx, policy, build)
		if err != nil {
			return buildcoord.Artifact{}, err
		}
		return it.sign(artifact, coordinatorPublication)
	}
	artifact, err := build(ctx)
	if err != nil {
		return buildcoord.Artifact{}, err
	}
	return it.sign(artifact, coordinatorPublication)
}

func (it *CoordinatedBuilder) sign(artifact buildcoord.Artifact, required bool) (buildcoord.Artifact, error) {
	if it.Signatures == nil {
		if required {
			return buildcoord.Artifact{}, buildcoord.ErrUnverifiedArtifact
		}
		return artifact, nil
	}
	signatures, err := it.Signatures(artifact)
	if err != nil {
		return buildcoord.Artifact{}, fmt.Errorf("sign coordinated artifact: %w", err)
	}
	if len(signatures) == 0 {
		return buildcoord.Artifact{}, buildcoord.ErrUnverifiedArtifact
	}
	subject := buildcoord.ArtifactTrustDigest(artifact)
	for _, signature := range signatures {
		if signature.ArtifactDigest != subject || signature.KeyID == "" {
			return buildcoord.Artifact{}, buildcoord.ErrUnverifiedArtifact
		}
	}
	artifact.Signatures = append([]artifacttrust.Signature(nil), signatures...)
	return artifact, nil
}

// VerifyCompletion reads the committed Manifest back from the authoritative
// provider. This is separate from the receipt carried by an Artifact so
// coordinator-loss fallback cannot trust a builder's own ready claim.
func (it *CoordinatedBuilder) VerifyCompletion(ctx context.Context, artifact buildcoord.Artifact) error {
	if err := buildcoord.ValidateAuthoritativeCompletion(artifact); err != nil {
		return err
	}
	provider := it.Provider
	if artifact.Provider == "local-filesystem" || provider == nil {
		root := it.LocalProviderRoot
		if root == "" {
			root = filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
		}
		var err error
		provider, err = artifactprovider.NewFilesystem(root)
		if err != nil {
			return err
		}
	}
	digest, err := environmentartifact.ParseDigest(artifact.Digest)
	if err != nil {
		return err
	}
	manifestBytes, err := provider.ResolveManifest(ctx, digest)
	if err != nil {
		return fmt.Errorf("resolve committed manifest: %w", err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		return fmt.Errorf("decode committed manifest: %w", err)
	}
	if manifest.ArtifactDigest != digest {
		return fmt.Errorf("committed manifest digest mismatch")
	}
	return nil
}

var _ buildcoord.EnforcedBuilder = (*CoordinatedBuilder)(nil)
var _ buildcoord.AuthoritativeCompletionVerifier = (*CoordinatedBuilder)(nil)
