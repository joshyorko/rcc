package environmentlifecycle

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
	"strings"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestGCReportUsesDistinctByteFields(t *testing.T) {
	b, err := json.Marshal(GCReport{ProtectedBytes: 1, ReclaimableBytes: 2, ReclaimedBytes: 3})
	if err != nil { t.Fatal(err) }
	var got map[string]int64
	if err := json.Unmarshal(b, &got); err != nil { t.Fatal(err) }
	if got["protectedBytes"] != 1 || got["reclaimableBytes"] != 2 || got["reclaimedBytes"] != 3 { t.Fatalf("byte fields = %v", got) }
}

func TestCrashMatrixExecutesEveryLifecyclePoint(t *testing.T) {
	defer SetCrashHook(nil)
	seen := map[CrashPoint]bool{}
	SetCrashHook(func(point CrashPoint) error { seen[point] = true; return errors.New("injected crash") })
	for _, point := range CrashPoints() { _ = crash(point) }
	for _, point := range CrashPoints() { if !seen[point] { t.Fatalf("crash point %q was not executed", point) } }
}

func TestReferenceGraphProtectsSharedClosure(t *testing.T) {
	d := func(ch byte) environmentartifact.Digest { v,_:=environmentartifact.ParseDigest("sha256:"+strings.Repeat(string(ch),64));return v }
	manifest:=environmentartifact.Manifest{ArtifactDigest:d('a'),Specification:environmentartifact.Specification{Descriptor:environmentartifact.Descriptor{Digest:d('b')}},LegacyBlueprint:environmentartifact.LegacyBlueprint{Descriptor:environmentartifact.Descriptor{Digest:d('c')}},Catalogs:[]environmentartifact.CatalogDescriptor{{Descriptor:environmentartifact.Descriptor{Digest:d('d')}}},ObjectIndex:environmentartifact.Descriptor{Digest:d('e')}}
	graph:=BuildReferenceGraph(manifest,environmentartifact.ObjectIndex{Entries:[]environmentartifact.ObjectEntry{{StoredDigest:d('f')}}});if len(graph.Protected)!=5{t.Fatalf("protected=%d",len(graph.Protected))}
}

func TestRetentionEligibilityUsesInjectedClock(t *testing.T) {
	now := time.Unix(100, 0)
	record := materializationRecord{VerifiedAt: now.Add(-10 * time.Second)}
	if RetentionEligible(record, now, 11*time.Second) { t.Fatal("record incorrectly eligible") }
	if !RetentionEligible(record, now, 10*time.Second) { t.Fatal("record at retention boundary not eligible") }
}
