package environmentlifecycle

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestGCReportUsesDistinctByteFields(t *testing.T) {
	b, err := json.Marshal(GCReport{ProtectedBytes: 1, ReclaimableBytes: 2, ReclaimedBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	var protected, reclaimable, reclaimed int64
	for key, target := range map[string]*int64{"protectedBytes": &protected, "reclaimableBytes": &reclaimable, "reclaimedBytes": &reclaimed} {
		if err := json.Unmarshal(got[key], target); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if protected != 1 || reclaimable != 2 || reclaimed != 3 {
		t.Fatalf("byte fields = %d,%d,%d", protected, reclaimable, reclaimed)
	}
}

func TestCrashMatrixExecutesEveryLifecyclePoint(t *testing.T) {
	defer SetCrashHook(nil)
	seen := map[CrashPoint]bool{}
	SetCrashHook(func(point CrashPoint) error { seen[point] = true; return errors.New("injected crash") })
	for _, point := range CrashPoints() {
		_ = crash(point)
	}
	for _, point := range CrashPoints() {
		if !seen[point] {
			t.Fatalf("crash point %q was not executed", point)
		}
	}
}

func TestReferenceGraphProtectsSharedClosure(t *testing.T) {
	d := func(ch byte) environmentartifact.Digest {
		v, _ := environmentartifact.ParseDigest("sha256:" + strings.Repeat(string(ch), 64))
		return v
	}
	manifest := environmentartifact.Manifest{ArtifactDigest: d('a'), Specification: environmentartifact.Specification{Descriptor: environmentartifact.Descriptor{Digest: d('b')}}, LegacyBlueprint: environmentartifact.LegacyBlueprint{Descriptor: environmentartifact.Descriptor{Digest: d('c')}}, Catalogs: []environmentartifact.CatalogDescriptor{{Descriptor: environmentartifact.Descriptor{Digest: d('d')}}}, ObjectIndex: environmentartifact.Descriptor{Digest: d('e')}}
	graph := BuildReferenceGraph(manifest, environmentartifact.ObjectIndex{Entries: []environmentartifact.ObjectEntry{{StoredDigest: d('f')}}})
	if len(graph.Protected) != 5 {
		t.Fatalf("protected=%d", len(graph.Protected))
	}
}

func TestManifestObjectIndexReferenceRootSurvivesLifecycleRestart(t *testing.T) {
	digest := environmentartifact.DigestBytes([]byte("durable-reference-root"))
	manifest := environmentartifact.Manifest{ArtifactDigest: digest}
	index := environmentartifact.ObjectIndex{}
	previous := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(previous) })
	if err := writeReferenceRoot(manifest, index); err != nil {
		t.Fatal(err)
	}
	got, err := readReferenceRoot(digest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest != digest || !referenceRootExists(digest) {
		t.Fatalf("reference root = %+v", got)
	}
}

func TestProtectionRecordsCoverRepresentativeLargeClosure(t *testing.T) {
	const objectCount = 5694
	previous := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(previous) })

	manifest := environmentartifact.DigestBytes([]byte("large-reference-root"))
	protected := make([]environmentartifact.Digest, objectCount+4)
	for index := range protected {
		protected[index] = environmentartifact.DigestBytes([]byte(fmt.Sprintf("protected-%d", index)))
	}
	root := durableReferenceRoot{Manifest: manifest, Protected: protected, State: "live"}
	content, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) <= maxMaterializationRecordBytes {
		t.Fatalf("large closure did not exceed the small record bound: %d", len(content))
	}
	if err := writeAtomicMutable(recordRoot(), []string{manifest.Hex(), "references.json"}, content); err != nil {
		t.Fatal(err)
	}
	if loaded, err := readReferenceRoot(manifest); err != nil || len(loaded.Protected) != len(protected) {
		t.Fatalf("large reference root = %d, %v", len(loaded.Protected), err)
	}

	lease := Lease{ID: "large-lease", ArtifactDigest: manifest, Protected: protected}
	leaseContent, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicMutable(recordRoot(), leaseComponents(manifest, lease.ID), leaseContent); err != nil {
		t.Fatal(err)
	}
	if loaded, err := readLease(manifest, lease.ID); err != nil || len(loaded.Protected) != len(protected) {
		t.Fatalf("large lease protection = %d, %v", len(loaded.Protected), err)
	}
}

