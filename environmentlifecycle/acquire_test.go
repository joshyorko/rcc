package environmentlifecycle

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
)

type corruptingProvider struct {
	artifactprovider.Provider
	target environmentartifact.Digest
}

func (it corruptingProvider) GetObject(ctx context.Context, descriptor environmentartifact.Descriptor) (io.ReadCloser, error) {
	reader, err := it.Provider.GetObject(ctx, descriptor)
	if err != nil {
		return nil, err
	}
	if descriptor.Digest != it.target {
		return reader, nil
	}
	_ = reader.Close()
	return io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), int(descriptor.Size)))), nil
}

func TestAcquireVerifiedContentInstallsExactLegacyClosure(t *testing.T) {
	fixture, remote, artifactDigest := publishedFixture(t)
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false

	content, err := acquireVerifiedContent(context.Background(), artifactDigest, remote)
	if err != nil {
		t.Fatal(err)
	}
	if content.manifest.ArtifactDigest != artifactDigest || content.index.Count != 1 {
		t.Fatalf("verified content = %+v", content)
	}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(common.BlueprintHash(fixture.build.LegacyBlueprint)))
	assertFileBytes(t, catalogPath, fixture.catalogBytes)
	logicalID := content.index.Entries[0].LegacyObjectID
	assertFileBytes(t, htfs.ExactDefaultLocation(logicalID), fixture.objectBytes)
	if _, err := os.Lstat(common.HololibCompressMarker()); !os.IsNotExist(err) {
		t.Fatalf("acquisition changed compress.no state: %v", err)
	}
	local, err := artifactprovider.NewFilesystem(filepath.Join(consumerHome, "artifacts", "v1", "content"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.ResolveManifest(context.Background(), artifactDigest); err != nil {
		t.Fatalf("canonical local manifest cache is incomplete: %v", err)
	}
}

func TestAcquireRejectsIncompatibleProviderBeforeResolve(t *testing.T) {
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	digest, err := environmentartifact.ParseDigest("sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	provider := &capabilityRecordingProvider{}
	_, err = NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: provider})
	if err == nil || !strings.Contains(err.Error(), "provider does not support environment artifact v1") {
		t.Fatalf("Acquire error = %v", err)
	}
	if provider.resolveCalls != 0 {
		t.Fatalf("ResolveManifest calls = %d", provider.resolveCalls)
	}
}

type capabilityRecordingProvider struct {
	resolveCalls int
}

func (p *capabilityRecordingProvider) Capabilities(context.Context) (artifactprovider.Capabilities, error) {
	return artifactprovider.Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}}, nil
}

func (p *capabilityRecordingProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	p.resolveCalls++
	return nil, os.ErrNotExist
}

func (p *capabilityRecordingProvider) MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	return nil, nil
}
func (p *capabilityRecordingProvider) PutObject(context.Context, artifactprovider.Blob) error {
	return nil
}
func (p *capabilityRecordingProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}
func (p *capabilityRecordingProvider) CommitManifest(context.Context, []byte) error { return nil }

func TestAcquireRejectsCorruptProviderBytesBeforeLegacyInstallation(t *testing.T) {
	fixture, remote, artifactDigest := publishedFixture(t)
	manifestBytes, err := remote.ResolveManifest(context.Background(), artifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false

	_, err = acquireVerifiedContent(context.Background(), artifactDigest, corruptingProvider{
		Provider: remote, target: manifest.LegacyBlueprint.Digest,
	})
	if err == nil {
		t.Fatal("corrupt provider bytes were acquired")
	}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(common.BlueprintHash(fixture.build.LegacyBlueprint)))
	if _, statErr := os.Lstat(catalogPath); !os.IsNotExist(statErr) {
		t.Fatalf("catalog installed after verification failure: %v", statErr)
	}
}

func TestAcquireRejectsExistingConflictingLegacyObject(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	manifestBytes, err := remote.ResolveManifest(context.Background(), artifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	indexReader, err := remote.GetObject(context.Background(), manifest.ObjectIndex)
	if err != nil {
		t.Fatal(err)
	}
	indexBytes, err := io.ReadAll(indexReader)
	_ = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		t.Fatal(err)
	}
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false
	objectPath := htfs.ExactDefaultLocation(index.Entries[0].LegacyObjectID)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o750); err != nil {
		t.Fatal(err)
	}
	wantConflict := bytes.Repeat([]byte("c"), int(index.Entries[0].StoredSize))
	if err := os.WriteFile(objectPath, wantConflict, 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := acquireVerifiedContent(context.Background(), artifactDigest, remote); err == nil {
		t.Fatal("conflicting existing legacy object was accepted")
	}
	assertFileBytes(t, objectPath, wantConflict)
}

