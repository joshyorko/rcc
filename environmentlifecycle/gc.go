package environmentlifecycle

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

type GCPolicy struct {
	Retention   time.Duration
	DryRun      bool
	Clock       func() time.Time
	MaxBytes    int64
	Pressure    bool
	LocalOnly   map[string]bool
	Pinned      map[string]bool
	Legal       map[string]bool
	RemoteKnown map[string]bool
	ContentRoot string
}

type GCReport struct {
	Scanned, Reclaimed, SkippedActive, SkippedAmbiguous int
	ReclaimedDigests                                    []environmentartifact.Digest
	Items                                               []GCItem
	ProtectedBytes                                      int64     `json:"protectedBytes"`
	ReclaimableBytes                                    int64     `json:"reclaimableBytes"`
	ReclaimedBytes                                      int64     `json:"reclaimedBytes"`
	LastVerified                                        time.Time `json:"lastVerified,omitempty"`
	ReferenceRoots                                      int       `json:"referenceRoots"`
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
	var report GCReport
	err := withContentTransaction(ctx, localContentRoot(), func(ctx context.Context) error {
		var err error
		report, err = collectLocked(ctx, policy)
		return err
	})
	return report, err
}

func collectLocked(ctx context.Context, policy GCPolicy) (GCReport, error) {
	if err := crash(CrashBeforeGC); err != nil {
		return GCReport{}, err
	}
	if policy.Retention < 0 {
		policy.Retention = 0
	}
	if policy.Clock == nil {
		policy.Clock = time.Now
	}
	var report GCReport
	entries, err := os.ReadDir(recordRoot())
	if os.IsNotExist(err) {
		entries = nil
	}
	if err != nil && !os.IsNotExist(err) {
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
	protected, roots, protectAll, err := durableProtectedDigests(policy)
	if err != nil {
		return report, err
	}
	report.ReferenceRoots = roots
	protectedBytes, reclaimableBytes, reclaimedBytes, err := collectUnreferencedContent(ctx, policy, protected, protectAll)
	if err != nil {
		return report, err
	}
	report.ProtectedBytes += protectedBytes
	report.ReclaimableBytes += reclaimableBytes
	report.ReclaimedBytes += reclaimedBytes
	if err := crash(CrashAfterGC); err != nil {
		return report, err
	}
	return report, nil
}

func durableProtectedDigests(policy GCPolicy) (map[environmentartifact.Digest]bool, int, bool, error) {
	protected := map[environmentartifact.Digest]bool{}
	protectAll := false
	entries, err := os.ReadDir(recordRoot())
	if os.IsNotExist(err) {
		return protected, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	roots := 0
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) != 64 {
			continue
		}
		digest, err := environmentartifact.ParseDigest("sha256:" + entry.Name())
		if err != nil {
			continue
		}
		leaseProtected := false
		leaseEmbedded := false
		leaseDir := filepath.Join(recordRoot(), digest.Hex(), "leases")
		if leaseEntries, readErr := os.ReadDir(leaseDir); readErr == nil {
			for _, leaseEntry := range leaseEntries {
				if leaseEntry.IsDir() || filepath.Ext(leaseEntry.Name()) != ".json" {
					continue
				}
				lease, leaseErr := readLease(digest, strings.TrimSuffix(leaseEntry.Name(), ".json"))
				if leaseErr != nil {
					protectAll = true
					continue
				}
				if classifyLease(lease) != LeaseStale {
					leaseProtected = true
					if len(lease.Protected) == 0 {
						protectAll = true
					} else {
						leaseEmbedded = true
						for _, d := range lease.Protected {
							protected[d] = true
						}
					}
				}
			}
		}
		if !referenceRootExists(digest) {
			if leaseProtected && !leaseEmbedded {
				protectAll = true
			}
			continue
		}
		root, rootErr := readReferenceRoot(digest)
		if rootErr != nil {
			if !leaseEmbedded {
				protectAll = true
			}
			continue
		}
		roots++
		protectRoot := root.State == "live" || leaseProtected
		policyProtected := policy.Pinned[digest.Hex()] || policy.Legal[digest.Hex()] || (policy.LocalOnly[digest.Hex()] && !policy.RemoteKnown[digest.Hex()])
		if root.State == "live" && !leaseProtected && !policyProtected {
			if _, readyErr := readReadyRecord(digest); os.IsNotExist(readyErr) {
				if retireErr := retireReferenceRoot(digest, policy.Clock()); retireErr == nil {
					root, _ = readReferenceRoot(digest)
					protectRoot = false
				}
			}
		}
		if root.State == "retired" && policy.Clock != nil && !root.RetiredAt.IsZero() && policy.Clock().Sub(root.RetiredAt) < policy.Retention {
			protectRoot = true
		}
		if policyProtected {
			protectRoot = true
		}
		if protectRoot {
			for _, d := range root.Protected {
				protected[d] = true
			}
		}
	}
	return protected, roots, protectAll, nil
}

