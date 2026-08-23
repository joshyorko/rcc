package environmentlifecycle

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

type GCPolicy struct {
	Retention time.Duration
	DryRun    bool
	Clock     func() time.Time
	MaxBytes  int64
	Pressure  bool
	LocalOnly map[string]bool
	Pinned    map[string]bool
	Legal     map[string]bool
	RemoteKnown map[string]bool
}

type GCReport struct {
	Scanned, Reclaimed, SkippedActive, SkippedAmbiguous int
	ReclaimedDigests                                    []environmentartifact.Digest
	Items                                               []GCItem
	ProtectedBytes, ReclaimableBytes, ReclaimedBytes    int64 `json:"protectedBytes"`
	LastVerified                                        time.Time `json:"lastVerified,omitempty"`
}
type GCItem struct {
	Digest string `json:"digest"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (it *LocalMaterializer) Collect(ctx context.Context, policy GCPolicy) (GCReport, error) {
	return Collect(ctx, policy)
}

func Collect(ctx context.Context, policy GCPolicy) (GCReport, error) {
	if err := crash(CrashBeforeGC); err != nil { return GCReport{}, err }
	if policy.Retention < 0 {
		policy.Retention = 0
	}
	if policy.Clock == nil { policy.Clock = time.Now }
	var report GCReport
	entries, err := os.ReadDir(recordRoot())
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if !entry.IsDir() || len(entry.Name()) != 64 {
			continue
		}
		digest, err := environmentartifact.ParseDigest("sha256:" + entry.Name())
		if err != nil {
			continue
		}
		lock := artifactLock(digest)
		lock.Lock()
		crossRelease, err := acquireCrossArtifactLock(digest)
		if err != nil {
			lock.Unlock()
			return report, err
		}
		one, err := collectDigestLocked(ctx, policy, digest)
		_ = crossRelease()
		lock.Unlock()
		if err != nil {
			return report, err
		}
		mergeGCReport(&report, one)
	}
	if err := crash(CrashAfterGC); err != nil { return report, err }; return report, nil
}

func collectDigestLocked(ctx context.Context, policy GCPolicy, digest environmentartifact.Digest) (GCReport, error) {
	var report GCReport
	reconcile, err := reconcileLocked(ctx, digest)
	if err != nil {
		return report, err
	}
	if reconcile.Ambiguous > 0 {
		report.SkippedAmbiguous++
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "ambiguous-lease"})
		return report, nil
	}
	key := digest.Hex()
	if policy.Pinned[key] || policy.Legal[key] || (policy.LocalOnly[key] && !policy.RemoteKnown[key]) {
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "protected", Reason: "retention-policy"})
		return report, nil
	}
	if reconcile.Active > 0 {
		report.SkippedActive++
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "active-lease"})
		return report, nil
	}
	record, err := readReadyRecord(digest)
	if err != nil {
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "missing-or-invalid-ready-record"})
		return report, nil
	}
	want, _ := filepath.Abs(filepath.Join(common.HolotreeLocation(), record.MaterializationID))
	actual, _ := filepath.Abs(record.Path)
	if actual != want || filepath.Base(record.Path) != record.MaterializationID {
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "blocked", Reason: "ready-record-path-out-of-root"})
		return report, nil
	}
	now := policy.Clock()
	report.LastVerified = record.VerifiedAt
	if policy.Retention > 0 && now.Sub(record.VerifiedAt) < policy.Retention {
		return report, nil
	}
	if policy.MaxBytes > 0 && !policy.Pressure {
		return report, nil
	}
	report.ReclaimableBytes = materializationSize(record.Path)
	if policy.DryRun {
		report.Reclaimed++
		report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "dry-run", Reason: "eligible"})
		return report, nil
	}
	if err := removeMaterialization(record.Path); err != nil {
		return report, err
	}
	if err := os.Remove(filepath.Join(recordRoot(), digest.Hex(), "ready.json")); err != nil && !os.IsNotExist(err) {
		return report, err
	}
	report.Reclaimed++
	report.ReclaimedBytes = report.ReclaimableBytes
	report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
	report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "reclaimed", Reason: "eligible"})
	return report, nil
}

func materializationSize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error { if err == nil && info.Mode().IsRegular() { total += info.Size() }; return nil })
	return total
}

func mergeGCReport(dst *GCReport, src GCReport) {
	dst.Scanned++
	dst.Reclaimed += src.Reclaimed
	dst.SkippedActive += src.SkippedActive
	dst.SkippedAmbiguous += src.SkippedAmbiguous
	dst.ReclaimedDigests = append(dst.ReclaimedDigests, src.ReclaimedDigests...)
	dst.Items = append(dst.Items, src.Items...)
}

func removeMaterialization(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse unsafe materialization root")
	}
	var paths []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink in materialization")
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(paths) - 1; i >= 0; i-- {
		if err := os.Remove(paths[i]); err != nil {
			return err
		}
	}
	return nil
}
