package environmentlifecycle

import (
	"context"
	"fmt"
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
	ArtifactDigest           environmentartifact.Digest
	Active, Stale, Ambiguous int
	Repaired                 []string
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
	if err := ctx.Err(); err != nil {
		return ReconcileReport{}, err
	}
	report := ReconcileReport{ArtifactDigest: digest}
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
			continue
		}
		switch classifyLease(lease) {
		case LeaseActive:
			report.Active++
		case LeaseStale:
			report.Stale++
			if err := removeRegularNoFollow(recordRoot(), leaseComponents(digest, id)); err != nil && !os.IsNotExist(err) {
				return report, err
			}
			report.Repaired = append(report.Repaired, id)
		case LeaseAmbiguous:
			report.Ambiguous++
		}
	}
	return report, nil
}

var errAmbiguousLease = fmt.Errorf("ambiguous lease owner")
