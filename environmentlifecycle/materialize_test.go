package environmentlifecycle

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
)

type failOnTouchProvider struct{ t *testing.T }

func (it failOnTouchProvider) fail() {
	it.t.Helper()
	it.t.Fatal("warm acquisition touched the provider")
}

func (it failOnTouchProvider) Capabilities(context.Context) (artifactprovider.Capabilities, error) {
	it.fail()
	return artifactprovider.Capabilities{}, nil
}
func (it failOnTouchProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	it.fail()
	return nil, nil
}
func (it failOnTouchProvider) MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	it.fail()
	return nil, nil
}
func (it failOnTouchProvider) PutObject(context.Context, artifactprovider.Blob) error {
	it.fail()
	return nil
}
func (it failOnTouchProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	it.fail()
	return nil, nil
}
func (it failOnTouchProvider) CommitManifest(context.Context, []byte) error {
	it.fail()
	return nil
}

func TestMaterializationUsesPortableV12CatalogWithoutChangingArtifactBytes(t *testing.T) {
	fixture, remote, artifactDigest := publishedFixture(t)
	producer, err := htfs.LoadPortableCatalog(fixture.build.CatalogPath)
	if err != nil {
		t.Fatal(err)
	}
	producerPath := producer.Root().Path
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false
	provider := &recordingProvider{delegate: remote}

	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactDigest != artifactDigest || result.CacheHit != CacheProvider {
		t.Fatalf("acquire result = %+v", result)
	}
	if result.Path == producerPath || filepath.Dir(result.Path) != common.HolotreeLocation() {
		t.Fatalf("materialization was not rebased from %q to consumer home: %q", producerPath, result.Path)
	}
	assertFileBytes(t, filepath.Join(result.Path, "python"), []byte("immutable python bytes"))
	if info, err := os.Lstat(filepath.Join(result.Path, "python")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("materialized Python is not a regular file: %v, %v", info, err)
	}
	assertFileBytes(t, filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(common.BlueprintHash(fixture.build.LegacyBlueprint))), fixture.catalogBytes)
	if len(provider.events) == 0 {
		t.Fatal("cold acquisition made no provider requests")
	}
}

func TestWarmAcquireDoesNotTouchProviderOrBuilder(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}

	second, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: failOnTouchProvider{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ArtifactDigest != first.ArtifactDigest || second.MaterializationID != first.MaterializationID || second.Path != first.Path {
		t.Fatalf("warm result changed identity: first %+v, second %+v", first, second)
	}
	if second.CacheHit != CacheLocalMaterialization {
		t.Fatalf("warm cache provenance = %q", second.CacheHit)
	}
	if got, err := os.ReadFile(filepath.Join(second.Path, "python")); err != nil || !bytes.Equal(got, []byte("immutable python bytes")) {
		t.Fatalf("warm materialization is invalid: %q, %v", got, err)
	}
}

func TestMaterializationFailureNeverPublishesReadyRecord(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	content, err := acquireVerifiedContent(context.Background(), artifactDigest, remote)
	if err != nil {
		t.Fatal(err)
	}
	legacyID := content.index.Entries[0].LegacyObjectID
	if err := os.WriteFile(htfs.ExactDefaultLocation(legacyID), []byte("corrupt after verification"), 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := NewLocalMaterializer().Materialize(context.Background(), content.manifest); err == nil {
		t.Fatal("materialization succeeded with corrupt local legacy content")
	}
	if _, err := readReadyRecord(artifactDigest); err == nil {
		t.Fatal("failed materialization published a ready record")
	}
}

func TestWarmAcquireRepairsNonExecutablePythonWithoutProvider(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(first.Path, "python")
	if err := os.Chmod(python, 0o640); err != nil {
		t.Fatal(err)
	}

	second, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: failOnTouchProvider{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(second.Path, "python"))
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("warm acquire retained non-executable Python: %v, %v", info, err)
	}
}

func TestWarmAcquireAndExecutionRejectSymlinkedPythonParent(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	acquirer := NewAcquirer()
	first, err := acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "python"), []byte("#!/bin/sh\nexit 0\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(first.Path, "bin")); err != nil {
		t.Fatal(err)
	}
	if candidate, err := materializedPython(first.Path); err == nil {
		t.Fatalf("component-wise executable validation trusted %q", candidate)
	}
	if _, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: failOnTouchProvider{t: t},
	}); err == nil {
		t.Fatal("warm acquire trusted Python through a symlinked parent")
	}
	materializer := NewLocalMaterializer()
	materialization := Materialization{
		ArtifactDigest: first.ArtifactDigest, ID: first.MaterializationID, Path: first.Path, CacheHit: first.CacheHit,
	}
	lease, err := materializer.Lease(context.Background(), materialization)
	if err != nil {
		t.Fatal(err)
	}
	defer materializer.Release(context.Background(), lease)
	if _, err := materializer.ExecutionHandle(context.Background(), lease, []string{"python", "-V"}); err == nil {
		t.Fatal("execution handle trusted Python through a symlinked parent")
	}
}
