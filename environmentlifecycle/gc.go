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
}

type GCReport struct {
	Scanned, Reclaimed, SkippedActive, SkippedAmbiguous int
	ReclaimedDigests                                    []environmentartifact.Digest
	Items                                               []GCItem
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
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	return collectLocked(ctx, policy)
}

func collectLocked(ctx context.Context, policy GCPolicy) (GCReport, error) {
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
		reconcile, err := reconcileLocked(ctx, digest)
		if err != nil {
			return report, err
		}
		if reconcile.Ambiguous > 0 {
			report.SkippedAmbiguous++
			report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "ambiguous-lease"})
			continue
		}
		if reconcile.Active > 0 {
			report.SkippedActive++
			report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "active-lease"})
			continue
		}
		record, err := readReadyRecord(digest)
		if err != nil {
			report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "missing-or-invalid-ready-record"})
			continue
		}
		want, _ := filepath.Abs(filepath.Join(common.HolotreeLocation(), record.MaterializationID))
		actual, _ := filepath.Abs(record.Path)
		if actual != want || filepath.Base(record.Path) != record.MaterializationID {
			report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "blocked", Reason: "ready-record-path-out-of-root"})
			continue
		}
		if policy.Retention > 0 && time.Since(record.VerifiedAt) < policy.Retention {
			continue
		}
		if policy.DryRun {
			report.Reclaimed++
			report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
			report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "dry-run", Reason: "eligible"})
			continue
		}
		if err := removeMaterialization(record.Path); err != nil {
			return report, err
		}
		if err := os.Remove(filepath.Join(recordRoot(), digest.Hex(), "ready.json")); err != nil && !os.IsNotExist(err) {
			return report, err
		}
		report.Reclaimed++
		report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "reclaimed", Reason: "eligible"})
	}
	return report, nil
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
