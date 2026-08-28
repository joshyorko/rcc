package environmentlifecycle

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
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

type localCacheENOSPCProvider struct {
	artifactprovider.Provider
	failPut bool
}

func (it *localCacheENOSPCProvider) PutObject(ctx context.Context, blob artifactprovider.Blob) error {
	if it.failPut {
		return syscall.ENOSPC
	}
	return it.Provider.PutObject(ctx, blob)
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

func TestAcquireLocalCASENOSPCFailsClosedAndRetries(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false

	remoteManifest, err := remote.ResolveManifest(context.Background(), artifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	local, err := artifactprovider.NewFilesystem(localContentRoot())
	if err != nil {
		t.Fatal(err)
	}
	localProvider := &localCacheENOSPCProvider{Provider: local, failPut: true}
	acquirer := NewAcquirer()
	acquirer.localProviderFactory = func() (artifactprovider.Provider, error) {
		return localProvider, nil
	}

	_, err = acquirer.Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err == nil || !errors.Is(err, syscall.ENOSPC) || !strings.Contains(err.Error(), "local cache write") {
		t.Fatalf("local CAS ENOSPC error = %v, want local cache write wrapping ENOSPC", err)
	}
	if _, err := local.ResolveManifest(context.Background(), artifactDigest); !os.IsNotExist(err) {
		t.Fatalf("local manifest after failed import = %v, want not exist", err)
	}
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing, stateReady} {
		path := filepath.Join(recordRoot(), artifactDigest.Hex(), string(state)+".json")
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s record after failed import = %v, want not exist", state, err)
		}
	}
	resolvedRemote, err := remote.ResolveManifest(context.Background(), artifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resolvedRemote, remoteManifest) {
		t.Fatal("remote manifest changed during failed local import")
	}
	manifest, err := environmentartifact.DecodeManifest(resolvedRemote)
	if err != nil || manifest.ArtifactDigest != artifactDigest {
		t.Fatalf("remote artifact identity after failed import = %s, %v", manifest.ArtifactDigest, err)
	}

	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactDigest != artifactDigest || result.Path == "" {
		t.Fatalf("repaired acquire result = %+v", result)
	}
	if _, err := local.ResolveManifest(context.Background(), artifactDigest); err != nil {
		t.Fatalf("local manifest after repair = %v", err)
	}
	if _, err := readReadyRecord(artifactDigest); err != nil {
		t.Fatalf("ready materialization after repair = %v", err)
	}
}

