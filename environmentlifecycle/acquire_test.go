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
