package environmentlifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/environmentartifact"
)

type LeaseStatus string

const (
	LeaseActive    LeaseStatus = "active"
	LeaseStale     LeaseStatus = "stale"
	LeaseAmbiguous LeaseStatus = "ambiguous"
)

type ReconcileReport struct {
	ArtifactDigest                  environmentartifact.Digest
	Active, Stale, Ambiguous        int
	Repaired                        []string
	Items                           []ReconcileItem
	Provisional, ProvisionalRemoved int `json:"provisional,omitempty"`
}

type ReconcileItem struct {
	ID       string      `json:"id"`
	Status   LeaseStatus `json:"status"`
	Reason   string      `json:"reason"`
	Repaired bool        `json:"repaired"`
}

func classifyLease(lease Lease) LeaseStatus {
	if lease.OwnerPID <= 0 || lease.OwnerStart == "" {
		return LeaseAmbiguous
	}
	identity, err := processIdentityLookup(lease.OwnerPID)
	if err != nil {
		if os.IsNotExist(err) {
			return LeaseStale
		}
		return LeaseAmbiguous
	}
	if identity == "" {
		return LeaseAmbiguous
	}
	if identity != lease.OwnerStart {
		return LeaseStale
	}
	return LeaseActive
}

func Reconcile(ctx context.Context, digest environmentartifact.Digest) (ReconcileReport, error) {
	var report ReconcileReport
	err := withArtifactTransaction(ctx, digest, func(ctx context.Context) error {
		var err error
		report, err = reconcileLocked(ctx, digest)
		return err
	})
	return report, err
}

func reconcileLocked(ctx context.Context, digest environmentartifact.Digest) (ReconcileReport, error) {
	if err := ctx.Err(); err != nil {
		return ReconcileReport{}, err
	}
	report := ReconcileReport{ArtifactDigest: digest}
	for _, state := range []materializationState{stateVerifiedContent, stateMaterializing} {
		path := filepath.Join(recordRoot(), digest.Hex(), string(state)+".json")
		if _, statErr := os.Stat(path); statErr == nil {
			report.Provisional++
			// These records are transactional intent, never readiness. Remove the
			// journal entry after a crash; the ready record remains authoritative.
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return report, err
			}
			report.ProvisionalRemoved++
		}
	}
	dir := filepath.Join(recordRoot(), digest.Hex(), "leases")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		lease, err := readLease(digest, id)
		if err != nil {
			report.Ambiguous++
			report.Items = append(report.Items, ReconcileItem{ID: id, Status: LeaseAmbiguous, Reason: "malformed-or-noncanonical-lease"})
			continue
		}
		switch classifyLease(lease) {
		case LeaseActive:
			report.Active++
			report.Items = append(report.Items, ReconcileItem{ID: id, Status: LeaseActive, Reason: "owner-identity-matches"})
		case LeaseStale:
			report.Stale++
			if err := removeRegularNoFollow(recordRoot(), leaseComponents(digest, id)); err != nil && !os.IsNotExist(err) {
				return report, err
			}
			report.Repaired = append(report.Repaired, id)
			report.Items = append(report.Items, ReconcileItem{ID: id, Status: LeaseStale, Reason: "owner-missing-or-pid-reused", Repaired: true})
		case LeaseAmbiguous:
			report.Ambiguous++
			report.Items = append(report.Items, ReconcileItem{ID: id, Status: LeaseAmbiguous, Reason: "owner-identity-unavailable-or-ambiguous"})
		}
	}
	return report, nil
}