func TestAcquireVerifiedContentRegistersConsumerLocalLegacyCatalogAndPreservesCanonicalBytes(t *testing.T) {
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
	legacyCatalog, err := htfs.LoadPortableCatalog(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	wantBase := filepath.Join(consumerHome, "holotree")
	if legacyCatalog.Root().HolotreeBase() != wantBase {
		t.Fatalf("legacy catalog base = %q, want consumer base %q", legacyCatalog.Root().HolotreeBase(), wantBase)
	}
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
	catalogReader, err := local.GetObject(context.Background(), content.manifest.Catalogs[0].Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	canonicalCatalog, readErr := io.ReadAll(catalogReader)
	closeErr := catalogReader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(canonicalCatalog, fixture.catalogBytes) {
		t.Fatalf("canonical catalog changed during consumer registration: read=%v close=%v", readErr, closeErr)
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

func TestAcquireRejectsCompatibilityMismatchBeforeObjectFetch(t *testing.T) {
	fixture, remote, artifactDigest := publishedFixture(t)
	recorder := &recordingProvider{delegate: remote}
	previous := collectWorkerCapabilities
	collectWorkerCapabilities = func(context.Context, environmentartifact.CompatibilityRequirements) (environmentartifact.WorkerCapabilities, error) {
		required := fixture.build.Compatibility
		return environmentartifact.WorkerCapabilities{
			SchemaVersion:      environmentartifact.CompatibilitySchemaV1,
			RelocationVersions: []string{required.RelocationVersion},
			Python: environmentartifact.PythonCapabilities{
				Implementations: []string{required.Python.Implementation}, Versions: []string{required.Python.Version}, ABIs: []string{required.Python.ABI},
			},
			OS: environmentartifact.OSCapabilities{
				Family: required.OS.Family, Version: required.OS.MinimumVersion, KernelVersion: required.OS.KernelMinimum,
				LibC: required.OS.LibC, LibCVersion: "1.0", NativeArchitecture: required.OS.NativeArchitecture,
				Translation: "native", Runtime: required.OS.Runtime, Libraries: append([]string{}, required.OS.RequiredLibraries...),
			},
			CPU: environmentartifact.CPUCapabilities{Architecture: required.CPU.Architecture, Features: append([]string{}, required.CPU.RequiredFeatures...)},
			Filesystem: environmentartifact.FilesystemCapabilities{
				CaseSensitive: true, Symlinks: true, Junctions: true, LongPaths: true, MaxPath: 4096,
			},
		}, nil
	}
	t.Cleanup(func() { collectWorkerCapabilities = previous })
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false

	if _, err := acquireVerifiedContent(context.Background(), artifactDigest, recorder); err == nil || !strings.Contains(err.Error(), "libc-version") {
		t.Fatalf("compatibility mismatch = %v", err)
	}
	if !reflect.DeepEqual(recorder.events, []string{"resolve"}) {
		t.Fatalf("provider object fetch occurred before compatibility rejection: %v", recorder.events)
	}
}

func TestAcquireAcceptsArtifactBuiltOnNewerKernelOnSupportedOlderWorker(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	manifest, manifestBytes := mutateManifest(t, remote, artifactDigest, func(compatibility *environmentartifact.CompatibilityRequirements) {
		compatibility.OS.MinimumVersion = "1"
		compatibility.OS.KernelMinimum = "3.15"
	})
	if err := remote.CommitManifest(context.Background(), manifestBytes); err != nil {
		t.Fatal(err)
	}
	previous := collectWorkerCapabilities
	collectWorkerCapabilities = func(_ context.Context, required environmentartifact.CompatibilityRequirements) (environmentartifact.WorkerCapabilities, error) {
		return environmentartifact.WorkerCapabilities{
			SchemaVersion:      environmentartifact.CompatibilitySchemaV1,
			RelocationVersions: []string{required.RelocationVersion},
			Python: environmentartifact.PythonCapabilities{
				Implementations: []string{required.Python.Implementation}, Versions: []string{required.Python.Version}, ABIs: []string{required.Python.ABI},
			},
			OS: environmentartifact.OSCapabilities{
				Family: required.OS.Family, Version: required.OS.MinimumVersion,
				KernelVersion: "5.14.0-687.10.1.el9_8.0.1.x86_64", LibC: required.OS.LibC, LibCVersion: required.OS.LibCMinimum,
				NativeArchitecture: required.OS.NativeArchitecture, Translation: "native", Runtime: required.OS.Runtime,
				Libraries: append([]string{}, required.OS.RequiredLibraries...),
			},
			CPU: environmentartifact.CPUCapabilities{
				Architecture: required.CPU.Architecture, Features: append([]string{}, required.CPU.RequiredFeatures...),
			},
			Filesystem: environmentartifact.FilesystemCapabilities{
				CaseSensitive: true, Symlinks: true, Junctions: true, LongPaths: true, MaxPath: 4096,
			},
		}, nil
	}
	t.Cleanup(func() { collectWorkerCapabilities = previous })
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false

	content, err := acquireVerifiedContent(context.Background(), manifest.ArtifactDigest, remote)
	if err != nil {
		t.Fatalf("artifact built on Linux 7.1.8 was rejected on supported Linux 5.14 worker: %v", err)
	}
	if content.manifest.ArtifactDigest != manifest.ArtifactDigest {
		t.Fatalf("unexpected acquired content: %+v", content.manifest)
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
			Compatibility: fixture.build.Compatibility,
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

func TestHTTPAndOfflineArchiveConvergeOnExactArtifactIdentity(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	handler := artifactprovider.NewHandler(remote)
	server := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	defer server.Close()
	httpProvider, err := artifactprovider.NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "environment.rcca")
	manifest, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: artifactDigest, Provider: httpProvider, OutputPath: archivePath})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ArtifactDigest != artifactDigest {
		t.Fatalf("HTTP export digest = %s, want %s", manifest.ArtifactDigest, artifactDigest)
	}
	previousHome := common.Product.Home()
	consumerHome := t.TempDir()
	common.Product.ForceHome(consumerHome)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	imported, err := ImportArchive(context.Background(), ImportArchiveRequest{Path: archivePath})
	if err != nil {
		t.Fatal(err)
	}
	if imported.ArtifactDigest != artifactDigest {
		t.Fatalf("offline import digest = %s, want %s", imported.ArtifactDigest, artifactDigest)
	}
	local, err := artifactprovider.NewFilesystem(filepath.Join(consumerHome, "artifacts", "v1", "content"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := local.ResolveManifest(context.Background(), artifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := environmentartifact.DecodeManifest(resolved)
	if err != nil || decoded.ArtifactDigest != artifactDigest {
		t.Fatalf("offline local record identity = %s, %v", decoded.ArtifactDigest, err)
	}
}

func TestImportArchiveRollsBackStagedObjectsAfterInjectedFailure(t *testing.T) {
	fixture, remote, artifactDigest := publishedFixture(t)
	archivePath := filepath.Join(t.TempDir(), "environment.rcca")
	if _, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: artifactDigest, Provider: remote, OutputPath: archivePath}); err != nil {
		t.Fatal(err)
	}
	previousHome := common.Product.Home()
	home := t.TempDir()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	local, err := artifactprovider.NewFilesystem(filepath.Join(home, "artifacts", "v1", "content"))
	if err != nil {
		t.Fatal(err)
	}
	var puts int
	errInjected := errors.New("injected archive import failure")
	_, err = ImportArchive(context.Background(), ImportArchiveRequest{Path: archivePath, PutObject: func(ctx context.Context, blob artifactprovider.Blob) error {
		puts++
		if puts == 2 {
			return errInjected
		}
		return local.PutObject(ctx, blob)
	}})
	if !errors.Is(err, errInjected) {
		t.Fatalf("import error = %v, want injected failure", err)
	}
	if puts != 2 {
		t.Fatalf("PutObject calls = %d, want 2", puts)
	}
	if _, err := local.ResolveManifest(context.Background(), artifactDigest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest after rollback = %v", err)
	}
	objectsRoot := filepath.Join(home, "artifacts", "v1", "content", "objects")
	var residual int
	_ = filepath.Walk(objectsRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info.Mode().IsRegular() {
			residual++
		}
		return nil
	})
	if residual != 0 {
		t.Fatalf("rollback left %d staged object files", residual)
	}
	_ = fixture
}

func TestImportArchiveRejectsIncompatibleRequirementsBeforeBulkObjectImport(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	archivePath := filepath.Join(t.TempDir(), "artifact.rcca")
	if _, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: artifactDigest, Provider: remote, OutputPath: archivePath}); err != nil {
		t.Fatal(err)
	}
	previousHome := common.Product.Home()
	home := t.TempDir()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	previousCapabilities := collectWorkerCapabilities
	collectWorkerCapabilities = func(_ context.Context, required environmentartifact.CompatibilityRequirements) (environmentartifact.WorkerCapabilities, error) {
		return environmentartifact.WorkerCapabilities{
			SchemaVersion:      environmentartifact.CompatibilitySchemaV1,
			RelocationVersions: []string{required.RelocationVersion},
			Python: environmentartifact.PythonCapabilities{
				Implementations: []string{required.Python.Implementation}, Versions: []string{required.Python.Version}, ABIs: []string{required.Python.ABI},
			},
			OS: environmentartifact.OSCapabilities{
				Family: required.OS.Family, Version: required.OS.MinimumVersion, KernelVersion: "0",
				LibC: required.OS.LibC, LibCVersion: required.OS.LibCMinimum, NativeArchitecture: required.OS.NativeArchitecture,
				Translation: "native", Runtime: required.OS.Runtime, Libraries: append([]string{}, required.OS.RequiredLibraries...),
			},
			CPU:        environmentartifact.CPUCapabilities{Architecture: required.CPU.Architecture, Features: append([]string{}, required.CPU.RequiredFeatures...)},
			Filesystem: environmentartifact.FilesystemCapabilities{CaseSensitive: true, Symlinks: true, Junctions: true, LongPaths: true, MaxPath: 4096},
		}, nil
	}
	t.Cleanup(func() { collectWorkerCapabilities = previousCapabilities })
	puts := 0
	var local *artifactprovider.Filesystem
	var err error
	_, err = ImportArchive(context.Background(), ImportArchiveRequest{Path: archivePath, PutObject: func(ctx context.Context, blob artifactprovider.Blob) error {
		puts++
		if local == nil {
			var localErr error
			local, localErr = artifactprovider.NewFilesystem(filepath.Join(home, "artifacts", "v1", "content"))
			if localErr != nil {
				return localErr
			}
		}
		return local.PutObject(ctx, blob)
	}})
	if err == nil || !strings.Contains(err.Error(), "kernel-version") {
		t.Fatalf("incompatible archive error = %v, want kernel-version before import", err)
	}
	if puts != 0 {
		t.Fatalf("archive PutObject calls = %d, want zero", puts)
	}
	for _, path := range []string{
		filepath.Join(home, "artifacts", "v1", "content"),
		common.HololibCatalogLocation(), common.HolotreeLocation(),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("incompatible archive created %s: %v", path, statErr)
		}
	}
}

