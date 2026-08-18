package environmentlifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

const maxMaterializationRecordBytes = 64 << 10

type materializationState string

const (
	stateVerifiedContent materializationState = "verified-content"
	stateMaterializing   materializationState = "materializing"
	stateReady           materializationState = "ready"
)

type materializationRecord struct {
	ArtifactDigest     environmentartifact.Digest `json:"artifactDigest"`
	LegacyBlueprintKey string                     `json:"legacyBlueprintKey"`
	MaterializationID  string                     `json:"materializationId"`
	Path               string                     `json:"path"`
	State              materializationState       `json:"state"`
	CreatedAt          time.Time                  `json:"createdAt"`
	VerifiedAt         time.Time                  `json:"verifiedAt"`
}

func recordRoot() string {
	return filepath.Join(common.Product.Home(), "artifacts", "v1", "materializations")
}

func recordComponents(digest environmentartifact.Digest, state materializationState) []string {
	return []string{digest.Hex(), string(state) + ".json"}
}

func writeMaterializationRecord(record materializationRecord) error {
	content, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode materialization record: %w", err)
	}
	return writeAtomicMutable(recordRoot(), recordComponents(record.ArtifactDigest, record.State), content)
}

func readReadyRecord(digest environmentartifact.Digest) (materializationRecord, error) {
	content, err := readRegularNoFollow(recordRoot(), recordComponents(digest, stateReady), maxMaterializationRecordBytes)
	if err != nil {
		return materializationRecord{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var record materializationRecord
	if err := decoder.Decode(&record); err != nil {
		return materializationRecord{}, fmt.Errorf("decode ready materialization record: %w", err)
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(canonical, content) {
		return materializationRecord{}, fmt.Errorf("ready materialization record is not canonical")
	}
	if record.ArtifactDigest != digest || record.State != stateReady {
		return materializationRecord{}, fmt.Errorf("ready materialization record identity mismatch")
	}
	return record, nil
}
