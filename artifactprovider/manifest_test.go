package artifactprovider

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/joshyorko/rcc/environmentartifact"
)

type providerFixture struct {
	manifest      environmentartifact.Manifest
	manifestBytes []byte
	blobs         []Blob
}

func newProviderFixture(t *testing.T) providerFixture {
	t.Helper()
	platform := environmentartifact.Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}
	builder := environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: "v0.test", CompatibilityKey: "v12-gzip-sha256"}
	contents := struct {
		specification []byte
		blueprint     []byte
		catalog       []byte
		object        []byte
	}{[]byte(`{"dependencies":["python=3.11"]}`), []byte("dependencies:\n  - python=3.11\n"), []byte("immutable catalog bytes"), []byte("immutable gzip object bytes")}
	descriptor := func(mediaType string, content []byte) environmentartifact.Descriptor {
		return environmentartifact.Descriptor{MediaType: mediaType, Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
	}
	objectDescriptor := descriptor("application/vnd.rcc.hololib.object.v12+gzip", contents.object)
	_, indexBytes, err := environmentartifact.NewObjectIndex([]environmentartifact.ObjectEntry{{
		LegacyObjectID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		StoredDigest:   objectDescriptor.Digest, StoredSize: objectDescriptor.Size, LogicalSize: 99,
		Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256",
	}})
	if err != nil {
		t.Fatal(err)
	}
	indexDescriptor := descriptor(environmentartifact.ObjectIndexMediaType, indexBytes)
	manifest, manifestBytes, err := environmentartifact.NewManifest(environmentartifact.ManifestInput{
		Specification:   environmentartifact.Specification{Descriptor: descriptor(environmentartifact.SpecificationMediaType, contents.specification), SourceKind: "robot.yaml", Platform: platform, Builder: builder},
		LegacyBlueprint: environmentartifact.LegacyBlueprint{Descriptor: descriptor(environmentartifact.LegacyBlueprintMediaType, contents.blueprint), LegacyBlueprintKey: "0123456789abcdef"},
		Platform:        platform, Builder: builder,
		Catalogs:     []environmentartifact.CatalogDescriptor{{Descriptor: descriptor(environmentartifact.CatalogV12MediaType, contents.catalog), LegacyName: "0123456789abcdefv12.linux_amd64"}},
		ObjectIndex:  indexDescriptor,
		Requirements: environmentartifact.Requirements{CatalogReader: "v12", Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256", RequiredFeatures: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return providerFixture{manifest: manifest, manifestBytes: manifestBytes, blobs: []Blob{
		{Descriptor: manifest.Specification.Descriptor, Reader: bytes.NewReader(contents.specification)},
		{Descriptor: manifest.LegacyBlueprint.Descriptor, Reader: bytes.NewReader(contents.blueprint)},
		{Descriptor: manifest.Catalogs[0].Descriptor, Reader: bytes.NewReader(contents.catalog)},
		{Descriptor: indexDescriptor, Reader: bytes.NewReader(indexBytes)},
		{Descriptor: objectDescriptor, Reader: bytes.NewReader(contents.object)},
	}}
}

func putFixtureBlob(t *testing.T, provider *Filesystem, blob Blob) {
	t.Helper()
	content, err := io.ReadAll(blob.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(content)}); err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemCommitManifestPublishesOnlyCompleteClosure(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs[:len(fixture.blobs)-1] {
		putFixtureBlob(t, provider, blob)
	}

	if err := provider.CommitManifest(context.Background(), fixture.manifestBytes); err == nil {
		t.Fatal("manifest with a missing referenced object committed")
	}
	if _, err := provider.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err == nil {
		t.Fatal("incomplete manifest became resolvable")
	}

	putFixtureBlob(t, provider, fixture.blobs[len(fixture.blobs)-1])
	if err := provider.CommitManifest(context.Background(), fixture.manifestBytes); err != nil {
		t.Fatal(err)
	}
	resolved, err := provider.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resolved, fixture.manifestBytes) {
		t.Fatal("resolved manifest differs from committed canonical bytes")
	}
}

func TestFilesystemCommitManifestRejectsCorruptReferencedContent(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		putFixtureBlob(t, provider, blob)
	}
	corrupt := fixture.blobs[0].Descriptor
	if err := os.WriteFile(provider.objectPath(corrupt.Digest), bytes.Repeat([]byte("x"), int(corrupt.Size)), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := provider.CommitManifest(context.Background(), fixture.manifestBytes); err == nil {
		t.Fatal("manifest referencing corrupt provider content committed")
	}
	if _, err := provider.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err == nil {
		t.Fatal("failed commit became resolvable")
	}
}

func TestFilesystemResolveManifestRejectsCorruptCommittedState(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		putFixtureBlob(t, provider, blob)
	}
	if err := provider.CommitManifest(context.Background(), fixture.manifestBytes); err != nil {
		t.Fatal(err)
	}
	path := provider.manifestPath(fixture.manifest.ArtifactDigest)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), len(fixture.manifestBytes)), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err == nil {
		t.Fatal("corrupt committed manifest resolved")
	}
}

func TestFilesystemGetObjectRejectsCorruptOrSymlinkedState(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("verified immutable bytes")
	blob := testBlob(content)
	putFixtureBlob(t, provider, blob)
	reader, err := provider.GetObject(context.Background(), blob.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("verified object read = %q, %v", got, err)
	}

	path := provider.objectPath(blob.Descriptor.Digest)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), len(content)), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetObject(context.Background(), blob.Descriptor); err == nil {
		t.Fatal("corrupt object resolved")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetObject(context.Background(), blob.Descriptor); err == nil {
		t.Fatal("symlinked object resolved")
	}
}

func TestFilesystemConcurrentIdenticalManifestCommitIsIdempotent(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		putFixtureBlob(t, provider, blob)
	}
	const workers = 12
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			errors <- provider.CommitManifest(context.Background(), fixture.manifestBytes)
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent commit: %v", err)
		}
	}
}

func TestFilesystemCommitManifestRejectsNonCanonicalOrWrongSelfIdentity(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		putFixtureBlob(t, provider, blob)
	}
	for name, content := range map[string][]byte{
		"leading whitespace":  append([]byte("\n"), fixture.manifestBytes...),
		"wrong self identity": bytes.Replace(fixture.manifestBytes, []byte(fixture.manifest.ArtifactDigest.String()), []byte("sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := provider.CommitManifest(context.Background(), content); err == nil {
				t.Fatal("invalid manifest committed")
			}
			if _, err := provider.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err == nil {
				t.Fatal("invalid commit made manifest visible")
			}
		})
	}
}

func TestFilesystemManifestPathsRejectSymlinkedParents(t *testing.T) {
	root := t.TempDir()
	provider, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		putFixtureBlob(t, provider, blob)
	}
	outside := t.TempDir()
	fanout := filepath.Join(root, "manifests", "sha256", fixture.manifest.ArtifactDigest.Hex()[:2])
	if err := os.Symlink(outside, fanout); err != nil {
		t.Fatal(err)
	}
	if err := provider.CommitManifest(context.Background(), fixture.manifestBytes); err == nil {
		t.Fatal("manifest commit traversed symlinked parent")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("manifest commit wrote outside provider root: %v", entries)
	}
}