func TestRetentionEligibilityUsesInjectedClock(t *testing.T) {
	now := time.Unix(100, 0)
	record := materializationRecord{VerifiedAt: now.Add(-10 * time.Second)}
	if RetentionEligible(record, now, 11*time.Second) {
		t.Fatal("record incorrectly eligible")
	}
	if !RetentionEligible(record, now, 10*time.Second) {
		t.Fatal("record at retention boundary not eligible")
	}
}

func TestInspectDoesNotDeadlockInsideArtifactTransaction(t *testing.T) {
	digest := environmentartifact.DigestBytes([]byte("inspect-transaction"))
	previous := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(previous) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Inspect(ctx, digest); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("inspect failed: %v", err)
	}
}

func TestVerifyRejectsMissingArtifactClosureAsLocalCorruption(t *testing.T) {
	_, remote, digest, manifest, _ := acquiredAdminFixture(t)
	indexPath := adminContentObjectPath(manifest.ObjectIndex.Digest)
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), digest)
	if !errors.Is(err, ErrMaterializationCorrupt) {
		t.Fatalf("Verify error = %v, want local corruption", err)
	}
	if errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("Verify classified local corruption as provider absence: %v", err)
	}
	if verification.Verified || verification.Digest != digest {
		t.Fatalf("verification = %+v", verification)
	}
	inspection, err := Inspect(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Corrupt || inspection.ProviderUnavailable || inspection.Digest != digest {
		t.Fatalf("inspection = %+v", inspection)
	}
	if _, err := remote.ResolveManifest(context.Background(), digest); err != nil {
		t.Fatalf("trusted provider lost durable artifact identity: %v", err)
	}
}

func TestVerifyRejectsMissingOrCorruptManifestAsLocalCorruption(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "missing", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "corrupt", mutate: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("corrupt manifest"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, digest, _, _ := acquiredAdminFixture(t)
			testCase.mutate(t, adminContentManifestPath(digest))

			verification, err := Verify(context.Background(), digest)
			if !errors.Is(err, ErrMaterializationCorrupt) {
				t.Fatalf("Verify error = %v, want local corruption", err)
			}
			if errors.Is(err, ErrProviderUnavailable) || verification.Verified {
				t.Fatalf("verification = %+v, error = %v", verification, err)
			}
		})
	}
}

func TestVerifyRejectsCorruptArtifactClosureAsLocalCorruption(t *testing.T) {
	_, _, digest, manifest, _ := acquiredAdminFixture(t)
	path := adminContentObjectPath(manifest.Catalogs[0].Digest)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(manifest.Catalogs[0].Size)), 0o600); err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), digest)
	if !errors.Is(err, ErrMaterializationCorrupt) {
		t.Fatalf("Verify error = %v, want local corruption", err)
	}
	if errors.Is(err, ErrProviderUnavailable) || verification.Verified {
		t.Fatalf("verification = %+v, error = %v", verification, err)
	}
}

func TestRepairFailsClosedOnCorruptClosureAndPreservesArtifactIdentity(t *testing.T) {
	_, _, digest, manifest, _ := acquiredAdminFixture(t)
	if err := os.Remove(adminContentObjectPath(manifest.Specification.Digest)); err != nil {
		t.Fatal(err)
	}

	report, err := Repair(context.Background(), digest)
	if !errors.Is(err, ErrMaterializationCorrupt) {
		t.Fatalf("Repair error = %v, want local corruption", err)
	}
	if errors.Is(err, ErrProviderUnavailable) || !report.Inspection.Corrupt || report.Verification.Verified {
		t.Fatalf("repair report = %+v, error = %v", report, err)
	}
	root, err := readReferenceRoot(digest)
	if err != nil {
		t.Fatal(err)
	}
	if root.Manifest != digest {
		t.Fatalf("reference root identity = %s, want %s", root.Manifest, digest)
	}
	if _, err := readReadyRecord(digest); !os.IsNotExist(err) {
		t.Fatalf("corrupt ready record was not invalidated: %v", err)
	}
}

