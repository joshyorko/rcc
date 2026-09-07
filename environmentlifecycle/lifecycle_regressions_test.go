package environmentlifecycle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestAcquireDoesNotRematerializeActiveLeaseTarget(t *testing.T) {
	_, remote, digest := publishedFixture(t)
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	materialization := Materialization{ArtifactDigest: result.ArtifactDigest, ID: result.MaterializationID, Path: result.Path}
	lease, err := NewLocalMaterializer().Lease(context.Background(), materialization)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_ = NewLocalMaterializer().Release(context.Background(), lease)
		}
	})

	if err := os.Remove(filepath.Join(recordRoot(), digest.Hex(), string(stateReady)+".json")); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(result.Path, "lease-holder-sentinel")
	if err := os.WriteFile(sentinel, []byte("holder must survive"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
	if err == nil || !errors.Is(err, ErrActiveLease) {
		t.Fatalf("acquire while lease held = %v, want an active-lease refusal", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("active lease target was modified or removed: %v", err)
	}
	reconciled, err := Reconcile(context.Background(), digest)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Active != 1 || reconciled.Stale != 0 || reconciled.Ambiguous != 0 {
		t.Fatalf("lease state after refused acquire = %+v", reconciled)
	}
	if err := NewLocalMaterializer().Release(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	released = true
	if _, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote}); err != nil {
		t.Fatalf("acquire after lease release = %v", err)
	}
}

func TestRepairFromProviderDefersMaterializationWhileLeaseIsActive(t *testing.T) {
	_, remote, digest := publishedFixture(t)
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: digest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := NewLocalMaterializer().Lease(context.Background(), Materialization{
		ArtifactDigest: digest, ID: result.MaterializationID, Path: result.Path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(recordRoot(), digest.Hex(), string(stateReady)+".json")); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(result.Path, "lease-holder-sentinel")
	if err := os.WriteFile(sentinel, []byte("holder must survive"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = RepairFromProvider(context.Background(), digest, remote)
	if err == nil || !errors.Is(err, ErrActiveLease) {
		t.Fatalf("repair while lease held = %v, want an active-lease refusal", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("active lease target was modified or removed: %v", err)
	}
	if err := NewLocalMaterializer().Release(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if report, err := RepairFromProvider(context.Background(), digest, remote); err != nil || !report.Repaired {
		t.Fatalf("repair after lease release = %+v, %v", report, err)
	}
}

func TestGCDryRunPreservesProvisionalRecordsAndReferenceRoot(t *testing.T) {
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	home := t.TempDir()
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	digest := environmentartifact.DigestBytes([]byte("dry-run-provisional"))
	id := materializationID(digest)
	path := filepath.Join(common.HolotreeLocation(), id)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "partial"), []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing} {
		if err := writeMaterializationRecord(materializationRecord{
			ArtifactDigest: digest, MaterializationID: id, Path: path, State: state,
			CreatedAt: time.Unix(1, 0).UTC(), VerifiedAt: time.Unix(1, 0).UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeReferenceRoot(environmentartifact.Manifest{ArtifactDigest: digest}, environmentartifact.ObjectIndex{}); err != nil {
		t.Fatal(err)
	}
	records := make(map[materializationState][]byte)
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing} {
		content, err := os.ReadFile(filepath.Join(recordRoot(), digest.Hex(), string(state)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		records[state] = content
	}
	rootPath := filepath.Join(recordRoot(), digest.Hex(), "references.json")
	rootBefore, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Collect(context.Background(), GCPolicy{ContentRoot: localContentRoot(), DryRun: true, Pressure: true, Clock: func() time.Time { return time.Unix(100, 0) }}); err != nil {
		t.Fatal(err)
	}
	for state, want := range records {
		got, err := os.ReadFile(filepath.Join(recordRoot(), digest.Hex(), string(state)+".json"))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("dry-run %s record = %q, %v; want %q", state, got, err, want)
		}
	}
	rootAfter, err := os.ReadFile(rootPath)
	if err != nil || !bytes.Equal(rootAfter, rootBefore) {
		t.Fatalf("dry-run reference root changed: %q, %v; want %q", rootAfter, err, rootBefore)
	}
	if _, err := os.Stat(filepath.Join(path, "partial")); err != nil {
		t.Fatalf("dry-run provisional materialization changed: %v", err)
	}
}

func TestGCRejectsProviderOnlyContentRoot(t *testing.T) {
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	home := t.TempDir()
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	providerRoot := filepath.Join(home, "artifacts", "v1", "provider")
	provider, err := artifactprovider.NewFilesystem(providerRoot)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("provider-owned-content")
	digest := environmentartifact.DigestBytes(content)
	if err := provider.PutObject(context.Background(), artifactprovider.Blob{
		Descriptor: environmentartifact.Descriptor{Digest: digest, Size: int64(len(content))}, Reader: bytes.NewReader(content),
	}); err != nil {
		t.Fatal(err)
	}

	_, err = Collect(context.Background(), GCPolicy{ContentRoot: providerRoot, Pressure: true, Clock: func() time.Time { return time.Unix(100, 0) }})
	if err == nil {
		t.Fatal("consumer GC accepted provider-owned content root")
	}
	h := digest.Hex()
	objectPath := filepath.Join(providerRoot, "objects", "sha256", h[:2], h[2:4], h)
	if got, readErr := os.ReadFile(objectPath); readErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("provider-owned content after rejected GC = %q, %v", got, readErr)
	}
}

func TestGCInterruptionAfterUnlinkLeavesRecoveryPointer(t *testing.T) {
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	home := t.TempDir()
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	contentRoot := localContentRoot()
	provider, err := artifactprovider.NewFilesystem(contentRoot)
	if err != nil {
		t.Fatal(err)
	}
	first := []byte("first-interrupted-object")
	second := []byte("second-interrupted-object")
	paths := make([]string, 0, 2)
	for index, content := range [][]byte{first, second} {
		digest := environmentartifact.DigestBytes(content)
		if err := provider.PutObject(context.Background(), artifactprovider.Blob{
			Descriptor: environmentartifact.Descriptor{Digest: digest, Size: int64(len(content))}, Reader: bytes.NewReader(content),
		}); err != nil {
			t.Fatal(err)
		}
		h := digest.Hex()
		path := filepath.Join(contentRoot, "objects", "sha256", h[:2], h[2:4], h)
		paths = append(paths, path)
		if err := os.Chtimes(path, time.Unix(int64(index+1), 0), time.Unix(int64(index+1), 0)); err != nil {
			t.Fatal(err)
		}
	}
	ctx := &cancelAfterRemovalContext{Context: context.Background(), watched: paths[0]}
	if _, err := Collect(ctx, GCPolicy{ContentRoot: contentRoot, Pressure: true, Retention: time.Second, Clock: func() time.Time { return time.Unix(100, 0) }}); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted GC error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("first object after interrupted unlink = %v, want removed", err)
	}
	if _, err := os.Stat(paths[1]); err != nil {
		t.Fatalf("second object after interrupted unlink = %v, want discoverable", err)
	}
	recoveryPath := filepath.Join(recordRoot(), "gc-recovery.json")
	recovery, err := os.ReadFile(recoveryPath)
	if err != nil || len(recovery) == 0 {
		t.Fatalf("GC recovery pointer = %q, %v", recovery, err)
	}
	if _, err := Collect(context.Background(), GCPolicy{ContentRoot: contentRoot, Pressure: true, Retention: time.Second, Clock: func() time.Time { return time.Unix(100, 0) }}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths[1]); !os.IsNotExist(err) {
		t.Fatalf("second object after recovery = %v, want removed", err)
	}
	if _, err := os.Stat(recoveryPath); !os.IsNotExist(err) {
		t.Fatalf("GC recovery pointer after recovery = %v, want absent", err)
	}
}
