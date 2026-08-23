package environmentlifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
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
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}, Provider: provider}
	if _, err := executor.Build(context.Background(), buildcoord.Claim{}, buildcoord.ExecutionPolicy{Root: t.TempDir(), CPULimit: 1}); !errors.Is(err, buildcoord.ErrUnenforcedBuildPolicy) {
		t.Fatalf("resource policy without boundary: %v", err)
	}
}

func TestCoordinatedBuilderDelegatesPolicyToProcessBoundary(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var received buildcoord.ExecutionPolicy
	executor := &CoordinatedBuilder{RobotFile: "fixtures/robot.yaml", Builder: &recordingBuilder{result: fixture.build}, Provider: provider, Boundary: buildcoord.ProcessBoundaryFunc(func(ctx context.Context, policy buildcoord.ExecutionPolicy, build func(context.Context) (buildcoord.Artifact, error)) (buildcoord.Artifact, error) {
		received = policy
		return build(ctx)
	})}
	if _, err := executor.Build(context.Background(), buildcoord.Claim{}, buildcoord.ExecutionPolicy{Root: t.TempDir(), CPULimit: 2}); err != nil {
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
