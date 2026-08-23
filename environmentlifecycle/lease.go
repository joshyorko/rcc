package environmentlifecycle

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

type Lease struct {
	ID                string                            `json:"id"`
	MaterializationID string                            `json:"materializationId"`
	ArtifactDigest    environmentartifact.Digest        `json:"artifactDigest"`
	OwnerPID          int                               `json:"ownerPid"`
	OwnerStart        string                            `json:"ownerStart"`
	CreatedAt         time.Time                         `json:"createdAt"`
	Protected         []environmentartifact.Digest      `json:"protected,omitempty"`
	Verification      artifacttrust.VerificationReceipt `json:"verification"`
}

func leasesEqual(left, right Lease) bool {
	return left.ID == right.ID && left.MaterializationID == right.MaterializationID && left.ArtifactDigest == right.ArtifactDigest && left.OwnerPID == right.OwnerPID && left.OwnerStart == right.OwnerStart && left.CreatedAt.Equal(right.CreatedAt) && reflect.DeepEqual(left.Protected, right.Protected) && reflect.DeepEqual(left.Verification, right.Verification)
}

type ProcessIdentityLookup func(int) (string, error)

var processIdentityLookup = lookupProcessIdentity

var lifecycleLocks sync.Map

func artifactLock(digest environmentartifact.Digest) *sync.Mutex {
	lock, _ := lifecycleLocks.LoadOrStore(digest.Hex(), &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func leaseComponents(digest environmentartifact.Digest, id string) []string {
	return []string{digest.Hex(), "leases", id + ".json"}
}

func (it *LocalMaterializer) Lease(ctx context.Context, materialization Materialization) (Lease, error) {
	lock := artifactLock(materialization.ArtifactDigest)
	lock.Lock()
	defer lock.Unlock()
	crossRelease, err := acquireCrossArtifactLock(materialization.ArtifactDigest)
	if err != nil {
		return Lease{}, err
	}
	defer func() { _ = crossRelease() }()
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	if err := crash(CrashBeforeLease); err != nil {
		return Lease{}, err
	}
	record, err := readReadyRecord(materialization.ArtifactDigest)
	if err != nil {
		return Lease{}, fmt.Errorf("lease requires a ready materialization: %w", err)
	}
	if record.MaterializationID != materialization.ID || record.Path != materialization.Path {
		return Lease{}, fmt.Errorf("materialization does not match ready record")
	}
	verification, err := refreshMaterializationTrust(ctx, materialization)
	if err != nil {
		return Lease{}, err
	}
	materialization.Verification = verification
	if materialization.Verification.Code != "" {
		if !materialization.Verification.Valid {
			return Lease{}, fmt.Errorf("materialization trust decision is not valid")
		}
		if materialization.Verification.ArtifactDigest != "" && materialization.Verification.ArtifactDigest != materialization.ArtifactDigest.String() {
			return Lease{}, fmt.Errorf("materialization trust decision is bound to another artifact")
		}
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Lease{}, fmt.Errorf("create lease identity: %w", err)
	}
	lease := Lease{
		ID: hex.EncodeToString(idBytes), MaterializationID: materialization.ID,
		ArtifactDigest: materialization.ArtifactDigest, OwnerPID: os.Getpid(), CreatedAt: time.Now().UTC(),
		Verification: materialization.Verification,
	}
	if root, rootErr := readReferenceRoot(materialization.ArtifactDigest); rootErr == nil {
		lease.Protected = append([]environmentartifact.Digest(nil), root.Protected...)
	} else {
		return Lease{}, fmt.Errorf("lease requires a valid durable reference root: %w", rootErr)
	}
	if lease.Verification.ArtifactDigest == "" {
		lease.Verification.ArtifactDigest = lease.ArtifactDigest.String()
	}
	lease.Verification.LeaseID = lease.ID
	lease.OwnerStart, err = processIdentityLookup(lease.OwnerPID)
	if err != nil || lease.OwnerStart == "" {
		if err == nil {
			err = fmt.Errorf("ambiguous process identity")
		}
		return Lease{}, fmt.Errorf("strong owner identity unavailable: %w", err)
	}
	content, err := json.Marshal(lease)
	if err != nil {
		return Lease{}, err
	}
	descriptor := environmentartifact.Descriptor{MediaType: "application/vnd.rcc.environment.lease.v1+json", Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
	if err := installLegacyImmutable(recordRoot(), leaseComponents(lease.ArtifactDigest, lease.ID), descriptor, content); err != nil {
		return Lease{}, fmt.Errorf("publish lease: %w", err)
	}
	if err := crash(CrashAfterLease); err != nil {
		return Lease{}, err
	}
	if lease.Verification.DecisionID != "" {
		store := artifacttrust.NewReceiptStore(filepath.Join(common.Product.Home(), "artifacts", "v1", "verification"))
		if err := store.Put(lease.Verification); err != nil {
			_ = removeRegularNoFollow(recordRoot(), leaseComponents(lease.ArtifactDigest, lease.ID))
			return Lease{}, fmt.Errorf("persist lease trust receipt: %w", err)
		}
	}
	return lease, nil
}

func readLease(digest environmentartifact.Digest, id string) (Lease, error) {
	content, err := readRegularNoFollow(recordRoot(), leaseComponents(digest, id), maxMaterializationRecordBytes)
	if err != nil {
		return Lease{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var lease Lease
	if err := decoder.Decode(&lease); err != nil {
		return Lease{}, err
	}
	canonical, err := json.Marshal(lease)
	if err != nil || !bytes.Equal(canonical, content) || lease.ArtifactDigest != digest || lease.ID != id {
		return Lease{}, fmt.Errorf("lease is not canonical or has wrong identity")
	}
	return lease, nil
}

func (it *LocalMaterializer) Release(_ context.Context, lease Lease) error {
	if err := crash(CrashBeforeRelease); err != nil {
		return err
	}
	lock := artifactLock(lease.ArtifactDigest)
	lock.Lock()
	defer lock.Unlock()
	crossRelease, err := acquireCrossArtifactLock(lease.ArtifactDigest)
	if err != nil {
		return err
	}
	defer func() { _ = crossRelease() }()
	if lease.ID == "" || len(lease.ArtifactDigest.Hex()) != 64 {
		return fmt.Errorf("invalid lease")
	}
	err = removeRegularNoFollow(recordRoot(), leaseComponents(lease.ArtifactDigest, lease.ID))
	if err == nil {
		err = crash(CrashAfterRelease)
	}
	return err
}
