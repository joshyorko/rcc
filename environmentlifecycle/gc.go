package environmentlifecycle

import (
	"bytes"
	"context"
	"encoding/json"
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
	Keep        map[string]bool
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
	if err := validateGCContentRoot(gcContentRoot(policy)); err != nil {
		return report, err
	}
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
	if err := validateGCContentRoot(gcContentRoot(policy)); err != nil {
		return GCReport{}, err
	}
	if policy.Retention < 0 {
		policy.Retention = 0
	}
	if policy.Clock == nil {
		policy.Clock = time.Now
	}
	var report GCReport
	if err := validateGCDirectory(recordRoot()); err != nil {
		return report, err
	}
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
		mergeGCReport(&report, one)
		if err != nil {
			return report, err
		}
	}
	protected, roots, protectAll, err := durableProtectedDigests(policy)
	if err != nil {
		return report, err
	}
	report.ReferenceRoots = roots
	protectedBytes, reclaimableBytes, reclaimedBytes, err := collectUnreferencedContent(ctx, policy, protected, protectAll, &report)
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
	if err := validateGCDirectory(recordRoot()); err != nil {
		return nil, 0, false, err
	}
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
		_, readyErr := readReadyRecord(digest)
		root, rootErr := readReferenceRoot(digest)
		if rootErr != nil {
			if readyErr == nil || !leaseEmbedded {
				protectAll = true
			}
			continue
		}
		roots++
		protectRoot := root.State == "live" || leaseProtected
		policyProtected := gcPolicyProtects(policy, digest)
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

func collectUnreferencedContent(ctx context.Context, policy GCPolicy, protected map[environmentartifact.Digest]bool, protectAll bool, report *GCReport) (int64, int64, int64, error) {
	root := gcContentRoot(policy)
	if err := validateGCContentRoot(root); err != nil {
		return 0, 0, 0, err
	}
	objects := filepath.Join(root, "objects", "sha256")
	if err := validateGCDirectory(objects); err != nil {
		return 0, 0, 0, err
	}
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
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modified.Equal(candidates[j].modified) {
			return candidates[i].digest.Hex() < candidates[j].digest.Hex()
		}
		return candidates[i].modified.Before(candidates[j].modified)
	})
	if policy.Clock == nil {
		policy.Clock = time.Now
	}
	var reclaimableBytes, reclaimedBytes int64
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return protectedBytes, reclaimableBytes, reclaimedBytes, err
		}
		if policy.Retention > 0 && policy.Clock().Sub(item.modified) < policy.Retention {
			report.Items = append(report.Items, GCItem{Digest: item.digest.String(), Status: "skipped", Reason: "unreferenced-content-retention"})
			continue
		}
		if err := ctx.Err(); err != nil {
			return protectedBytes, reclaimableBytes, reclaimedBytes, err
		}
		if policy.MaxBytes > 0 && total <= policy.MaxBytes {
			break
		}
		reclaimableBytes += item.size
		if policy.DryRun {
			total -= item.size
			report.Items = append(report.Items, GCItem{Digest: item.digest.String(), Status: "dry-run", Reason: "unreferenced-content"})
			continue
		}
		h := item.digest.Hex()
		if err := removeRegularNoFollow(root, []string{"objects", "sha256", h[:2], h[2:4], h}); err != nil && !os.IsNotExist(err) {
			return protectedBytes, reclaimableBytes, reclaimedBytes, err
		}
		total -= item.size
		reclaimedBytes += item.size
		report.Items = append(report.Items, GCItem{Digest: item.digest.String(), Status: "reclaimed", Reason: "unreferenced-content"})
	}
	return protectedBytes, reclaimableBytes, reclaimedBytes, nil
}

func gcContentRoot(policy GCPolicy) string {
	if policy.ContentRoot != "" {
		return policy.ContentRoot
	}
	return filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
}