func TestImportArchiveRejectsCorruptObjectAfterCompatiblePreflight(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	archivePath := filepath.Join(t.TempDir(), "artifact.rcca")
	if _, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: artifactDigest, Provider: remote, OutputPath: archivePath}); err != nil {
		t.Fatal(err)
	}
	entries, err := environmentartifact.ReadArchiveFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	index, err := environmentartifact.DecodeObjectIndex(entries[environmentartifact.ArchiveObjectIndex])
	if err != nil || len(index.Entries) == 0 {
		t.Fatalf("object index = %+v, %v", index, err)
	}
	objectName := environmentartifact.ArchiveObjectDirectory + index.Entries[0].StoredDigest.Hex()
	entries[objectName] = bytes.Repeat([]byte("x"), len(entries[objectName]))
	corruptPath := filepath.Join(t.TempDir(), "corrupt.rcca")
	archiveFile, err := os.Create(corruptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := environmentartifact.WriteArchive(archiveFile, entries); err != nil {
		_ = archiveFile.Close()
		t.Fatal(err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatal(err)
	}
	previousHome := common.Product.Home()
	home := t.TempDir()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	if _, err := ImportArchive(context.Background(), ImportArchiveRequest{Path: corruptPath}); err == nil || !strings.Contains(err.Error(), "size or digest mismatch") {
		t.Fatalf("corrupt archive error = %v, want object digest mismatch", err)
	}
	manifestPath := filepath.Join(home, "artifacts", "v1", "content", "manifests", "sha256", artifactDigest.Hex()[:2], artifactDigest.Hex()[2:4], artifactDigest.Hex())
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("corrupt archive left manifest cache: %v", err)
	}
}

