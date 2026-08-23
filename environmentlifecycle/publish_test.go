package environmentlifecycle

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
)

type recordingBuilder struct {
	result BuildResult
	calls  []string
}

func (it *recordingBuilder) Build(_ context.Context, robotFile string) (BuildResult, error) {
	it.calls = append(it.calls, robotFile)
	return it.result, nil
}

type recordingProvider struct {
	delegate  artifactprovider.Provider
	events    []string
	put       []environmentartifact.Digest
	commitErr error
}

func (it *recordingProvider) Capabilities(ctx context.Context) (artifactprovider.Capabilities, error) {
	it.events = append(it.events, "capabilities")
	return it.delegate.Capabilities(ctx)
}

func (it *recordingProvider) ResolveManifest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	it.events = append(it.events, "resolve")
	return it.delegate.ResolveManifest(ctx, digest)
}

func (it *recordingProvider) MissingObjects(ctx context.Context, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	it.events = append(it.events, "missing")
	return it.delegate.MissingObjects(ctx, descriptors)
}

func (it *recordingProvider) PutObject(ctx context.Context, blob artifactprovider.Blob) error {
	it.events = append(it.events, "put")
	it.put = append(it.put, blob.Descriptor.Digest)
	return it.delegate.PutObject(ctx, blob)
}

func (it *recordingProvider) GetObject(ctx context.Context, descriptor environmentartifact.Descriptor) (io.ReadCloser, error) {
	it.events = append(it.events, "get")
	return it.delegate.GetObject(ctx, descriptor)
}

func (it *recordingProvider) CommitManifest(ctx context.Context, content []byte) error {
	it.events = append(it.events, "commit")
	if it.commitErr != nil {
		return it.commitErr
	}
	return it.delegate.CommitManifest(ctx, content)
}

type publishFixture struct {
	build        BuildResult
	catalogBytes []byte
	objectBytes  []byte
	objectPath   string
}

func testBuildCompatibility(platform environmentartifact.Platform) environmentartifact.CompatibilityRequirements {
	requirements := environmentartifact.CompatibilityRequirements{
		SchemaVersion: 1, RelocationVersion: "holotree-v12-path-rewrite-v1",
		Python: environmentartifact.PythonRequirements{Implementation: "cpython", Version: "3.11.9", ABI: "cp311"},
		OS: environmentartifact.OSRequirements{
			Family: platform.OS, MinimumVersion: "1", KernelMinimum: "1", NativeArchitecture: platform.Arch,
			TranslationPolicy: "native-only", RequiredLibraries: []string{},
		},
		CPU:        environmentartifact.CPURequirements{Architecture: platform.Arch, RequiredFeatures: []string{}},
		Filesystem: environmentartifact.FilesystemRequirements{LongPaths: true, MinimumMaxPath: 260},
	}
	if platform.OS == "linux" {
		requirements.OS.LibC = "glibc"
		requirements.OS.LibCMinimum = "2.17"
	}
	return requirements
}

