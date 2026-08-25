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

func TestGCPolicyKeepStateProtectsRetiredContent(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.SharedHolotree = previousShared
		common.Product.ForceHome(previous)
	})

	contentRoot := filepath.Join(home, "artifacts", "v1", "content")
	provider, err := artifactprovider.NewFilesystem(contentRoot)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("keep-me-immutable")
	digest := environmentartifact.DigestBytes(content)
	if err := provider.PutObject(context.Background(), artifactprovider.Blob{
		Descriptor: environmentartifact.Descriptor{Digest: digest, Size: int64(len(content))},
		Reader:     bytes.NewReader(content),
	}); err != nil {
		t.Fatal(err)
	}
	manifest := environmentartifact.Manifest{ArtifactDigest: environmentartifact.DigestBytes([]byte("kept-artifact"))}
	if err := writeReferenceRoot(manifest, environmentartifact.ObjectIndex{Entries: []environmentartifact.ObjectEntry{{StoredDigest: digest}}}); err != nil {
		t.Fatal(err)
	}
	if err := retireReferenceRoot(manifest.ArtifactDigest, time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}

	_, err = Collect(context.Background(), GCPolicy{
		ContentRoot: contentRoot,
		Pressure:    true,
		Keep:        map[string]bool{manifest.ArtifactDigest.Hex(): true},
		Clock:       func() time.Time { return time.Unix(20, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	objectPath := filepath.Join(contentRoot, "objects", "sha256", digest.Hex()[:2], digest.Hex()[2:4], digest.Hex())
	got, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("kept immutable content changed: %q", got)
	}
}

func TestGCInterruptionLeavesContentForRestart(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previous) })

	contentRoot := filepath.Join(home, "artifacts", "v1", "content")
	provider, err := artifactprovider.NewFilesystem(contentRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range [][]byte{[]byte("old-a"), []byte("old-b")} {
		digest := environmentartifact.DigestBytes(content)
		if err := provider.PutObject(context.Background(), artifactprovider.Blob{
			Descriptor: environmentartifact.Descriptor{Digest: digest, Size: int64(len(content))},
			Reader:     bytes.NewReader(content),
		}); err != nil {
			t.Fatal(err)
		}
		h := digest.Hex()
		if err := os.Chtimes(filepath.Join(contentRoot, "objects", "sha256", h[:2], h[2:4], h), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	clock := func() time.Time {
		cancel()
		return time.Unix(100, 0)
	}
	_, err = Collect(ctx, GCPolicy{ContentRoot: contentRoot, Pressure: true, Retention: time.Second, Clock: clock})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted GC error = %v", err)
	}

	for _, content := range [][]byte{[]byte("old-a"), []byte("old-b")} {
		digest := environmentartifact.DigestBytes(content)
		h := digest.Hex()
		if _, err := os.Stat(filepath.Join(contentRoot, "objects", "sha256", h[:2], h[2:4], h)); err != nil {
			t.Fatalf("interrupted GC removed %s: %v", content, err)
		}
	}
}

func TestGCInterruptionRecoversAfterMaterializationRemoval(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.SharedHolotree = previousShared
		common.Product.ForceHome(previous)
	})

	digest := environmentartifact.DigestBytes([]byte("interrupted-materialization"))
	materializationID := "h123456_123456789abcdeft"
	path := filepath.Join(common.HolotreeLocation(), materializationID)
	if err := writeMaterializationRecord(materializationRecord{
		ArtifactDigest: digest, MaterializationID: materializationID, Path: path,
		State: stateReady, VerifiedAt: time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeReferenceRoot(environmentartifact.Manifest{ArtifactDigest: digest}, environmentartifact.ObjectIndex{}); err != nil {
		t.Fatal(err)
	}

	if _, err := Collect(context.Background(), GCPolicy{Pressure: true, Clock: func() time.Time { return time.Unix(100, 0) }}); err != nil {
		t.Fatal(err)
	}
	if _, err := readReadyRecord(digest); !os.IsNotExist(err) {
		t.Fatalf("ready record survived restart cleanup: %v", err)
	}
	root, err := readReferenceRoot(digest)
	if err != nil || root.State != "retired" {
		t.Fatalf("reference root after restart cleanup = %+v, %v", root, err)
	}
}

func TestGCPolicyCleansProvisionalMaterialization(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.SharedHolotree = previousShared
		common.Product.ForceHome(previous)
	})

	digest := environmentartifact.DigestBytes([]byte("provisional-materialization"))
	materializationID := "h123456_123456789abcdeft"
	path := filepath.Join(common.HolotreeLocation(), materializationID)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "partial"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing} {
		if err := writeMaterializationRecord(materializationRecord{
			ArtifactDigest: digest, MaterializationID: materializationID, Path: path,
			State: state, VerifiedAt: time.Unix(1, 0),
		}); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Collect(context.Background(), GCPolicy{Pressure: true, Clock: func() time.Time { return time.Unix(100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("provisional materialization survived cleanup: %v", err)
	}
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing} {
		if _, err := os.Stat(filepath.Join(recordRoot(), digest.Hex(), string(state)+".json")); !os.IsNotExist(err) {
			t.Fatalf("provisional %s record survived cleanup: %v", state, err)
		}
	}
}

func TestLocalOnlyRemoteKnownPolicyAllowsBoundedReclamation(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.SharedHolotree = previousShared
		common.Product.ForceHome(previous)
	})

	contentRoot := filepath.Join(home, "artifacts", "v1", "content")
	provider, err := artifactprovider.NewFilesystem(contentRoot)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("remote-known-content")
	contentDigest := environmentartifact.DigestBytes(content)
	if err := provider.PutObject(context.Background(), artifactprovider.Blob{
		Descriptor: environmentartifact.Descriptor{Digest: contentDigest, Size: int64(len(content))},
		Reader:     bytes.NewReader(content),
	}); err != nil {
		t.Fatal(err)
	}
	manifestDigest := environmentartifact.DigestBytes([]byte("remote-known-artifact"))
	if err := writeReferenceRoot(environmentartifact.Manifest{ArtifactDigest: manifestDigest}, environmentartifact.ObjectIndex{Entries: []environmentartifact.ObjectEntry{{StoredDigest: contentDigest}}}); err != nil {
		t.Fatal(err)
	}
	if err := retireReferenceRoot(manifestDigest, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	policy := GCPolicy{
		ContentRoot: contentRoot,
		Pressure:    true,
		LocalOnly:   map[string]bool{manifestDigest.Hex(): true},
		RemoteKnown: map[string]bool{manifestDigest.Hex(): true},
		Clock:       func() time.Time { return time.Unix(100, 0) },
	}
	if _, err := Collect(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	h := contentDigest.Hex()
	if _, err := os.Stat(filepath.Join(contentRoot, "objects", "sha256", h[:2], h[2:4], h)); !os.IsNotExist(err) {
		t.Fatalf("remote-known local-only content survived bounded cleanup: %v", err)
	}
}

func TestReferenceRootProtectsSharedContentAcrossArtifacts(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previous) })

	contentRoot := filepath.Join(home, "artifacts", "v1", "content")
	provider, err := artifactprovider.NewFilesystem(contentRoot)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("shared-content")
	contentDigest := environmentartifact.DigestBytes(content)
	if err := provider.PutObject(context.Background(), artifactprovider.Blob{
		Descriptor: environmentartifact.Descriptor{Digest: contentDigest, Size: int64(len(content))},
		Reader:     bytes.NewReader(content),
	}); err != nil {
		t.Fatal(err)
	}
	keep := make(map[string]bool)
	for index, state := range []string{"live", "retired"} {
		manifestDigest := environmentartifact.DigestBytes([]byte{byte('a' + index)})
		if err := writeReferenceRoot(environmentartifact.Manifest{ArtifactDigest: manifestDigest}, environmentartifact.ObjectIndex{Entries: []environmentartifact.ObjectEntry{{StoredDigest: contentDigest}}}); err != nil {
			t.Fatal(err)
		}
		if state == "retired" {
			if err := retireReferenceRoot(manifestDigest, time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
		} else {
			keep[manifestDigest.Hex()] = true
		}
	}

	if _, err := Collect(context.Background(), GCPolicy{ContentRoot: contentRoot, Pressure: true, Keep: keep, Clock: func() time.Time { return time.Unix(100, 0) }}); err != nil {
		t.Fatal(err)
	}
	h := contentDigest.Hex()
	if _, err := os.Stat(filepath.Join(contentRoot, "objects", "sha256", h[:2], h[2:4], h)); err != nil {
		t.Fatalf("shared content was reclaimed through one retired root: %v", err)
	}
}

func TestReferenceRootRejectsMaterializationSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.SharedHolotree = previousShared
		common.Product.ForceHome(previous)
	})

	digest := environmentartifact.DigestBytes([]byte("symlinked-materialization"))
	materializationID := "h123456_123456789abcdeft"
	outside := t.TempDir()
	outsideMaterialization := filepath.Join(outside, materializationID)
	if err := os.MkdirAll(outsideMaterialization, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideMaterialization, "immutable"), []byte("do-not-delete"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, common.HolotreeLocation()); err != nil {
		t.Fatal(err)
	}
	if err := writeMaterializationRecord(materializationRecord{
		ArtifactDigest:    digest,
		MaterializationID: materializationID,
		Path:              filepath.Join(common.HolotreeLocation(), materializationID),
		State:             stateReady,
		VerifiedAt:        time.Unix(1, 0),
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(context.Background(), GCPolicy{Pressure: true, Clock: func() time.Time { return time.Unix(100, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) == 0 || report.Items[0].Status != "blocked" {
		t.Fatalf("symlink escape was not blocked: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(outsideMaterialization, "immutable")); err != nil {
		t.Fatalf("GC damaged escaped materialization: %v", err)
	}
}
