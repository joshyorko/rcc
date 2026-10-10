package treeartifact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
)

type materializeTestStore struct {
	objects map[Digest][]byte
	reads   map[Digest]int
	sizes   map[Digest]int64
}

func newMaterializeTestStore() *materializeTestStore {
	return &materializeTestStore{objects: make(map[Digest][]byte), reads: make(map[Digest]int), sizes: make(map[Digest]int64)}
}

func (s *materializeTestStore) MissingObjects(_ context.Context, descriptors []Descriptor) ([]Digest, error) {
	var missing []Digest
	for _, d := range descriptors {
		if _, exists := s.objects[d.Digest]; !exists {
			missing = append(missing, d.Digest)
		}
	}
	return missing, nil
}

func (s *materializeTestStore) PutObject(_ context.Context, blob artifactprovider.Blob) error {
	b, err := io.ReadAll(blob.Reader)
	if err != nil {
		return err
	}
	if err := environmentartifact.VerifyDescriptor(blob.Descriptor, b); err != nil {
		return err
	}
	s.objects[blob.Descriptor.Digest] = b
	return nil
}

func (s *materializeTestStore) GetObjectByDigest(_ context.Context, digest Digest) (io.ReadCloser, int64, error) {
	b, exists := s.objects[digest]
	if !exists {
		return nil, 0, fmt.Errorf("missing object %s", digest)
	}
	s.reads[digest]++
	size := int64(len(b))
	if override, exists := s.sizes[digest]; exists {
		size = override
	}
	return io.NopCloser(bytes.NewReader(b)), size, nil
}

func (s *materializeTestStore) file(name, content string, mode uint32) Entry {
	b := []byte(content)
	d := descriptor(FileMediaType, b)
	s.objects[d.Digest] = b
	return Entry{Name: name, Type: TypeFile, Mode: mode, Size: d.Size, Object: &d}
}