func TestLegacyV12WrapImportAndExecutePreservesClosure(t *testing.T) {
	fixture := newPublishFixture(t)
	inventory, err := environmentartifact.InventoryV12(environmentartifact.InventoryInput{CatalogPath: fixture.build.CatalogPath, LegacyBlueprint: fixture.build.LegacyBlueprint, ExpectedPlatform: fixture.build.Platform.RCCPlatform})
	if err != nil {
		t.Fatal(err)
	}
	specificationBytes, err := semanticSpecificationBytes(fixture.build.SourceKind, fixture.build.LegacyBlueprint, fixture.build.Platform, fixture.build.Builder, fixture.build.Compatibility)
	if err != nil {
		t.Fatal(err)
	}
	specification := environmentartifact.Specification{Descriptor: descriptor(environmentartifact.SpecificationMediaType, specificationBytes), SourceKind: fixture.build.SourceKind, Platform: fixture.build.Platform, Builder: fixture.build.Builder}
	legacyBlueprint := environmentartifact.LegacyBlueprint{Descriptor: inventory.LegacyBlueprint, LegacyBlueprintKey: inventory.LegacyBlueprintKey}
	input := environmentartifact.LegacyV12Input{Specification: specification, SpecificationBytes: specificationBytes, LegacyBlueprint: legacyBlueprint, LegacyBlueprintBytes: fixture.build.LegacyBlueprint, Catalog: inventory.Catalog, CatalogBytes: fixture.catalogBytes, Index: inventory.Index, IndexBytes: inventory.IndexBytes, Platform: fixture.build.Platform, Builder: fixture.build.Builder, Compatibility: fixture.build.Compatibility}
	input.Objects = make(map[environmentartifact.Digest][]byte, len(inventory.Objects))
	for digest, path := range inventory.Objects {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		input.Objects[digest] = content
	}
	manifest, _, entries, err := environmentartifact.WrapLegacyV12(input)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := environmentartifact.WriteArchive(&archive, entries); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "legacy.rcca")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	previousHome := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	local, err := artifactprovider.NewFilesystem(filepath.Join(common.Product.Home(), "artifacts", "v1", "content"))
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportArchive(context.Background(), ImportArchiveRequest{Path: archivePath})
	if err != nil || imported.ArtifactDigest != manifest.ArtifactDigest {
		t.Fatalf("legacy import = %s, %v", imported.ArtifactDigest, err)
	}
	for digest, want := range input.Objects {
		reader, getErr := local.GetObject(context.Background(), environmentartifact.Descriptor{Digest: digest, Size: int64(len(want))})
		if getErr != nil {
			t.Fatal(getErr)
		}
		got, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("legacy object %s read: %v, close: %v", digest, readErr, closeErr)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("legacy object %s changed during wrap/import", digest)
		}
	}
	catalogReader, err := local.GetObject(context.Background(), input.Catalog.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	gotCatalog, readErr := io.ReadAll(catalogReader)
	closeErr := catalogReader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(gotCatalog, input.CatalogBytes) {
		t.Fatalf("legacy catalog changed during wrap/import: read=%v close=%v", readErr, closeErr)
	}
	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: imported.ArtifactDigest})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactDigest != manifest.ArtifactDigest || result.Path == "" {
		t.Fatalf("legacy execution materialization = %+v", result)
	}
	_, child, err := Execute(context.Background(), NewLocalMaterializer(), Materialization{ArtifactDigest: result.ArtifactDigest, ID: result.MaterializationID, Path: result.Path, CacheHit: result.CacheHit}, []string{"sh", "-c", "test -f python"})
	if err != nil || child.ExitCode != 0 {
		t.Fatalf("legacy materialization task execution = %+v, %v", child, err)
	}
}

func TestWriteEnvironmentArtifactN1Fixture(t *testing.T) {
	if os.Getenv("RCC_WRITE_N1_ARCHIVE") != "1" {
		t.Skip("fixture writer is opt-in")
	}
	output := os.Getenv("RCC_N1_ARCHIVE")
	if output == "" {
		t.Fatal("RCC_N1_ARCHIVE is required")
	}
	_, remote, artifactDigest := publishedFixture(t)
	if _, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: artifactDigest, Provider: remote, OutputPath: output}); err != nil {
		t.Fatal(err)
	}
}
