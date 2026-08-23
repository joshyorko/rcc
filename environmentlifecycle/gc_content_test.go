package environmentlifecycle

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestGCProtectsManifestObjectIndexClosureAndReclaimsUnreferencedContent(t *testing.T) {
	home := t.TempDir()
	previous := common.Product.Home()
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() { common.Product.ForceHome(previous) })
	contentRoot := filepath.Join(home, "artifacts", "v1", "content")
	provider, err := artifactprovider.NewFilesystem(contentRoot)
	if err != nil {
		t.Fatal(err)
	}
	put := func(content []byte) environmentartifact.Digest {
		digest := environmentartifact.DigestBytes(content)
		descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: digest, Size: int64(len(content))}
		if err := provider.PutObject(context.Background(), artifactprovider.Blob{Descriptor: descriptor, Reader: bytes.NewReader(content)}); err != nil {
			t.Fatal(err)
		}
		return digest
	}
	live := put([]byte("live-payload"))
	dead := put([]byte("dead-payload"))
	indexDigest := environmentartifact.DigestBytes([]byte("index"))
	manifestDigest := environmentartifact.DigestBytes([]byte("manifest"))
	manifest := environmentartifact.Manifest{ArtifactDigest: manifestDigest, ObjectIndex: environmentartifact.Descriptor{Digest: indexDigest}}
	index := environmentartifact.ObjectIndex{Entries: []environmentartifact.ObjectEntry{{StoredDigest: live}}}
	if err := writeReferenceRoot(manifest, index); err != nil {
		t.Fatal(err)
	}
	pinned := map[string]bool{manifestDigest.Hex(): true}
	defaultReport, err := Collect(context.Background(), GCPolicy{ContentRoot: contentRoot})
	if err != nil {
		t.Fatal(err)
	}
	if defaultReport.ReclaimedBytes != 0 {
		t.Fatalf("default GC reclaimed sole local content: %+v", defaultReport)
	}
	if _, err := os.Stat(filepath.Join(contentRoot, "objects", "sha256", dead.Hex()[:2], dead.Hex()[2:4], dead.Hex())); err != nil {
		t.Fatalf("default GC removed unreferenced content: %v", err)
	}
	retained, err := Collect(context.Background(), GCPolicy{ContentRoot: contentRoot, Pressure: true, Retention: time.Hour, Pinned: pinned})
	if err != nil {
		t.Fatal(err)
	}
	if retained.ReclaimedBytes != 0 {
		t.Fatalf("retention policy reclaimed fresh content: %+v", retained)
	}
	report, err := Collect(context.Background(), GCPolicy{ContentRoot: contentRoot, Pressure: true, Pinned: pinned})
	if err != nil {
		t.Fatal(err)
	}
	if report.ReferenceRoots != 1 || report.ProtectedBytes != int64(len("live-payload")) {
		t.Fatalf("protected report = %+v", report)
	}
	if report.ReclaimableBytes != int64(len("dead-payload")) || report.ReclaimedBytes != report.ReclaimableBytes {
		t.Fatalf("reclaim report = %+v", report)
	}
	objectPath := func(d environmentartifact.Digest) string {
		h := d.Hex()
		return filepath.Join(contentRoot, "objects", "sha256", h[:2], h[2:4], h)
	}
	if _, err := os.Stat(objectPath(live)); err != nil {
		t.Fatalf("protected payload removed: %v", err)
	}
	if _, err := os.Stat(objectPath(dead)); !os.IsNotExist(err) {
		t.Fatalf("unreferenced payload still exists: %v", err)
	}
}

