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
	retained, err := Collect(context.Background(), GCPolicy{ContentRoot: contentRoot, Pressure: true, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if retained.ReclaimedBytes != 0 {
		t.Fatalf("retention policy reclaimed fresh content: %+v", retained)
	}
	report, err := Collect(context.Background(), GCPolicy{ContentRoot: contentRoot, Pressure: true})
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
