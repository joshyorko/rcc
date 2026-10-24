package treeartifact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
)

func captureFixture(t *testing.T) (*Engine, string) {
	t.Helper()
	base := t.TempDir()
	source := filepath.Join(base, "source")
	for _, dir := range []string{"a", "b/sub"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"a/one": "first", "a/two": "second", "b/sub/three": "untouched"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := artifactprovider.NewFilesystem(filepath.Join(base, "cas"))
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Store: store}, source
}

func mustCapture(t *testing.T, e *Engine, source string, parent *Digest, dirty ...string) CaptureResult {
	t.Helper()
	result, err := e.Capture(context.Background(), source, parent, dirty)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func findEntry(t *testing.T, tree Tree, name string) Entry {
	t.Helper()
	for _, entry := range tree.Entries {
		if entry.Name == name {
			return entry
		}
	}
	t.Fatalf("missing entry %s", name)
	return Entry{}
}

func TestIncrementalOnlyReadsDirtyFileAndAncestorTrees(t *testing.T) {
	e, source := captureFixture(t)
	seed := mustCapture(t, e, source, nil)
	old, err := e.LoadTree(context.Background(), seed.Root)
	if err != nil {
		t.Fatal(err)
	}
	// Removing an unreported branch proves capture neither enumerates nor stats
	// it. This deliberately violates the complete-dirty-set caller contract.
	if err := os.RemoveAll(filepath.Join(source, "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a/one"), []byte("edited"), 0644); err != nil {
		t.Fatal(err)
	}
	child := mustCapture(t, e, source, &seed.Snapshot, "a/one")
	if child.Root == seed.Root {
		t.Fatal("root identity did not change")
	}
	if s := child.Stats; s.FilesHashed != 1 || s.FileBytesHashed != 6 || s.SourceBytesRead != 12 || s.DirectoriesScanned != 0 || s.TreeNodesRebuilt != 2 || s.TreeObjectsRead != 2 || s.ObjectsWritten != 4 {
		t.Fatalf("unexpected incremental work: %+v", s)
	}
	current, err := e.LoadTree(context.Background(), child.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !sameEntry(findEntry(t, old, "b"), findEntry(t, current, "b")) {
		t.Fatal("untouched subtree changed")
	}
	snapshot, err := e.LoadSnapshot(context.Background(), child.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Parent == nil || *snapshot.Parent != seed.Snapshot {
		t.Fatal("snapshot lost parent identity")
	}
}

func TestIncrementalDeleteRenameAndNoop(t *testing.T) {
	e, source := captureFixture(t)
	seed := mustCapture(t, e, source, nil)
	if err := os.Rename(filepath.Join(source, "a/one"), filepath.Join(source, "a/renamed")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "a/two")); err != nil {
		t.Fatal(err)
	}
	child := mustCapture(t, e, source, &seed.Snapshot, "a/one", "a/renamed", "a/two")
	if s := child.Stats; s.FilesHashed != 1 || s.DirectoriesScanned != 0 || s.TreeNodesRebuilt != 2 || s.ObjectsWritten != 3 {
		t.Fatalf("rename should reuse file CAS object: %+v", s)
	}
	root, err := e.LoadTree(context.Background(), child.Root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.LoadTree(context.Background(), *findEntry(t, root, "a").Object)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Entries) != 1 || a.Entries[0].Name != "renamed" {
		t.Fatalf("wrong rename/delete result: %+v", a)
	}
	noop := mustCapture(t, e, source, &child.Snapshot)
	if noop.Root != child.Root || noop.Stats.FilesHashed != 0 || noop.Stats.TreeNodesRebuilt != 0 || noop.Stats.DirectoriesScanned != 0 {
		t.Fatalf("no-op changed tree/work: %+v", noop)
	}
	again := mustCapture(t, e, source, &child.Snapshot)
	if again.Snapshot != noop.Snapshot || again.Stats.ObjectsWritten != 0 {
		t.Fatalf("immutable commit is not idempotent: %+v", again)
	}
}

func TestIncrementalNewAncestorsWithoutEnumeration(t *testing.T) {
	e, source := captureFixture(t)
	seed := mustCapture(t, e, source, nil)
	if err := os.MkdirAll(filepath.Join(source, "new/nested"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "new/nested/file"), []byte("new file"), 0600); err != nil {
		t.Fatal(err)
	}
	child := mustCapture(t, e, source, &seed.Snapshot, "new/nested/file")
	if child.Stats.DirectoriesScanned != 0 || child.Stats.FilesHashed != 1 || child.Stats.TreeNodesRebuilt != 3 {
		t.Fatalf("new ancestor work: %+v", child.Stats)
	}
}

func TestCaptureRejectsUnsafePathsAndLinks(t *testing.T) {
	e, source := captureFixture(t)
	seed := mustCapture(t, e, source, nil)
	for _, dirty := range []string{"../outside", "/outside", "a/../one", "a//one", "a\\one", "C:drive", "."} {
		t.Run(dirty, func(t *testing.T) {
			if _, err := e.Capture(context.Background(), source, &seed.Snapshot, []string{dirty}); err == nil {
				t.Fatal("accepted unsafe dirty path")
			}
		})
	}
	if err := os.Symlink("a", filepath.Join(source, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Capture(context.Background(), source, &seed.Snapshot, []string{"alias/one"}); err == nil {
		t.Fatal("traversed symlink dirty ancestor")
	}
	if err := os.Symlink("../../outside", filepath.Join(source, "a/escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Capture(context.Background(), source, &seed.Snapshot, []string{"a/escape"}); err == nil {
		t.Fatal("accepted escaping symlink")
	}
}

type negotiationStore struct {
	Store
	negotiated map[Digest]bool
	calls      int
}

func (s *negotiationStore) MissingObjects(ctx context.Context, ds []Descriptor) ([]Digest, error) {
	if len(ds) > 128 {
		panic("unbounded negotiation")
	}
	s.calls++
	for _, d := range ds {
		s.negotiated[d.Digest] = true
	}
	return s.Store.MissingObjects(ctx, ds)
}
func (s *negotiationStore) PutObject(ctx context.Context, blob artifactprovider.Blob) error {
	if !s.negotiated[blob.Descriptor.Digest] {
		panic("write before negotiation")
	}
	return s.Store.PutObject(ctx, blob)
}

func TestCaptureNegotiatesBeforeEveryWrite(t *testing.T) {
	e, source := captureFixture(t)
	store := &negotiationStore{Store: e.Store, negotiated: make(map[Digest]bool)}
	e.Store = store
	seed := mustCapture(t, e, source, nil)
	if store.calls < 2 {
		t.Fatal("snapshot must commit after object publication")
	}
	store.negotiated = make(map[Digest]bool)
	if err := os.WriteFile(filepath.Join(source, "a/one"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	mustCapture(t, e, source, &seed.Snapshot, "a/one")
}

func TestCaptureRejectsLeafBeyondMaterializerDepthBound(t *testing.T) {
	e, source := captureFixture(t)
	dir := strings.Repeat("d/", maxDepth)
	if err := os.MkdirAll(filepath.Join(source, dir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, dir, "file"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Capture(context.Background(), source, nil, nil); err == nil {
		t.Fatal("published an unmaterializable leaf depth")
	}
}
