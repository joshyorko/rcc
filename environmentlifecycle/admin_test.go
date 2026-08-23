package environmentlifecycle

import (
	"encoding/json"
	"errors"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"strings"
	"testing"
	"time"
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
