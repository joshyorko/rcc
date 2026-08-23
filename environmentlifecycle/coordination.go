package environmentlifecycle

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/buildcoord"
	"github.com/joshyorko/rcc/common"
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

func (it *CoordinatedBuilder) Build(ctx context.Context, _ buildcoord.Claim, policy buildcoord.ExecutionPolicy) (buildcoord.Artifact, error) {
	if it == nil || it.Builder == nil {
		return buildcoord.Artifact{}, fmt.Errorf("coordinated builder requires an environment builder")
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
		return buildcoord.Artifact{
			Digest:                result.ArtifactDigest.String(),
			Verified:              true,
			ClosureDigest:         result.ArtifactDigest.String(),
			Provider:              publishName,
			ProviderAuthorization: "lifecycle-commit:" + result.ArtifactDigest.String(),
			Source:                "environmentlifecycle",
		}, nil
	}
	if policy.RequiresBoundary() {
		if it.Boundary == nil {
			return buildcoord.Artifact{}, buildcoord.ErrUnenforcedBuildPolicy
		}
		artifact, err := it.Boundary.Run(ctx, policy, build)
		if err != nil {
			return buildcoord.Artifact{}, err
		}
		return it.sign(artifact)
	}
	artifact, err := build(ctx)
	if err != nil {
		return buildcoord.Artifact{}, err
	}
	return it.sign(artifact)
}

func (it *CoordinatedBuilder) sign(artifact buildcoord.Artifact) (buildcoord.Artifact, error) {
	if it.Signatures == nil {
		return artifact, nil
	}
	signatures, err := it.Signatures(artifact)
	if err != nil {
		return buildcoord.Artifact{}, fmt.Errorf("sign coordinated artifact: %w", err)
	}
	artifact.Signatures = append([]artifacttrust.Signature(nil), signatures...)
	return artifact, nil
}

var _ buildcoord.EnforcedBuilder = (*CoordinatedBuilder)(nil)
