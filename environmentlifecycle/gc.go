package environmentlifecycle

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type GCPolicy struct {
	Retention time.Duration
	DryRun    bool
}

type GCReport struct {
	Scanned, Reclaimed, SkippedActive, SkippedAmbiguous int
	ReclaimedDigests                                    []environmentartifact.Digest
}

func (it *LocalMaterializer) Collect(ctx context.Context, policy GCPolicy) (GCReport, error) {
	return Collect(ctx, policy)
}

func Collect(ctx context.Context, policy GCPolicy) (GCReport, error) {
	var report GCReport
	if policy.Retention < 0 {
		policy.Retention = 0
	}
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
		report.Scanned++
		reconcile, err := Reconcile(ctx, digest)
		if err != nil {
			return report, err
		}
		if reconcile.Ambiguous > 0 {
			report.SkippedAmbiguous++
			continue
		}
		if reconcile.Active > 0 {
			report.SkippedActive++
			continue
		}
		record, err := readReadyRecord(digest)
		if err != nil {
			continue
		}
		if policy.Retention > 0 && time.Since(record.VerifiedAt) < policy.Retention {
			continue
		}
		if policy.DryRun {
			report.Reclaimed++
			report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
			continue
		}
		if err := os.RemoveAll(record.Path); err != nil {
			return report, err
		}
		if err := os.Remove(filepath.Join(recordRoot(), digest.Hex(), "ready.json")); err != nil && !os.IsNotExist(err) {
			return report, err
		}
		report.Reclaimed++
		report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
	}
	return report, nil
}