func collectDigestLocked(ctx context.Context, policy GCPolicy, digest environmentartifact.Digest) (GCReport, error) {
	var report GCReport
	if err := validateProvisionalRecords(digest); err != nil {
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "blocked", Reason: "invalid-provisional-record"})
		return report, nil
	}
	incomplete, err := incompleteMaterializations(digest)
	if err != nil {
		return report, err
	}
	reconcile, err := reconcileLocked(ctx, digest)
	if err != nil {
		return report, err
	}
	if reconcile.Ambiguous > 0 {
		report.SkippedAmbiguous++
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "ambiguous-lease"})
		return report, nil
	}
	if gcPolicyProtects(policy, digest) {
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
		if len(incomplete) > 0 {
			for _, path := range incomplete {
				if err := ctx.Err(); err != nil {
					return report, err
				}
				removed, removeErr := removeMaterializationContext(ctx, path)
				if removeErr != nil {
					appendMaterializationRemovalReport(&report, digest, removed)
					return report, removeErr
				}
			}
			report.Reclaimed++
			report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "reclaimed", Reason: "provisional-materialization"})
		}
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "skipped", Reason: "missing-or-invalid-ready-record"})
		return report, nil
	}
	if record.MaterializationID != materializationID(digest) {
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "blocked", Reason: "ready-record-materialization-id-mismatch"})
		return report, nil
	}
	_, rootErr := readReferenceRoot(digest)
	if rootErr != nil {
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "blocked", Reason: "invalid-reference-root"})
		return report, nil
	}
	report.ReferenceRoots = 1
	if err := validateMaterializationPath(record.Path, record.MaterializationID); err != nil {
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
	report.ReclaimableBytes, err = materializationSize(record.Path)
	if err != nil {
		return report, err
	}
	if policy.DryRun {
		report.Reclaimed++
		report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
		report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "dry-run", Reason: "eligible"})
		return report, nil
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	removed, removeErr := removeMaterializationContext(ctx, record.Path)
	if removeErr != nil {
		appendMaterializationRemovalReport(&report, digest, removed)
		return report, removeErr
	}
	if err := ctx.Err(); err != nil {
		appendMaterializationRemovalReport(&report, digest, removed)
		return report, err
	}
	if err := removeRegularNoFollow(recordRoot(), recordComponents(digest, stateReady)); err != nil && !os.IsNotExist(err) {
		return report, err
	}
	if err := retireReferenceRoot(digest, now); err != nil {
		return report, err
	}
	report.Reclaimed++
	report.ReclaimedBytes = report.ReclaimableBytes
	report.ReclaimedDigests = append(report.ReclaimedDigests, digest)
	report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: "reclaimed", Reason: "eligible"})
	return report, nil
}

func gcPolicyProtects(policy GCPolicy, digest environmentartifact.Digest) bool {
	key := digest.Hex()
	return policy.Pinned[key] || policy.Legal[key] || policy.Keep[key] || (policy.LocalOnly[key] && !policy.RemoteKnown[key])
}

func incompleteMaterializations(digest environmentartifact.Digest) ([]string, error) {
	paths := make([]string, 0, 2)
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing} {
		content, err := readRegularNoFollow(recordRoot(), recordComponents(digest, state), maxMaterializationRecordBytes)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var record materializationRecord
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil || record.ArtifactDigest != digest || record.State != state || record.MaterializationID != materializationID(digest) {
			continue
		}
		canonical, err := json.Marshal(record)
		if err != nil || !bytes.Equal(canonical, content) {
			continue
		}
		if err := validateMaterializationPath(record.Path, record.MaterializationID); err != nil {
			return nil, err
		}
		paths = append(paths, record.Path)
	}
	return paths, nil
}

func validateProvisionalRecords(digest environmentartifact.Digest) error {
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing} {
		content, err := readRegularNoFollow(recordRoot(), recordComponents(digest, state), maxMaterializationRecordBytes)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var record materializationRecord
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return fmt.Errorf("decode provisional materialization record: %w", err)
		}
		if record.ArtifactDigest != digest || record.State != state {
			return fmt.Errorf("provisional materialization record identity mismatch")
		}
		canonical, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if !bytes.Equal(canonical, content) {
			return fmt.Errorf("provisional materialization record is not canonical")
		}
		if record.MaterializationID == materializationID(digest) {
			if err := validateMaterializationPath(record.Path, record.MaterializationID); err != nil {
				return err
			}
			continue
		}
		if err := validateAbsentOrphanMaterializationPath(record.Path, record.MaterializationID); err != nil {
			return err
		}
	}
	return nil
}

func validateAbsentOrphanMaterializationPath(path, id string) error {
	if id == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != id {
		return fmt.Errorf("refuse unsafe orphan materialization path")
	}
	root, err := filepath.Abs(common.HolotreeLocation())
	if err != nil {
		return err
	}
	expected := filepath.Join(root, id)
	actual, err := filepath.Abs(path)
	if err != nil || actual != expected {
		return fmt.Errorf("refuse orphan materialization path outside root")
	}
	if err := validateGCDirectory(root); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refuse existing orphan materialization path")
	}
	return fmt.Errorf("orphan materialization path exists")
}

func materializationSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink in materialization")
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
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

func appendMaterializationRemovalReport(report *GCReport, digest environmentartifact.Digest, removed int) {
	reason := "materialization-removal-partial"
	status := "partial"
	if removed == 0 {
		reason = "materialization-removal-blocked"
		status = "blocked"
	}
	report.Items = append(report.Items, GCItem{Digest: digest.String(), Status: status, Reason: reason})
}

func removeMaterialization(path string) error {
	_, err := removeMaterializationContext(context.Background(), path)
	return err
}

func validateGCDirectory(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := filepath.VolumeName(abs) + string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(abs), current), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refuse unsafe GC directory: %s", path)
		}
	}
	return nil
}

func validateMaterializationPath(path, id string) error {
	if id == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != id {
		return fmt.Errorf("refuse unsafe materialization path")
	}
	root, err := filepath.Abs(common.HolotreeLocation())
	if err != nil {
		return err
	}
	expected := filepath.Join(root, id)
	actual, err := filepath.Abs(path)
	if err != nil || actual != expected {
		return fmt.Errorf("refuse materialization path outside root")
	}
	if err := validateGCDirectory(root); err != nil {
		return err
	}
	return validateGCDirectory(path)
}