func TestVerifyIgnoresUnreferencedContent(t *testing.T) {
	_, _, digest, manifest, index := acquiredAdminFixture(t)
	provider, err := artifactprovider.NewFilesystem(localContentRoot())
	if err != nil {
		t.Fatal(err)
	}
	dead := []byte("unreferenced local artifact content")
	deadDigest := environmentartifact.DigestBytes(dead)
	if err := provider.PutObject(context.Background(), artifactprovider.Blob{
		Descriptor: environmentartifact.Descriptor{Digest: deadDigest, Size: int64(len(dead))}, Reader: bytes.NewReader(dead),
	}); err != nil {
		t.Fatal(err)
	}

	verification, err := Verify(context.Background(), digest)
	if err != nil || !verification.Verified {
		t.Fatalf("Verify with unreferenced content = %+v, %v", verification, err)
	}
	graph := BuildReferenceGraph(manifest, index)
	for _, protected := range graph.Protected {
		if protected == deadDigest {
			t.Fatalf("unreferenced digest was protected: %s", deadDigest)
		}
	}
}

func TestRepairFromProviderReportsProviderUnavailableSeparately(t *testing.T) {
	digest := environmentartifact.DigestBytes([]byte("provider-unavailable"))
	previous := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previous)
		common.SharedHolotree = previousShared
	})
	provider := artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
		return nil, errors.New("provider is offline")
	})

	report, err := RepairFromProvider(context.Background(), digest, provider)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("RepairFromProvider error = %v, want provider unavailable", err)
	}
	if errors.Is(err, ErrMaterializationCorrupt) || report.Inspection.Corrupt {
		t.Fatalf("provider absence was classified as local corruption: %+v, %v", report, err)
	}
}