func collectUnreferencedContent(ctx context.Context, policy GCPolicy, protected map[environmentartifact.Digest]bool, protectAll bool) (int64, int64, int64, error) {
	root := policy.ContentRoot
	if root == "" {
		root = filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
	}
	objects := filepath.Join(root, "objects", "sha256")
	type candidate struct {
		path     string
		digest   environmentartifact.Digest
		size     int64
		modified time.Time
	}
	candidates := make([]candidate, 0)
	var total, protectedBytes int64
	err := filepath.WalkDir(objects, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink in content CAS: %s", path)
		}
		if len(entry.Name()) != 64 {
			return nil
		}
		digest, err := environmentartifact.ParseDigest("sha256:" + entry.Name())
		if err != nil {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if protected[digest] {
			protectedBytes += info.Size()
			total += info.Size()
			return nil
		}
		total += info.Size()
		candidates = append(candidates, candidate{path: path, digest: digest, size: info.Size(), modified: info.ModTime()})
		return nil
	})
	if err != nil {
		return protectedBytes, 0, 0, err
	}
	if protectAll {
		for _, item := range candidates {
			protectedBytes += item.size
		}
		return protectedBytes, 0, 0, nil
	}
	allow := policy.Pressure || (policy.MaxBytes > 0 && total > policy.MaxBytes)
	if !allow {
		return protectedBytes, 0, 0, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modified.Before(candidates[j].modified) })
	if policy.Clock == nil {
		policy.Clock = time.Now
	}
	var reclaimableBytes, reclaimedBytes int64
	for _, item := range candidates {
		if policy.Retention > 0 && policy.Clock().Sub(item.modified) < policy.Retention {
			continue
		}
		if policy.MaxBytes > 0 && total <= policy.MaxBytes {
			break
		}
		reclaimableBytes += item.size
		if policy.DryRun {
			total -= item.size
			continue
		}
		if err := os.Remove(item.path); err != nil {
			return protectedBytes, reclaimableBytes, reclaimedBytes, err
		}
		total -= item.size
		reclaimedBytes += item.size
	}
	return protectedBytes, reclaimableBytes, reclaimedBytes, nil
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
	if referenceRootExists(digest) {
		if _, err := readReferenceRoot(digest); err != nil {
			report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "blocked", Reason: "invalid-reference-root"})
			return report, nil
		}
		report.ReferenceRoots = 1
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
	if referenceRootExists(digest) {
		if err := retireReferenceRoot(digest, now); err != nil {
			return report, err
		}
	}
	report.Reclaimed++
	report.ReclaimedBytes = report.ReclaimableBytes
	report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
	report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "reclaimed", Reason: "eligible"})
	return report, nil
}

func materializationSize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func mergeGCReport(dst *GCReport, src GCReport) {
	dst.Scanned++
	dst.Reclaimed += src.Reclaimed
	dst.ProtectedBytes += src.ProtectedBytes
	dst.ReclaimableBytes += src.ReclaimableBytes
	dst.ReclaimedBytes += src.ReclaimedBytes
	dst.ReferenceRoots += src.ReferenceRoots
	dst.SkippedActive += src.SkippedActive
	dst.SkippedAmbiguous += src.SkippedAmbiguous
	dst.ReclaimedDigests = append(dst.ReclaimedDigests, src.ReclaimedDigests...)
	dst.Items = append(dst.Items, src.Items...)
	if dst.LastVerified.Before(src.LastVerified) {
		dst.LastVerified = src.LastVerified
	}
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
