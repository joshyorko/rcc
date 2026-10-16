package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTreeArtifactRejectsAliasedStoreWorkspaceOverlapBeforeCreation(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	source := filepath.Join(real, "source")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(source, "new", "cas")
	if _, err := treeArtifactEngine(store, filepath.Join(alias, "source")); err == nil {
		t.Fatal("accepted store inside aliased source")
	}
	if _, err := os.Stat(filepath.Join(source, "new")); !os.IsNotExist(err) {
		t.Fatal("created provider before rejecting overlap", err)
	}
	if _, err := treeArtifactEngine(real, filepath.Join(alias, "source")); err == nil {
		t.Fatal("accepted source inside aliased store")
	}
	if _, err := treeArtifactEngine(filepath.Join(root, "separate", "cas"), source); err != nil {
		t.Fatal(err)
	}
}