func (s *materializeTestStore) tree(t *testing.T, mode uint32, entries ...Entry) Descriptor {
	t.Helper()
	b, d, err := EncodeTree(Tree{Version: Version, Mode: mode, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	s.objects[d.Digest] = b
	return d
}

func (s *materializeTestStore) snapshot(t *testing.T, root Descriptor) Digest {
	t.Helper()
	b, err := json.Marshal(Snapshot{Version: Version, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	d := environmentartifact.DigestBytes(b)
	s.objects[d] = b
	return d
}

func materializeDirectory(name string, mode uint32, d Descriptor) Entry {
	return Entry{Name: name, Type: TypeDirectory, Mode: mode, Object: &d}
}

func materializeSymlink(name, target string) Entry {
	return Entry{Name: name, Type: TypeSymlink, Mode: 0777, Target: target, Size: int64(len(target))}
}

func requireMaterializedFile(t *testing.T, destination, name, content string, mode os.FileMode) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(destination, name))
	if err != nil || string(b) != content {
		t.Fatalf("read %s: %q, %v; want %q", name, b, err, content)
	}
	info, err := os.Stat(filepath.Join(destination, name))
	if err != nil || info.Mode().Perm() != mode {
		t.Fatalf("mode of %s: %v, %v; want %o", name, info, err, mode)
	}
}

func TestMaterializeIncrementalSkipsUnchangedSubtrees(t *testing.T) {
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	leftOld := store.tree(t, 0755, store.file("changed", "old", 0644), store.file("stable", "stable", 0644))
	right := store.tree(t, 0755, store.file("untouched", "untouched", 0644))
	parent := store.snapshot(t, store.tree(t, 0755, materializeDirectory("left", 0755, leftOld), materializeDirectory("right", 0755, right)))
	destination := t.TempDir()
	if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
		t.Fatal(err)
	}
	stableBefore, _ := os.Stat(filepath.Join(destination, "left", "stable"))
	rightBefore, _ := os.Stat(filepath.Join(destination, "right", "untouched"))
	leftNew := store.tree(t, 0755, store.file("changed", "new content", 0755), store.file("stable", "stable", 0644))
	child := store.snapshot(t, store.tree(t, 0755, materializeDirectory("left", 0755, leftNew), materializeDirectory("right", 0755, right)))
	store.reads = make(map[Digest]int)
	stats, err := engine.Materialize(context.Background(), destination, &parent, child)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TreeObjectsRead != 4 || stats.FilesTouched != 1 || stats.BytesWritten != 11 || stats.PathsDeleted != 0 || stats.DirectoriesTouched != 0 || stats.TotalNS <= 0 {
		t.Fatalf("unexpected incremental stats: %+v", stats)
	}
	if store.reads[right.Digest] != 0 {
		t.Fatal("unchanged subtree was loaded")
	}
	if store.reads[environmentartifact.DigestBytes([]byte("stable"))] != 0 || store.reads[environmentartifact.DigestBytes([]byte("untouched"))] != 0 {
		t.Fatal("unchanged file bytes were loaded")
	}
	stableAfter, _ := os.Stat(filepath.Join(destination, "left", "stable"))
	rightAfter, _ := os.Stat(filepath.Join(destination, "right", "untouched"))
	if !os.SameFile(stableBefore, stableAfter) || !os.SameFile(rightBefore, rightAfter) {
		t.Fatal("unchanged files were replaced")
	}
	requireMaterializedFile(t, destination, "left/changed", "new content", 0755)
	store.reads = make(map[Digest]int)
	stats, err = engine.Materialize(context.Background(), destination, &child, child)
	if err != nil || stats.TreeObjectsRead != 0 || stats.FilesTouched != 0 || stats.DirectoriesTouched != 0 || stats.BytesWritten != 0 || stats.PathsDeleted != 0 {
		t.Fatalf("unchanged snapshot: %+v, %v", stats, err)
	}
}

func TestMaterializeTypeTransitions(t *testing.T) {
	for _, oldType := range []string{TypeFile, TypeDirectory, TypeSymlink} {
		for _, newType := range []string{TypeFile, TypeDirectory, TypeSymlink} {
			if oldType == newType {
				continue
			}
			t.Run(oldType+"-to-"+newType, func(t *testing.T) {
				store := newMaterializeTestStore()
				engine := Engine{Store: store}
				entry := func(kind, content string) Entry {
					switch kind {
					case TypeDirectory:
						d := store.tree(t, 0755, store.file("nested", content, 0644))
						return materializeDirectory("item", 0755, d)
					case TypeSymlink:
						return materializeSymlink("item", content)
					default:
						return store.file("item", content, 0644)
					}
				}
				parent := store.snapshot(t, store.tree(t, 0755, entry(oldType, "old")))
				child := store.snapshot(t, store.tree(t, 0755, entry(newType, "new")))
				destination := t.TempDir()
				if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
					t.Fatal(err)
				}
				if _, err := engine.Materialize(context.Background(), destination, &parent, child); err != nil {
					t.Fatal(err)
				}
				info, err := os.Lstat(filepath.Join(destination, "item"))
				if err != nil {
					t.Fatal(err)
				}
				switch newType {
				case TypeDirectory:
					if !info.IsDir() {
						t.Fatal("item is not a directory")
					}
					requireMaterializedFile(t, destination, "item/nested", "new", 0644)
				case TypeFile:
					requireMaterializedFile(t, destination, "item", "new", 0644)
				case TypeSymlink:
					target, err := os.Readlink(filepath.Join(destination, "item"))
					if err != nil || target != "new" {
						t.Fatalf("link: %q, %v", target, err)
					}
				}
			})
		}
	}
}

func TestMaterializeReadOnlyDirectoriesAndDeletion(t *testing.T) {
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	oldBranch := store.tree(t, 0555, store.file("changed", "old", 0444), store.file("deleted", "gone", 0444))
	newBranch := store.tree(t, 0555, store.file("changed", "new", 0444))
	parent := store.snapshot(t, store.tree(t, 0555, materializeDirectory("branch", 0555, oldBranch)))
	child := store.snapshot(t, store.tree(t, 0555, materializeDirectory("branch", 0555, newBranch)))
	destination := t.TempDir()
	defer func() {
		_ = os.Chmod(destination, 0755)
		_ = os.Chmod(filepath.Join(destination, "branch"), 0755)
	}()
	if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
		t.Fatal(err)
	}
	stats, err := engine.Materialize(context.Background(), destination, &parent, child)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesTouched != 1 || stats.PathsDeleted != 1 || stats.DirectoriesTouched != 2 {
		t.Fatalf("unexpected readonly update stats: %+v", stats)
	}
	requireMaterializedFile(t, destination, "branch/changed", "new", 0444)
	if _, err := os.Lstat(filepath.Join(destination, "branch", "deleted")); !os.IsNotExist(err) {
		t.Fatalf("deleted path remains: %v", err)
	}
	for _, name := range []string{destination, filepath.Join(destination, "branch")} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0555 {
			t.Fatalf("readonly directory mode: %v, %v", info, err)
		}
	}
}

