package environmentlifecycle

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type Lease struct {
	ID                string                     `json:"id"`
	MaterializationID string                     `json:"materializationId"`
	ArtifactDigest    environmentartifact.Digest `json:"artifactDigest"`
	OwnerPID          int                        `json:"ownerPid"`
	OwnerStart        string                     `json:"ownerStart"`
	CreatedAt         time.Time                  `json:"createdAt"`
}

func leaseComponents(digest environmentartifact.Digest, id string) []string {
	return []string{digest.Hex(), "leases", id + ".json"}
}

func (it *LocalMaterializer) Lease(ctx context.Context, materialization Materialization) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	record, err := readReadyRecord(materialization.ArtifactDigest)
	if err != nil {
		return Lease{}, fmt.Errorf("lease requires a ready materialization: %w", err)
	}
	if record.MaterializationID != materialization.ID || record.Path != materialization.Path {
		return Lease{}, fmt.Errorf("materialization does not match ready record")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Lease{}, fmt.Errorf("create lease identity: %w", err)
	}
	lease := Lease{
		ID: hex.EncodeToString(idBytes), MaterializationID: materialization.ID,
		ArtifactDigest: materialization.ArtifactDigest, OwnerPID: os.Getpid(),
		OwnerStart: processStartIdentity(os.Getpid()), CreatedAt: time.Now().UTC(),
	}
	content, err := json.Marshal(lease)
	if err != nil {
		return Lease{}, err
	}
	descriptor := environmentartifact.Descriptor{MediaType: "application/vnd.rcc.environment.lease.v1+json", Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
	if err := installLegacyImmutable(recordRoot(), leaseComponents(lease.ArtifactDigest, lease.ID), descriptor, content); err != nil {
		return Lease{}, fmt.Errorf("publish lease: %w", err)
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
	if lease.ID == "" || len(lease.ArtifactDigest.Hex()) != 64 {
		return fmt.Errorf("invalid lease")
	}
	return removeRegularNoFollow(recordRoot(), leaseComponents(lease.ArtifactDigest, lease.ID))
}

func processStartIdentity(pid int) string {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err == nil {
		if closing := strings.LastIndexByte(string(content), ')'); closing >= 0 {
			fields := strings.Fields(string(content[closing+1:]))
			if len(fields) > 19 {
				return fields[19]
			}
		}
	}
	return strconv.Itoa(pid)
}