func TestPublishUploadsOnlyMissingContentThenCommitsManifest(t *testing.T) {
	fixture := newPublishFixture(t)
	filesystem, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	specificationDescriptor := descriptor(environmentartifact.SpecificationMediaType, fixture.build.SpecificationBytes)
	if err := filesystem.PutObject(context.Background(), artifactprovider.Blob{
		Descriptor: specificationDescriptor,
		Reader:     bytes.NewReader(fixture.build.SpecificationBytes),
	}); err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{delegate: filesystem}
	builder := &recordingBuilder{result: fixture.build}

	result, err := Publish(context.Background(), PublishRequest{
		RobotFile: "fixtures/robot.yaml", Provider: provider, Builder: builder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(builder.calls, []string{"fixtures/robot.yaml"}) {
		t.Fatalf("builder calls = %#v", builder.calls)
	}
	if result.SpecificationDigest != specificationDescriptor.Digest {
		t.Fatalf("specification digest = %s", result.SpecificationDigest)
	}
	if result.LegacyBlueprintKey != common.BlueprintHash(fixture.build.LegacyBlueprint) {
		t.Fatalf("legacy blueprint key = %q", result.LegacyBlueprintKey)
	}
	if result.ObjectCount != 1 {
		t.Fatalf("object count = %d", result.ObjectCount)
	}
	if containsDigest(provider.put, specificationDescriptor.Digest) {
		t.Fatal("already-present semantic specification was uploaded")
	}
	if len(provider.events) == 0 || provider.events[len(provider.events)-1] != "commit" {
		t.Fatalf("provider event order = %#v", provider.events)
	}

	manifestBytes, err := filesystem.ResolveManifest(context.Background(), result.ArtifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	assertProviderObject(t, filesystem, manifest.Specification.Descriptor, fixture.build.SpecificationBytes)
	assertProviderObject(t, filesystem, manifest.LegacyBlueprint.Descriptor, fixture.build.LegacyBlueprint)
	assertProviderObject(t, filesystem, manifest.Catalogs[0].Descriptor, fixture.catalogBytes)
	if manifest.Specification.Digest == manifest.LegacyBlueprint.Digest {
		t.Fatal("semantic specification and legacy blueprint share an identity")
	}
	if manifest.LegacyBlueprint.LegacyBlueprintKey != common.BlueprintHash(fixture.build.LegacyBlueprint) {
		t.Fatal("manifest did not retain the exact legacy blueprint compatibility key")
	}
}

func TestPublishWritesGeneratedProvenanceToCarrierAfterManifestCommit(t *testing.T) {
	t.Setenv("RCC_PUBLISH_AUTH_SECRET", "publish-provider-secret-sentinel")
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	result, err := Publish(context.Background(), PublishRequest{
		RobotFile: "fixtures/robot.yaml", Provider: provider, Builder: &recordingBuilder{result: fixture.build}, TrustCarrier: carrier,
	})
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := artifacttrust.LoadAttachments(carrier, result.ArtifactDigest.String())
	if err != nil || attachments.Provenance == nil || attachments.SBOM == nil {
		t.Fatalf("attachments=%+v err=%v", attachments, err)
	}
	if attachments.Provenance.ArtifactDigest != result.ArtifactDigest.String() || attachments.Provenance.Platform != fixture.build.Platform.RCCPlatform || attachments.Provenance.Builder != fixture.build.Builder.Kind {
		t.Fatalf("provenance=%+v", attachments.Provenance)
	}
	if len(attachments.SBOM.Components) == 0 || attachments.SBOM.ArtifactDigest != result.ArtifactDigest.String() {
		t.Fatalf("SBOM=%+v", attachments.SBOM)
	}
	for _, kind := range []string{"provenance", "sbom", "revocations"} {
		data, err := carrier.Read(artifacttrust.AttachmentName(result.ArtifactDigest.String(), kind))
		if err != nil || bytes.Contains(data, []byte("publish-provider-secret-sentinel")) {
			t.Fatalf("%s attachment leaked provider secret: %q err=%v", kind, data, err)
		}
	}
}

func TestPublishStoresGeneratedSignatureAndRevocationAttachments(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Publish(context.Background(), PublishRequest{
		RobotFile: "fixtures/robot.yaml", Provider: provider, Builder: &recordingBuilder{result: fixture.build},
		TrustCarrier: carrier, TrustSigningKey: private, TrustSigningKeyID: "publish-key",
		TrustRevocations: []artifacttrust.Revocation{{KeyIDs: []string{"incident-key"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := artifacttrust.LoadAttachments(carrier, result.ArtifactDigest.String())
	if err != nil || !attachments.SignaturesPresent || !attachments.RevocationsPresent || len(attachments.Signatures) != 1 || len(attachments.Revocations) != 1 {
		t.Fatalf("attachments=%+v err=%v", attachments, err)
	}
	if err := artifacttrust.VerifySignature(attachments.Signatures[0], public, result.ArtifactDigest.String()); err != nil {
		t.Fatal(err)
	}
}

type failingTrustCarrier struct {
	delegate artifacttrust.Carrier
	failOn   string
}

func (c failingTrustCarrier) Read(name string) ([]byte, error) { return c.delegate.Read(name) }

func (c failingTrustCarrier) Write(name string, data []byte) error {
	if strings.Contains(name, c.failOn) {
		return errors.New("injected trust carrier write failure")
	}
	return c.delegate.Write(name, data)
}

func TestPublishStagesTrustBeforeManifestCommitAndRetryRecovers(t *testing.T) {
	fixture := newPublishFixture(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	providerEvents := &recordingProvider{delegate: provider}
	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	failing := failingTrustCarrier{delegate: carrier, failOn: "sbom"}
	if _, err := Publish(context.Background(), PublishRequest{
		RobotFile: "fixtures/robot.yaml", Provider: providerEvents, Builder: &recordingBuilder{result: fixture.build}, TrustCarrier: failing,
	}); err == nil {
		t.Fatal("trust staging failure unexpectedly published")
	}
	for _, event := range providerEvents.events {
		if event == "commit" {
			t.Fatal("manifest committed before trust staging completed")
		}
	}
	if _, err := Publish(context.Background(), PublishRequest{
		RobotFile: "fixtures/robot.yaml", Provider: providerEvents, Builder: &recordingBuilder{result: fixture.build}, TrustCarrier: carrier,
	}); err != nil {
		t.Fatal(err)
	}
	if len(providerEvents.events) == 0 || providerEvents.events[len(providerEvents.events)-1] != "commit" {
		t.Fatalf("retry provider events=%v", providerEvents.events)
	}
}

func TestPublishCommitFailureDoesNotMutateBuiltEnvironment(t *testing.T) {
	fixture := newPublishFixture(t)
	filesystem, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{delegate: filesystem, commitErr: errors.New("commit unavailable")}
	builder := &recordingBuilder{result: fixture.build}

	_, err = Publish(context.Background(), PublishRequest{RobotFile: "robot.yaml", Provider: provider, Builder: builder})
	if err == nil {
		t.Fatal("publish succeeded despite manifest commit failure")
	}
	catalogAfter, readErr := os.ReadFile(fixture.build.CatalogPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	objectAfter, readErr := os.ReadFile(fixture.objectPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(catalogAfter, fixture.catalogBytes) || !bytes.Equal(objectAfter, fixture.objectBytes) {
		t.Fatal("provider failure mutated the locally built environment")
	}
	if provider.events[len(provider.events)-1] != "commit" {
		t.Fatalf("provider event order = %#v", provider.events)
	}
}

func TestPublishRejectsRawModeBeforeProviderCallsWithoutMutatingMarker(t *testing.T) {
	fixture := newPublishFixture(t)
	marker := common.HololibCompressMarker()
	wantMarker := []byte("operator-selected raw mode\n")
	if err := os.WriteFile(marker, wantMarker, 0o640); err != nil {
		t.Fatal(err)
	}
	filesystem, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{delegate: filesystem}
	builder := &recordingBuilder{result: fixture.build}

	if _, err := Publish(context.Background(), PublishRequest{RobotFile: "robot.yaml", Provider: provider, Builder: builder}); err == nil {
		t.Fatal("raw Hololib mode was published")
	}
	if len(provider.events) != 0 {
		t.Fatalf("provider touched before storage-mode validation: %#v", provider.events)
	}
	gotMarker, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotMarker, wantMarker) {
		t.Fatalf("compress.no mutated: %q", gotMarker)
	}
}

func TestPublishRejectsNonCanonicalOrConflatedSemanticSpecification(t *testing.T) {
	for name, specification := range map[string]func(publishFixture) []byte{
		"legacy blueprint bytes": func(fixture publishFixture) []byte { return fixture.build.LegacyBlueprint },
		"non-canonical JSON":     func(publishFixture) []byte { return []byte(" {\"dependencies\":[]}") },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newPublishFixture(t)
			fixture.build.SpecificationBytes = specification(fixture)
			filesystem, err := artifactprovider.NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			provider := &recordingProvider{delegate: filesystem}
			if _, err := Publish(context.Background(), PublishRequest{
				RobotFile: "robot.yaml", Provider: provider, Builder: &recordingBuilder{result: fixture.build},
			}); err == nil {
				t.Fatal("invalid semantic specification was published")
			}
			if len(provider.events) != 0 {
				t.Fatalf("provider touched before specification validation: %#v", provider.events)
			}
		})
	}
}

func newPublishFixture(t *testing.T) publishFixture {
	t.Helper()
	previousCompatibilityVerifier := verifyMaterializedCompatibility
	verifyMaterializedCompatibility = func(context.Context, string, environmentartifact.CompatibilityRequirements) error { return nil }
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
		verifyMaterializedCompatibility = previousCompatibilityVerifier
	})
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(common.HololibLibraryLocation(), 0o750); err != nil {
		t.Fatal(err)
	}

	legacyBlueprint := []byte("channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n")
	logical := []byte("immutable python bytes")
	logicalSum := sha256.Sum256(logical)
	legacyObjectID := fmt.Sprintf("%x", logicalSum)
	objectPath := htfs.ExactDefaultLocation(legacyObjectID)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o750); err != nil {
		t.Fatal(err)
	}
	var stored bytes.Buffer
	writer, err := gzip.NewWriterLevel(&stored, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(logical); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, stored.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}

	root, err := htfs.NewRoot(filepath.Join(t.TempDir(), "h123456_123456789abcdeft"))
	if err != nil {
		t.Fatal(err)
	}
	root.Blueprint = common.BlueprintHash(legacyBlueprint)
	root.Tree.Mode = 0o750 | os.ModeDir
	pythonName := "python"
	if runtime.GOOS == "windows" {
		pythonName = "python.exe"
	}
	root.Tree.Files[pythonName] = &htfs.File{Name: pythonName, Size: int64(len(logical)), Mode: 0o750, Digest: legacyObjectID, Rewrite: []int64{}}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(root.Blueprint))
	if err := root.SaveAs(catalogPath); err != nil {
		t.Fatal(err)
	}
	catalogBytes, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	platform := environmentartifact.CurrentPlatform()
	return publishFixture{
		build: BuildResult{
			LegacyBlueprint:    legacyBlueprint,
			CatalogPath:        catalogPath,
			SpecificationBytes: []byte(`{"dependencies":["python=3.11"],"source":"robot.yaml"}`),
			SourceKind:         "robot.yaml",
			Platform:           platform,
			Builder:            environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: "v0.test", CompatibilityKey: "v12-gzip-sha256"},
			Compatibility:      testBuildCompatibility(platform),
		},
		catalogBytes: catalogBytes,
		objectBytes:  append([]byte(nil), stored.Bytes()...),
		objectPath:   objectPath,
	}
}

func descriptor(mediaType string, content []byte) environmentartifact.Descriptor {
	return environmentartifact.Descriptor{MediaType: mediaType, Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
}

func containsDigest(haystack []environmentartifact.Digest, needle environmentartifact.Digest) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}

func assertProviderObject(t *testing.T, provider artifactprovider.Provider, descriptor environmentartifact.Descriptor, want []byte) {
	t.Helper()
	reader, err := provider.GetObject(context.Background(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read provider object: %v, close: %v", readErr, closeErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("provider object = %q, want %q", got, want)
	}
}