func TestAcquireRejectsSymlinkedLegacyParent(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	manifestBytes, err := remote.ResolveManifest(context.Background(), artifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	indexReader, err := remote.GetObject(context.Background(), manifest.ObjectIndex)
	if err != nil {
		t.Fatal(err)
	}
	indexBytes, err := io.ReadAll(indexReader)
	_ = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		t.Fatal(err)
	}
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false
	outside := t.TempDir()
	objectPath := htfs.ExactDefaultLocation(index.Entries[0].LegacyObjectID)
	fanout := filepath.Dir(filepath.Dir(objectPath))
	if err := os.MkdirAll(filepath.Dir(fanout), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, fanout); err != nil {
		t.Fatal(err)
	}

	if _, err := acquireVerifiedContent(context.Background(), artifactDigest, remote); err == nil {
		t.Fatal("symlinked legacy parent was traversed")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("content escaped through symlinked parent: %#v", entries)
	}
}

func TestAcquireRejectsCompressNoBeforeProviderCalls(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o750); err != nil {
		t.Fatal(err)
	}
	wantMarker := []byte("raw mode remains operator-owned\n")
	if err := os.WriteFile(common.HololibCompressMarker(), wantMarker, 0o640); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingProvider{delegate: remote}

	if _, err := acquireVerifiedContent(context.Background(), artifactDigest, recorder); err == nil {
		t.Fatal("incompatible consumer Hololib mode was accepted")
	}
	if len(recorder.events) != 0 {
		t.Fatalf("provider touched before consumer compatibility check: %#v", recorder.events)
	}
	assertFileBytes(t, common.HololibCompressMarker(), wantMarker)
}

func TestAcquireRejectsCommittedMalformedSemanticSpecification(t *testing.T) {
	fixture := newPublishFixture(t)
	inventory, err := environmentartifact.InventoryV12(environmentartifact.InventoryInput{
		CatalogPath: fixture.build.CatalogPath, LegacyBlueprint: fixture.build.LegacyBlueprint,
		ExpectedPlatform: fixture.build.Platform.RCCPlatform,
	})
	if err != nil {
		t.Fatal(err)
	}
	malformedSpecification := []byte("dependencies:\n  - python=3.11\n")
	indexDescriptor := descriptor(environmentartifact.ObjectIndexMediaType, inventory.IndexBytes)
	manifest, manifestBytes, err := environmentartifact.NewManifest(environmentartifact.ManifestInput{
		Specification: environmentartifact.Specification{
			Descriptor: descriptor(environmentartifact.SpecificationMediaType, malformedSpecification),
			SourceKind: fixture.build.SourceKind, Platform: fixture.build.Platform, Builder: fixture.build.Builder,
		},
		LegacyBlueprint: environmentartifact.LegacyBlueprint{
			Descriptor: inventory.LegacyBlueprint, LegacyBlueprintKey: inventory.LegacyBlueprintKey,
		},
		Platform: fixture.build.Platform, Builder: fixture.build.Builder,
		Catalogs: []environmentartifact.CatalogDescriptor{inventory.Catalog}, ObjectIndex: indexDescriptor,
		Requirements: environmentartifact.Requirements{
			CatalogReader: "v12", Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256", RequiredFeatures: []string{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	putBytes := func(descriptor environmentartifact.Descriptor, content []byte) {
		t.Helper()
		if err := remote.PutObject(context.Background(), artifactprovider.Blob{Descriptor: descriptor, Reader: bytes.NewReader(content)}); err != nil {
			t.Fatal(err)
		}
	}
	putBytes(manifest.Specification.Descriptor, malformedSpecification)
	putBytes(manifest.LegacyBlueprint.Descriptor, fixture.build.LegacyBlueprint)
	putBytes(manifest.Catalogs[0].Descriptor, fixture.catalogBytes)
	putBytes(manifest.ObjectIndex, inventory.IndexBytes)
	for _, entry := range inventory.Index.Entries {
		content, err := os.ReadFile(inventory.Objects[entry.StoredDigest])
		if err != nil {
			t.Fatal(err)
		}
		putBytes(environmentartifact.Descriptor{
			MediaType: "application/vnd.rcc.hololib.object.v12+gzip", Digest: entry.StoredDigest, Size: entry.StoredSize,
		}, content)
	}
	if err := remote.CommitManifest(context.Background(), manifestBytes); err != nil {
		t.Fatal(err)
	}
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false

	if _, err := acquireVerifiedContent(context.Background(), manifest.ArtifactDigest, remote); err == nil {
		t.Fatal("committed non-JSON semantic specification was acquired")
	}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), manifest.Catalogs[0].LegacyName)
	if _, statErr := os.Lstat(catalogPath); !os.IsNotExist(statErr) {
		t.Fatalf("legacy catalog installed after specification rejection: %v", statErr)
	}
}

func publishedFixture(t *testing.T) (publishFixture, *artifactprovider.Filesystem, environmentartifact.Digest) {
	t.Helper()
	fixture := newPublishFixture(t)
	remote, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := Publish(context.Background(), PublishRequest{
		RobotFile: "robot.yaml", Provider: remote, Builder: &recordingBuilder{result: fixture.build},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, remote, result.ArtifactDigest
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
