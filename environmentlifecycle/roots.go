package environmentlifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type durableReferenceRoot struct {
	Manifest  environmentartifact.Digest   `json:"manifest"`
	Protected []environmentartifact.Digest `json:"protected"`
	State     string                       `json:"state"`
	RetiredAt time.Time                    `json:"retiredAt,omitempty"`
}

func writeReferenceRoot(manifest environmentartifact.Manifest, index environmentartifact.ObjectIndex) error {
	graph := BuildReferenceGraph(manifest, index)
	root := durableReferenceRoot{Manifest: graph.Manifest, Protected: graph.Protected, State: "live"}
	content, err := json.Marshal(root)
	if err != nil {
		return err
	}
	if len(content) > maxProtectionRecordBytes {
		return fmt.Errorf("reference root exceeds bounded protection record size")
	}
	return writeAtomicMutable(recordRoot(), []string{manifest.ArtifactDigest.Hex(), "references.json"}, content)
}

func readReferenceRoot(digest environmentartifact.Digest) (durableReferenceRoot, error) {
	content, err := readRegularNoFollow(recordRoot(), []string{digest.Hex(), "references.json"}, maxProtectionRecordBytes)
	if err != nil {
		return durableReferenceRoot{}, err
	}
	var root durableReferenceRoot
	if err := json.Unmarshal(content, &root); err != nil {
		return durableReferenceRoot{}, fmt.Errorf("decode reference root: %w", err)
	}
	if root.Manifest != digest {
		return durableReferenceRoot{}, fmt.Errorf("reference root identity mismatch")
	}
	if root.State == "" {
		root.State = "live"
	}
	if root.State != "live" && root.State != "retired" {
		return durableReferenceRoot{}, fmt.Errorf("invalid reference root state")
	}
	return root, nil
}

func retireReferenceRoot(digest environmentartifact.Digest, at time.Time) error {
	root, err := readReferenceRoot(digest)
	if err != nil {
		return err
	}
	if root.State == "retired" {
		return nil
	}
	root.State, root.RetiredAt = "retired", at.UTC()
	content, err := json.Marshal(root)
	if err != nil {
		return err
	}
	if len(content) > maxProtectionRecordBytes {
		return fmt.Errorf("reference root exceeds bounded protection record size")
	}
	return writeAtomicMutable(recordRoot(), []string{digest.Hex(), "references.json"}, content)
}

func referenceRootExists(digest environmentartifact.Digest) bool {
	if err := validateGCDirectory(recordRoot()); err != nil {
		return false
	}
	if err := validateGCDirectory(filepath.Join(recordRoot(), digest.Hex())); err != nil {
		return false
	}
	info, err := os.Lstat(filepath.Join(recordRoot(), digest.Hex(), "references.json"))
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}
