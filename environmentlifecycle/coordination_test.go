package environmentlifecycle

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/buildcoord"
)

func TestCoordinatedBuilderUsesLifecycleBuildAndPublish(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	builder := &recordingBuilder{result: fixture.build}
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: builder, Provider: provider, ProviderName: "fixture-provider"}
	artifact, err := executor.Build(context.Background(), buildcoord.Claim{}, buildcoord.ExecutionPolicy{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Verified || artifact.Digest == "" || artifact.ClosureDigest != artifact.Digest || artifact.Provider != "fixture-provider" {
		t.Fatalf("lifecycle artifact: %#v", artifact)
	}
	if len(builder.calls) != 1 || builder.calls[0] != "fixtures/robot.yaml" {
		t.Fatalf("builder calls: %#v", builder.calls)
	}
	if err := executor.VerifyCompletion(context.Background(), artifact); err != nil {
		t.Fatalf("authoritative completion: %v", err)
	}
}

func TestCoordinatedBuilderFailsClosedWhenProviderIsUnavailable(t *testing.T) {
	fixture := newPublishFixture(t)
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}}
	if _, err := executor.Build(context.Background(), buildcoord.Claim{}, buildcoord.ExecutionPolicy{Root: t.TempDir()}); err == nil {
		t.Fatal("provider outage accepted without local fallback")
	}
}

func TestCoordinatedBuilderUsesExplicitLocalFallbackProvider(t *testing.T) {
	fixture := newPublishFixture(t)
	providerRoot := t.TempDir()
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}, LocalProviderRoot: providerRoot, AllowLocalFallback: true}
	artifact, err := executor.Build(context.Background(), buildcoord.Claim{}, buildcoord.ExecutionPolicy{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Provider != "local-filesystem" || !artifact.Verified {
		t.Fatalf("fallback artifact: %#v", artifact)
	}
	if _, err := artifactprovider.NewFilesystem(providerRoot); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatedBuilderRequiresProcessBoundaryForResourcePolicy(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staging, cleanup, err := buildcoord.PrepareStaging(buildcoord.BuildRequest{Root: t.TempDir(), CPULimit: 1}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}, Provider: provider}
	if _, err := executor.Build(context.Background(), buildcoord.Claim{Staging: staging}, buildcoord.ExecutionPolicy{Root: staging, CPULimit: 1}); !errors.Is(err, buildcoord.ErrUnenforcedBuildPolicy) {
		t.Fatalf("resource policy without boundary: %v", err)
	}
}

func TestCoordinatedBuilderDelegatesPolicyToProcessBoundary(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staging, cleanup, err := buildcoord.PrepareStaging(buildcoord.BuildRequest{Root: t.TempDir(), CPULimit: 2}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	var received buildcoord.ExecutionPolicy
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}, Provider: provider, Boundary: buildcoord.ProcessBoundaryFunc(func(ctx context.Context, policy buildcoord.ExecutionPolicy, build func(context.Context) (buildcoord.Artifact, error)) (buildcoord.Artifact, error) {
		received = policy
		artifact, err := build(ctx)
		if err == nil {
			artifact.Execution = &buildcoord.ExecutionReceipt{StagingRoot: policy.Root, PolicyDigest: policy.Digest(), ConfinementPID: 1, MountNamespace: 1, ParentMountNamespace: 2, CPULimit: policy.CPULimit, MemoryBytes: policy.MemoryBytes, Timeout: policy.Timeout, NetworkIsolated: true, CredentialsExcluded: true, FilesystemRestricted: true}
		}
		return artifact, err
	})}
	if _, err := executor.Build(context.Background(), buildcoord.Claim{Staging: staging}, buildcoord.ExecutionPolicy{Root: staging, CPULimit: 2}); err != nil {
		t.Fatal(err)
	}
	if received.CPULimit != 2 {
		t.Fatalf("boundary policy: %#v", received)
	}
}

func TestCoordinatedBuilderRetriesTrustedLocalProviderAfterProviderOutage(t *testing.T) {
	fixture := newPublishFixture(t)
	remote, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	localRoot := t.TempDir()
	executor := &CoordinatedBuilder{
		RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build},
		Provider:          &recordingProvider{delegate: remote, commitErr: errors.New("provider unavailable")},
		LocalProviderRoot: localRoot, AllowLocalFallback: true,
	}
	artifact, err := executor.Build(context.Background(), buildcoord.Claim{}, buildcoord.ExecutionPolicy{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Provider != "local-filesystem" {
		t.Fatalf("fallback provider: %#v", artifact)
	}
}

func TestCoordinatedBuilderRequiresSignerForCoordinatorPublication(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staging, cleanup, err := buildcoord.PrepareStaging(buildcoord.BuildRequest{Root: t.TempDir()}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}, Provider: provider, ProviderName: "fixture-provider"}
	_, err = executor.Build(context.Background(), buildcoord.Claim{Owner: "owner", Epoch: 1, Staging: staging}, buildcoord.ExecutionPolicy{Root: staging})
	if !errors.Is(err, buildcoord.ErrUnverifiedArtifact) {
		t.Fatalf("unsigned coordinator publication accepted: %v", err)
	}
}

func TestCoordinatedBuilderSignedPublicationPassesMandatoryTrustVerifier(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := buildcoord.NewTrustVerifier(artifacttrust.Policy{RequireSignature: true, AcceptedKeys: []string{"builder-key"}}, map[string]ed25519.PublicKey{"builder-key": public}, nil, false, "linux_amd64", "builder-v1")
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := buildcoord.NewFilesystem(t.TempDir(), nil, verifier)
	if err != nil {
		t.Fatal(err)
	}
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}, Provider: provider, ProviderName: "fixture-provider", Signatures: func(artifact buildcoord.Artifact) ([]artifacttrust.Signature, error) {
		signature, err := artifacttrust.Sign(buildcoord.ArtifactTrustDigest(artifact), "builder-key", private)
		return []artifacttrust.Signature{signature}, err
	}}
	key := buildcoord.BuildKey{SpecificationDigest: "sha256:" + strings.Repeat("a", 64), Platform: "linux_amd64", BuilderCompatibility: "builder-v1"}
	items, err := coordinator.PrewarmWithExecutor(context.Background(), buildcoord.PrewarmRequest{Keys: []buildcoord.BuildKey{key}, Capacity: 1, Build: buildcoord.BuildRequest{Root: t.TempDir()}}, executor)
	if err != nil || len(items) != 1 || items[0].Status != buildcoord.PrewarmReady {
		t.Fatalf("signed prewarm: %#v %v", items, err)
	}
	artifact, ok, err := coordinator.Committed(key)
	if err != nil || !ok || artifact.Completion == nil || len(artifact.Signatures) != 1 {
		t.Fatalf("trusted committed artifact: %#v %v %v", artifact, ok, err)
	}
}