func TestMaterializeCorruptionDoesNotOverwriteTarget(t *testing.T) {
	for _, corruption := range []string{"digest", "truncated", "trailing", "reported-size"} {
		t.Run(corruption, func(t *testing.T) {
			store := newMaterializeTestStore()
			engine := Engine{Store: store}
			old := store.file("target", "original", 0644)
			next := store.file("target", "replacement", 0644)
			parent := store.snapshot(t, store.tree(t, 0755, old))
			child := store.snapshot(t, store.tree(t, 0755, next))
			destination := t.TempDir()
			if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
				t.Fatal(err)
			}
			before, _ := os.Stat(filepath.Join(destination, "target"))
			switch corruption {
			case "digest":
				store.objects[next.Object.Digest] = []byte("XXXXXXXXXXX")
			case "truncated":
				store.objects[next.Object.Digest] = []byte("replac")
				store.sizes[next.Object.Digest] = next.Size
			case "trailing":
				store.objects[next.Object.Digest] = []byte("replacement EXTRA")
				store.sizes[next.Object.Digest] = next.Size
			case "reported-size":
				store.sizes[next.Object.Digest] = next.Size + 1
			}
			stats, err := engine.Materialize(context.Background(), destination, &parent, child)
			if err == nil || stats.FilesTouched != 0 {
				t.Fatalf("corruption accepted: %+v, %v", stats, err)
			}
			requireMaterializedFile(t, destination, "target", "original", 0644)
			after, _ := os.Stat(filepath.Join(destination, "target"))
			if !os.SameFile(before, after) {
				t.Fatal("corruption replaced target inode")
			}
			names, err := os.ReadDir(destination)
			if err != nil || len(names) != 1 || names[0].Name() != "target" {
				t.Fatalf("temporary file leaked: %v, %v", names, err)
			}
		})
	}
}

