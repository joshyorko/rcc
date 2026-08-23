package environmentlifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/environmentartifact"
)

type durableReferenceRoot struct {
	Manifest  environmentartifact.Digest   `json:"manifest"`
	Protected []environmentartifact.Digest `json:"protected"`
}

func writeReferenceRoot(manifest environmentartifact.Manifest, index environmentartifact.ObjectIndex) error {
	graph := BuildReferenceGraph(manifest, index)
	root := durableReferenceRoot{Manifest: graph.Manifest, Protected: graph.Protected}
	content, err := json.Marshal(root)
	if err != nil {
		return err
	}
	return writeAtomicMutable(recordRoot(), []string{manifest.ArtifactDigest.Hex(), "references.json"}, content)
}

func readReferenceRoot(digest environmentartifact.Digest) (durableReferenceRoot, error) {
	content, err := readRegularNoFollow(recordRoot(), []string{digest.Hex(), "references.json"}, maxMaterializationRecordBytes)
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
	return root, nil
}

func referenceRootExists(digest environmentartifact.Digest) bool {
	_, err := os.Stat(filepath.Join(recordRoot(), digest.Hex(), "references.json"))
	return err == nil
}