func TestGCUsesActiveLeaseProtectedContentWhilePressureReclaimsDeadObjects(t *testing.T) {
	m := acquiredMaterialization(t)
	lease, err := NewLocalMaterializer().Lease(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = NewLocalMaterializer().Release(context.Background(), lease) }()
	provider, err := artifactprovider.NewFilesystem(localContentRoot())
	if err != nil {
		t.Fatal(err)
	}
	dead := []byte("active-lease-dead-object")
	digest := environmentartifact.DigestBytes(dead)
	if err := provider.PutObject(context.Background(), artifactprovider.Blob{Descriptor: environmentartifact.Descriptor{Digest: digest, Size: int64(len(dead))}, Reader: bytes.NewReader(dead)}); err != nil {
		t.Fatal(err)
	}
	report, err := Collect(context.Background(), GCPolicy{Pressure: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.ProtectedBytes == 0 || report.SkippedActive == 0 {
		t.Fatalf("active lease protection missing: %+v", report)
	}
	h := digest.Hex()
	if _, err := os.Stat(filepath.Join(localContentRoot(), "objects", "sha256", h[:2], h[2:4], h)); !os.IsNotExist(err) {
		t.Fatalf("dead object retained: %v", err)
	}
}

func TestMaterializationEvictionRetiresRootBeforeCASReclamation(t *testing.T) {
	m := acquiredMaterialization(t)
	retention := time.Hour
	report, err := Collect(context.Background(), GCPolicy{Pressure: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Reclaimed == 0 {
		t.Fatalf("materialization was not evicted: %+v", report)
	}
	root, err := readReferenceRoot(m.ArtifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	if root.State != "retired" || root.Manifest != m.ArtifactDigest {
		t.Fatalf("reference root lifecycle = %+v", root)
	}
	future := time.Now().Add(2 * retention)
	if _, err := Collect(context.Background(), GCPolicy{Pressure: true, Retention: retention, Clock: func() time.Time { return future }}); err != nil {
		t.Fatal(err)
	}
	if _, err := readReadyRecord(m.ArtifactDigest); !os.IsNotExist(err) {
		t.Fatalf("ready record survived eviction: %v", err)
	}
}

func TestActiveLeaseEmbeddedClosureSurvivesDamagedReferenceRoot(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			m := acquiredMaterialization(t)
			lease, err := NewLocalMaterializer().Lease(context.Background(), m)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = NewLocalMaterializer().Release(context.Background(), lease) }()
			if len(lease.Protected) == 0 {
				t.Fatal("lease did not persist protected closure")
			}
			rootPath := filepath.Join(recordRoot(), m.ArtifactDigest.Hex(), "references.json")
			if mode == "missing" {
				if err := os.Remove(rootPath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(rootPath, []byte("corrupt"), 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := Collect(context.Background(), GCPolicy{Pressure: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.ProtectedBytes == 0 {
				t.Fatalf("active lease closure was not protected: %+v", report)
			}
			digest := lease.Protected[0]
			h := digest.Hex()
			if _, err := os.Stat(filepath.Join(localContentRoot(), "objects", "sha256", h[:2], h[2:4], h)); err != nil {
				t.Fatalf("active lease payload removed: %v", err)
			}
		})
	}
}

func TestRetiredRootHonorsPinnedLegalAndLocalOnlyPolicies(t *testing.T) {
	for _, policyName := range []string{"pinned", "legal", "local-only"} {
		t.Run(policyName, func(t *testing.T) {
			m := acquiredMaterialization(t)
			root, err := readReferenceRoot(m.ArtifactDigest)
			if err != nil || len(root.Protected) == 0 {
				t.Fatalf("root = %+v, err=%v", root, err)
			}
			if err := retireReferenceRoot(m.ArtifactDigest, time.Now()); err != nil {
				t.Fatal(err)
			}
			key := m.ArtifactDigest.Hex()
			policy := GCPolicy{Pressure: true}
			switch policyName {
			case "pinned":
				policy.Pinned = map[string]bool{key: true}
			case "legal":
				policy.Legal = map[string]bool{key: true}
			default:
				policy.LocalOnly = map[string]bool{key: true}
			}
			if policy.LocalOnly != nil {
				policy.RemoteKnown = map[string]bool{}
			}
			if _, err := Collect(context.Background(), policy); err != nil {
				t.Fatal(err)
			}
			digest := root.Protected[0]
			h := digest.Hex()
			if _, err := os.Stat(filepath.Join(localContentRoot(), "objects", "sha256", h[:2], h[2:4], h)); err != nil {
				t.Fatalf("policy %s removed protected payload: %v", policyName, err)
			}
		})
	}
}

func TestRetiredClosureReclaimsAfterRetentionUnderPressureAndMaxBytes(t *testing.T) {
	m := acquiredMaterialization(t)
	root, err := readReferenceRoot(m.ArtifactDigest)
	if err != nil || len(root.Protected) == 0 {
		t.Fatalf("root = %+v, err=%v", root, err)
	}
	protected := root.Protected[0]
	if err := removeMaterialization(m.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(recordRoot(), m.ArtifactDigest.Hex(), "ready.json")); err != nil {
		t.Fatal(err)
	}
	retiredAt := time.Now()
	if err := retireReferenceRoot(m.ArtifactDigest, retiredAt); err != nil {
		t.Fatal(err)
	}
	retention := time.Hour
	if report, err := Collect(context.Background(), GCPolicy{Pressure: true, Retention: retention}); err != nil {
		t.Fatal(err)
	} else if report.ProtectedBytes == 0 {
		t.Fatalf("retired closure was not retained: %+v", report)
	}
	h := protected.Hex()
	objectPath := filepath.Join(localContentRoot(), "objects", "sha256", h[:2], h[2:4], h)
	if _, err := os.Stat(objectPath); err != nil {
		t.Fatalf("retained closure missing: %v", err)
	}
	future := retiredAt.Add(2 * retention)
	if report, err := Collect(context.Background(), GCPolicy{Retention: retention, MaxBytes: 1, Clock: func() time.Time { return future }}); err != nil {
		t.Fatal(err)
	} else if report.ReclaimedBytes == 0 {
		t.Fatalf("aged retired closure was not reclaimed: %+v", report)
	}
	if _, err := os.Stat(objectPath); !os.IsNotExist(err) {
		t.Fatalf("aged retired closure survived reclamation: %v", err)
	}
}

func TestAmbiguousIdentityLeaseProtectsEmbeddedClosureWithDamagedRoot(t *testing.T) {
	m := acquiredMaterialization(t)
	lease, err := NewLocalMaterializer().Lease(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	previous := processIdentityLookup
	processIdentityLookup = func(int) (string, error) { return "", nil }
	t.Cleanup(func() {
		processIdentityLookup = previous
		_ = NewLocalMaterializer().Release(context.Background(), lease)
	})
	if len(lease.Protected) == 0 {
		t.Fatal("lease did not persist embedded closure")
	}
	if err := os.Remove(filepath.Join(recordRoot(), m.ArtifactDigest.Hex(), "references.json")); err != nil {
		t.Fatal(err)
	}
	report, err := Collect(context.Background(), GCPolicy{Pressure: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.SkippedAmbiguous == 0 || report.ProtectedBytes == 0 {
		t.Fatalf("ambiguous lease protection missing: %+v", report)
	}
	digest := lease.Protected[0]
	h := digest.Hex()
	if _, err := os.Stat(filepath.Join(localContentRoot(), "objects", "sha256", h[:2], h[2:4], h)); err != nil {
		t.Fatalf("ambiguous lease closure removed: %v", err)
	}
}