func TestMaterializeRejectsDestinationSymlinks(t *testing.T) {
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	oldBranch := store.tree(t, 0755, store.file("file", "old", 0644))
	newBranch := store.tree(t, 0755, store.file("file", "new", 0644))
	parent := store.snapshot(t, store.tree(t, 0755, materializeDirectory("branch", 0755, oldBranch)))
	child := store.snapshot(t, store.tree(t, 0755, materializeDirectory("branch", 0755, newBranch)))
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(destination, "branch"), filepath.Join(destination, "parked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(destination, "branch")); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Materialize(context.Background(), destination, &parent, child); err == nil {
		t.Fatal("external symlink ancestor accepted")
	}
	requireMaterializedFile(t, outside, "file", "outside", 0644)
	if err := os.Remove(filepath.Join(destination, "branch")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("parked", filepath.Join(destination, "branch")); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Materialize(context.Background(), destination, &parent, child); err == nil {
		t.Fatal("in-root symlink ancestor accepted")
	}
	requireMaterializedFile(t, destination, "parked/file", "old", 0644)
	link := filepath.Join(t.TempDir(), "destination")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Materialize(context.Background(), link, nil, parent); err == nil {
		t.Fatal("symlink destination accepted")
	}
}

func TestMaterializeRejectsUnsafeMetadataAndNonemptyInitialTarget(t *testing.T) {
	for _, unsafe := range []string{"../escape", "/escape", "a/b", "a\\b"} {
		t.Run(unsafe, func(t *testing.T) {
			store := newMaterializeTestStore()
			entry := store.file(unsafe, "unsafe", 0644)
			b, err := json.Marshal(Tree{Version: Version, Mode: 0755, Entries: []Entry{entry}})
			if err != nil {
				t.Fatal(err)
			}
			d := descriptor(TreeMediaType, b)
			store.objects[d.Digest] = b
			engine := Engine{Store: store}
			if _, err := engine.Materialize(context.Background(), t.TempDir(), nil, store.snapshot(t, d)); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	unsafeBytes, err := json.Marshal(Tree{Version: Version, Mode: 0755, Entries: []Entry{materializeSymlink("link", "../escape")}})
	if err != nil {
		t.Fatal(err)
	}
	unsafeTree := descriptor(TreeMediaType, unsafeBytes)
	store.objects[unsafeTree.Digest] = unsafeBytes
	unsafeLink := store.snapshot(t, unsafeTree)
	destination := t.TempDir()
	if _, err := engine.Materialize(context.Background(), destination, nil, unsafeLink); err == nil {
		t.Fatal("escaping symlink accepted")
	}
	if err := os.WriteFile(filepath.Join(destination, "existing"), []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	empty := store.snapshot(t, store.tree(t, 0755))
	if _, err := engine.Materialize(context.Background(), destination, nil, empty); err == nil {
		t.Fatal("nonempty initial destination accepted")
	}
	requireMaterializedFile(t, destination, "existing", "existing", 0644)
}

func TestMaterializeFileModeOnlyDoesNotReadContent(t *testing.T) {
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	old := store.file("file", "content", 0644)
	next := old
	next.Mode = 0755
	parent := store.snapshot(t, store.tree(t, 0755, old))
	child := store.snapshot(t, store.tree(t, 0755, next))
	destination := t.TempDir()
	if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
		t.Fatal(err)
	}
	store.reads = make(map[Digest]int)
	stats, err := engine.Materialize(context.Background(), destination, &parent, child)
	if err != nil || stats.FilesTouched != 1 || stats.BytesWritten != 0 || store.reads[old.Object.Digest] != 0 {
		t.Fatalf("mode-only update: %+v, %v", stats, err)
	}
	requireMaterializedFile(t, destination, "file", "content", 0755)
}

func TestMaterializeRejectsDirectoryModeMismatch(t *testing.T) {
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	branch := store.tree(t, 0755)
	child := store.snapshot(t, store.tree(t, 0755, materializeDirectory("branch", 0555, branch)))
	if _, err := engine.Materialize(context.Background(), t.TempDir(), nil, child); err == nil {
		t.Fatal("directory entry/tree mode mismatch accepted")
	}
}

func TestMaterializeReplacesSymlinkWithoutWritingReferent(t *testing.T) {
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	referent := store.file("referent", "unchanged", 0644)
	parent := store.snapshot(t, store.tree(t, 0755, referent, materializeSymlink("target", "referent")))
	child := store.snapshot(t, store.tree(t, 0755, referent, store.file("target", "replacement", 0644)))
	destination := t.TempDir()
	if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Materialize(context.Background(), destination, &parent, child); err != nil {
		t.Fatal(err)
	}
	requireMaterializedFile(t, destination, "referent", "unchanged", 0644)
	requireMaterializedFile(t, destination, "target", "replacement", 0644)
	info, err := os.Lstat(filepath.Join(destination, "target"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("target was not replaced with regular file: %v, %v", info, err)
	}
}

func TestMaterializeCorruptFilePreservesPreviousDirectory(t *testing.T) {
	store := newMaterializeTestStore()
	engine := Engine{Store: store}
	branch := store.tree(t, 0755, store.file("nested", "original", 0644))
	parent := store.snapshot(t, store.tree(t, 0755, materializeDirectory("target", 0755, branch)))
	next := store.file("target", "replacement", 0644)
	child := store.snapshot(t, store.tree(t, 0755, next))
	destination := t.TempDir()
	if _, err := engine.Materialize(context.Background(), destination, nil, parent); err != nil {
		t.Fatal(err)
	}
	store.objects[next.Object.Digest] = []byte("XXXXXXXXXXX")
	stats, err := engine.Materialize(context.Background(), destination, &parent, child)
	if err == nil || stats.PathsDeleted != 0 {
		t.Fatalf("corruption deleted previous type: %+v, %v", stats, err)
	}
	requireMaterializedFile(t, destination, "target/nested", "original", 0644)
}
