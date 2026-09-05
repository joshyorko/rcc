package environmentlifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshyorko/rcc/common"
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

func validateGCContentRoot(path string) error {
	home, err := filepath.Abs(common.Product.Home())
	if err != nil {
		return err
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(filepath.Clean(home), filepath.Clean(root))
	if err != nil {
		return fmt.Errorf("prove GC content root is inside consumer home: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refuse GC content root outside consumer home: %s", path)
	}
	if err := validateGCDirectory(home); err != nil {
		return err
	}
	return validateGCDirectory(root)
}