func TestRepairFromProviderRestoresCorruptClosureWithSameArtifactIdentity(t *testing.T) {
	_, remote, digest, manifest, _ := acquiredAdminFixture(t)
	if err := os.WriteFile(adminContentObjectPath(manifest.LegacyBlueprint.Digest), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := RepairFromProvider(context.Background(), digest, remote)
	if err != nil {
		t.Fatalf("RepairFromProvider error = %v", err)
	}
	if !report.Repaired || !report.Verification.Verified || report.Verification.Digest != digest {
		t.Fatalf("repair report = %+v", report)
	}
	verification, err := Verify(context.Background(), digest)
	if err != nil || !verification.Verified || verification.Digest != digest {
		t.Fatalf("post-repair verification = %+v, %v", verification, err)
	}
}

func TestRepairFromProviderRestoresNonRegularCorruptClosureWithSameArtifactIdentity(t *testing.T) {
	_, remote, digest, manifest, _ := acquiredAdminFixture(t)
	path := adminContentObjectPath(manifest.LegacyBlueprint.Digest)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	report, err := RepairFromProvider(context.Background(), digest, remote)
	if err != nil {
		t.Fatalf("RepairFromProvider error = %v", err)
	}
	if !report.Repaired || !report.Verification.Verified || report.Verification.Digest != digest {
		t.Fatalf("repair report = %+v", report)
	}
	verification, err := Verify(context.Background(), digest)
	if err != nil || !verification.Verified || verification.Digest != digest {
		t.Fatalf("post-repair verification = %+v, %v", verification, err)
	}
	root, err := readReferenceRoot(digest)
	if err != nil || root.Manifest != digest {
		t.Fatalf("reference root = %+v, %v", root, err)
	}
}

func TestRepairFromProviderDoesNotFollowCorruptClosureSymlink(t *testing.T) {
	t.Run("leaf", func(t *testing.T) {
		_, remote, digest, manifest, _ := acquiredAdminFixture(t)
		outside := t.TempDir()
		sentinelPath := filepath.Join(outside, "sentinel")
		sentinel := []byte("must remain outside the CAS")
		if err := os.WriteFile(sentinelPath, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		path := adminContentObjectPath(manifest.LegacyBlueprint.Digest)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := createAdminDirectoryLink(outside, path); err != nil {
			t.Fatal(err)
		}

		if _, err := RepairFromProvider(context.Background(), digest, remote); err != nil {
			t.Fatalf("RepairFromProvider error = %v", err)
		}
		if got, err := os.ReadFile(sentinelPath); err != nil || !bytes.Equal(got, sentinel) {
			t.Fatalf("outside sentinel = %q, %v", got, err)
		}
	})

	t.Run("swapped-parent", func(t *testing.T) {
		_, remote, digest, _, _ := acquiredAdminFixture(t)
		manifestPath := adminContentManifestPath(digest)
		parent := filepath.Dir(manifestPath)
		held := parent + ".held"
		if err := os.Rename(parent, held); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		sentinelPath := filepath.Join(outside, filepath.Base(manifestPath))
		sentinel := []byte("must remain outside a swapped CAS parent")
		if err := os.WriteFile(sentinelPath, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := createAdminDirectoryLink(outside, parent); err != nil {
			t.Fatal(err)
		}

		if _, err := RepairFromProvider(context.Background(), digest, remote); !errors.Is(err, ErrMaterializationCorrupt) {
			t.Fatalf("RepairFromProvider error = %v, want fail-closed local corruption", err)
		}
		if got, err := os.ReadFile(sentinelPath); err != nil || !bytes.Equal(got, sentinel) {
			t.Fatalf("outside swapped-parent sentinel = %q, %v", got, err)
		}
	})
}

func createAdminDirectoryLink(target, link string) error {
	if runtime.GOOS != "windows" {
		return os.Symlink(target, link)
	}
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
}

func TestRepairFromProviderPreservesValidSharedCASObjectWhenProviderRepairFails(t *testing.T) {
	fixture, remote, digest, manifest, index := acquiredAdminFixture(t)
	sharedDigest := index.Entries[0].StoredDigest
	sharedPath := adminContentObjectPath(sharedDigest)
	sharedBefore, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatal(err)
	}

	catalogReader, err := gzip.NewReader(bytes.NewReader(fixture.catalogBytes))
	if err != nil {
		t.Fatal(err)
	}
	catalogContent, err := io.ReadAll(catalogReader)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalogReader.Close(); err != nil {
		t.Fatal(err)
	}
	var sharedCatalog bytes.Buffer
	catalogWriter, err := gzip.NewWriterLevel(&sharedCatalog, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	catalogWriter.ModTime = time.Unix(1, 0).UTC()
	if _, err := catalogWriter.Write(catalogContent); err != nil {
		t.Fatal(err)
	}
	if err := catalogWriter.Close(); err != nil {
		t.Fatal(err)
	}
	sharedCatalogDescriptor := manifest.Catalogs[0].Descriptor
	sharedCatalogDescriptor.Digest = environmentartifact.DigestBytes(sharedCatalog.Bytes())
	sharedCatalogDescriptor.Size = int64(sharedCatalog.Len())
	if err := remote.PutObject(context.Background(), artifactprovider.Blob{Descriptor: sharedCatalogDescriptor, Reader: bytes.NewReader(sharedCatalog.Bytes())}); err != nil {
		t.Fatal(err)
	}

	sharedManifest := manifest
	sharedManifest.Catalogs = append([]environmentartifact.CatalogDescriptor(nil), manifest.Catalogs...)
	sharedManifest.Catalogs[0].Descriptor = sharedCatalogDescriptor
	identity, err := sharedManifest.IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	sharedManifest.ArtifactDigest = environmentartifact.DigestBytes(identity)
	sharedManifestBytes, err := sharedManifest.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.CommitManifest(context.Background(), sharedManifestBytes); err != nil {
		t.Fatal(err)
	}
	local, err := artifactprovider.NewFilesystem(localContentRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := local.PutObject(context.Background(), artifactprovider.Blob{Descriptor: sharedCatalogDescriptor, Reader: bytes.NewReader(sharedCatalog.Bytes())}); err != nil {
		t.Fatal(err)
	}
	if err := local.CommitManifest(context.Background(), sharedManifestBytes); err != nil {
		t.Fatal(err)
	}
	if err := writeReferenceRoot(sharedManifest, index); err != nil {
		t.Fatal(err)
	}

	corruptDigest := manifest.Catalogs[0].Digest
	if err := os.WriteFile(adminContentObjectPath(corruptDigest), []byte("corrupt catalog"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := unavailableObjectProvider{Provider: remote, Digest: corruptDigest}
	if _, err := RepairFromProvider(context.Background(), digest, provider); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("RepairFromProvider error = %v, want provider unavailable", err)
	}

	sharedAfter, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("shared CAS object was removed: %v", err)
	}
	if !bytes.Equal(sharedAfter, sharedBefore) {
		t.Fatal("shared CAS object bytes changed during failed provider repair")
	}
	if root, err := readReferenceRoot(sharedManifest.ArtifactDigest); err != nil || root.Manifest != sharedManifest.ArtifactDigest {
		t.Fatalf("shared artifact reference root = %+v, %v", root, err)
	}
	if _, err := verifyLocalContentLocked(context.Background(), sharedManifest.ArtifactDigest); err != nil {
		t.Fatalf("shared artifact closure was damaged: %v", err)
	}
}

func TestRepairFromProviderPreservesUnreadableRegularCASObjectOnFailedRepair(t *testing.T) {
	_, remote, digest, manifest, index := acquiredAdminFixture(t)
	objectPath := adminContentObjectPath(index.Entries[0].StoredDigest)
	want, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(objectPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(objectPath, 0o600) })
	probe, probeErr := os.Open(objectPath)
	if probeErr == nil {
		_ = probe.Close()
		t.Skip("filesystem permits reading mode-zero files for this test user")
	}

	corruptDigest := manifest.Catalogs[0].Digest
	if err := os.WriteFile(adminContentObjectPath(corruptDigest), []byte("corrupt catalog"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := unavailableObjectProvider{Provider: remote, Digest: corruptDigest}
	_, err = RepairFromProvider(context.Background(), digest, provider)
	if !errors.Is(err, ErrMaterializationCorrupt) || errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("RepairFromProvider error = %v, want fail-closed local corruption", err)
	}

	if err := os.Chmod(objectPath, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatalf("unreadable regular CAS object was removed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("unreadable regular CAS object bytes changed")
	}
}

func TestVerifyInspectsClosureWhenReadyRecordMissing(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "missing-object", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "non-regular-object", mutate: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, digest, manifest, _ := acquiredAdminFixture(t)
			readyPath := filepath.Join(recordRoot(), digest.Hex(), string(stateReady)+".json")
			if err := os.Remove(readyPath); err != nil {
				t.Fatal(err)
			}
			testCase.mutate(t, adminContentObjectPath(manifest.ObjectIndex.Digest))

			inspection, err := Inspect(context.Background(), digest)
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Ready || !inspection.Corrupt || inspection.State != "corrupt" {
				t.Fatalf("inspection = %+v, want missing-ready corrupt closure", inspection)
			}

			verification, err := Verify(context.Background(), digest)
			if !errors.Is(err, ErrMaterializationCorrupt) || verification.Verified || verification.State != "corrupt" {
				t.Fatalf("verification = %+v, error = %v", verification, err)
			}
		})
	}
}

func TestVerifyLogicalObjectRejectsWrongLogicalDigest(t *testing.T) {
	logical := []byte("logical object")
	var stored bytes.Buffer
	writer := gzip.NewWriter(&stored)
	if _, err := writer.Write(logical); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	entry := environmentartifact.ObjectEntry{
		LegacyObjectID:               strings.Repeat("0", 64),
		StoredSize:                   int64(stored.Len()),
		LogicalSize:                  int64(len(logical)),
		Encoding:                     "gzip",
		LegacyLogicalDigestAlgorithm: "sha256",
	}
	if err := verifyLogicalObject(entry, stored.Bytes()); err == nil {
		t.Fatal("logical object with wrong digest was accepted")
	}
}

type unavailableObjectProvider struct {
	artifactprovider.Provider
	Digest environmentartifact.Digest
}

func (it unavailableObjectProvider) GetObject(ctx context.Context, descriptor environmentartifact.Descriptor) (io.ReadCloser, error) {
	if descriptor.Digest == it.Digest {
		return nil, errors.New("provider object unavailable")
	}
	return it.Provider.GetObject(ctx, descriptor)
}

func acquiredAdminFixture(t *testing.T) (publishFixture, artifactprovider.Provider, environmentartifact.Digest, environmentartifact.Manifest, environmentartifact.ObjectIndex) {
	t.Helper()
	fixture, remote, digest := publishedFixture(t)
	previous := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previous)
		common.SharedHolotree = previousShared
	})
	if _, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote}); err != nil {
		t.Fatal(err)
	}
	local, err := artifactprovider.NewFilesystem(localContentRoot())
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := local.ResolveManifest(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	indexBytes, err := readProviderObject(context.Background(), local, manifest.ObjectIndex)
	if err != nil {
		t.Fatal(err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, remote, digest, manifest, index
}

func adminContentObjectPath(digest environmentartifact.Digest) string {
	h := digest.Hex()
	return filepath.Join(localContentRoot(), "objects", "sha256", h[:2], h[2:4], h)
}

func adminContentManifestPath(digest environmentartifact.Digest) string {
	h := digest.Hex()
	return filepath.Join(localContentRoot(), "manifests", "sha256", h[:2], h[2:4], h)
}
