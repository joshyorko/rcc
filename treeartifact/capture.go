package treeartifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
)

// FileBytesHashed counts source content only; CAS verification hashes are part
// of WriteObjectsNS. SourceBytesRead includes the second read for missing files.
// MetadataBytesHashed includes newly encoded directories and the snapshot.
type CaptureStats struct {
	TotalNS             int64 `json:"total_ns"`
	FilesHashed         int64 `json:"files_hashed"`
	FileBytesHashed     int64 `json:"file_bytes_hashed"`
	SourceBytesRead     int64 `json:"source_bytes_read"`
	MetadataBytesHashed int64 `json:"metadata_bytes_hashed"`
	PathsStatted        int64 `json:"paths_statted"`
	DirectoriesScanned  int64 `json:"directories_scanned"`
	TreeObjectsRead     int64 `json:"tree_objects_read"`
	TreeNodesRebuilt    int64 `json:"tree_nodes_rebuilt"`
	MissingObjectsNS    int64 `json:"missing_objects_ns"`
	WriteObjectsNS      int64 `json:"write_objects_ns"`
	ObjectsWritten      int64 `json:"objects_written"`
	BytesWritten        int64 `json:"bytes_written"`
	RootCommitNS        int64 `json:"root_commit_ns"`
}

type CaptureResult struct {
	Snapshot Digest       `json:"snapshot"`
	Root     Descriptor   `json:"root"`
	Stats    CaptureStats `json:"stats"`
}

type dirtyNode struct {
	terminal bool
	children map[string]*dirtyNode
}

func dirtyTrie(paths []string) (*dirtyNode, error) {
	root := &dirtyNode{children: make(map[string]*dirtyNode)}
	for _, p := range paths {
		if err := validateRelativePath(p); err != nil {
			return nil, err
		}
		node := root
		for _, name := range strings.Split(p, "/") {
			if node.children[name] == nil {
				node.children[name] = &dirtyNode{children: make(map[string]*dirtyNode)}
			}
			node = node.children[name]
		}
		node.terminal = true
	}
	return root, nil
}

type pendingObject struct {
	descriptor Descriptor
	data       []byte
	source     string
	info       os.FileInfo
}
type capture struct {
	engine  *Engine
	ctx     context.Context
	source  *os.Root
	stats   CaptureStats
	pending []pendingObject
}

// Capture seeds by scanning the source when previous is nil. Otherwise dirty
// is a TRUSTED complete explicit set: deletions are absent paths; a rename is
// both old and new paths. Files absent from dirty are never statted or hashed.
// Dirty directory paths explicitly request a scan of that subtree (e.g. a new
// directory). Ancestors of dirty files are loaded from CAS, never enumerated.
// Incomplete dirty sets intentionally produce incomplete snapshots; this API
// does not infer changes or provide a watcher. The source must be quiescent.
func (e *Engine) Capture(ctx context.Context, source string, previous *Digest, dirty []string) (result CaptureResult, err error) {
	started := time.Now()
	c := &capture{engine: e, ctx: ctx}
	defer func() { c.stats.TotalNS = time.Since(started).Nanoseconds(); result.Stats = c.stats }()
	if e.Store == nil {
		return result, fmt.Errorf("store is required")
	}
	trie, err := dirtyTrie(dirty)
	if err != nil {
		return result, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return result, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, fmt.Errorf("source must be a real directory")
	}
	c.source, err = os.OpenRoot(source)
	if err != nil {
		return result, err
	}
	defer c.source.Close()
	var root Descriptor
	if previous == nil {
		if len(dirty) != 0 {
			return result, fmt.Errorf("initial capture cannot use a dirty set")
		}
		root, err = c.scanDirectory(".", 0)
	} else {
		prior, loadErr := e.LoadSnapshot(ctx, *previous)
		if loadErr != nil {
			return result, loadErr
		}
		root, err = c.updateDirectory(".", &prior.Root, trie, 0)
	}
	if err != nil {
		return result, err
	}
	if err := c.flush(); err != nil {
		return result, err
	}
	commitStart := time.Now()
	snapshot := Snapshot{Version: Version, Root: root, Parent: previous}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return result, err
	}
	c.stats.MetadataBytesHashed += int64(len(b))
	d := descriptor(SnapshotMediaType, b)
	c.pending = append(c.pending, pendingObject{descriptor: d, data: b})
	if err := c.flush(); err != nil {
		return result, err
	}
	c.stats.RootCommitNS = time.Since(commitStart).Nanoseconds()
	result.Snapshot, result.Root = d.Digest, root
	return result, nil
}

func (c *capture) lstat(p string) (os.FileInfo, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	c.stats.PathsStatted++
	info, err := c.source.Lstat(p)
	if err == nil && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, fmt.Errorf("special permission bits unsupported: %q", p)
	}
	return info, err
}

func (c *capture) scanDirectory(p string, depth int) (Descriptor, error) {
	if depth > maxDepth {
		return Descriptor{}, fmt.Errorf("tree depth exceeds bound")
	}
	info, err := c.lstat(p)
	if err != nil {
		return Descriptor{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Descriptor{}, fmt.Errorf("unsafe directory %q", p)
	}
	f, err := c.source.Open(p)
	if err != nil {
		return Descriptor{}, err
	}
	entries, readErr := f.ReadDir(-1)
	closeErr := f.Close()
	if readErr != nil {
		return Descriptor{}, readErr
	}
	if closeErr != nil {
		return Descriptor{}, closeErr
	}
	c.stats.DirectoriesScanned++
	if len(entries) > maxTreeEntries {
		return Descriptor{}, fmt.Errorf("directory fanout exceeds bound")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	t := Tree{Version: Version, Mode: uint32(info.Mode().Perm()), Entries: []Entry{}}
	for _, item := range entries {
		if err := validateName(item.Name()); err != nil {
			return Descriptor{}, err
		}
		entry, err := c.captureEntry(path.Join(p, item.Name()), item.Name(), depth+1)
		if err != nil {
			return Descriptor{}, err
		}
		t.Entries = append(t.Entries, entry)
	}
	return c.putTree(t, nil)
}

func (c *capture) captureEntry(p, name string, depth int) (Entry, error) {
	if depth > maxDepth {
		return Entry{}, fmt.Errorf("tree depth exceeds bound")
	}
	info, err := c.lstat(p)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{Name: name, Mode: uint32(info.Mode().Perm())}
	switch {
	case info.Mode().IsRegular():
		f, err := c.source.Open(p)
		if err != nil {
			return entry, err
		}
		opened, err := f.Stat()
		if err != nil {
			f.Close()
			return entry, err
		}
		if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			f.Close()
			return entry, fmt.Errorf("source changed while opening %q", p)
		}
		h := sha256.New()
		n, readErr := io.Copy(h, &contextReader{ctx: c.ctx, reader: f})
		after, statErr := f.Stat()
		closeErr := f.Close()
		c.stats.FilesHashed++
		c.stats.FileBytesHashed += n
		c.stats.SourceBytesRead += n
		if readErr != nil {
			return entry, readErr
		}
		if statErr != nil {
			return entry, statErr
		}
		if closeErr != nil {
			return entry, closeErr
		}
		if n != info.Size() || !stableFile(info, after) {
			return entry, fmt.Errorf("source changed while hashing %q", p)
		}
		digest, err := environmentartifact.ParseDigest("sha256:" + hex.EncodeToString(h.Sum(nil)))
		if err != nil {
			return entry, err
		}
		d := Descriptor{MediaType: FileMediaType, Digest: digest, Size: n}
		entry.Type, entry.Size, entry.Object = TypeFile, n, &d
		if err := c.stage(pendingObject{descriptor: d, source: p, info: after}); err != nil {
			return entry, err
		}
	case info.IsDir():
		d, err := c.scanDirectory(p, depth)
		if err != nil {
			return entry, err
		}
		entry.Type, entry.Object = TypeDirectory, &d
	case info.Mode()&os.ModeSymlink != 0:
		target, err := c.source.Readlink(p)
		if err != nil {
			return entry, err
		}
		if err := validateLink(p, target); err != nil {
			return entry, err
		}
		entry.Type, entry.Mode, entry.Size, entry.Target = TypeSymlink, 0777, int64(len(target)), target
	default:
		return entry, fmt.Errorf("special files unsupported: %q", p)
	}
	return entry, nil
}

func stableFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime() == b.ModTime()
}

func (c *capture) updateDirectory(p string, old *Descriptor, dirty *dirtyNode, depth int) (Descriptor, error) {
	if depth > maxDepth {
		return Descriptor{}, fmt.Errorf("tree depth exceeds bound")
	}
	info, err := c.lstat(p)
	if err != nil {
		return Descriptor{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Descriptor{}, fmt.Errorf("dirty ancestor is not a real directory: %q", p)
	}
	t := Tree{Version: Version, Entries: []Entry{}}
	if old != nil {
		t, err = c.engine.LoadTree(c.ctx, *old)
		c.stats.TreeObjectsRead++
		if err != nil {
			return Descriptor{}, err
		}
	}
	t.Mode = uint32(info.Mode().Perm())
	entries := make(map[string]Entry, len(t.Entries))
	for _, entry := range t.Entries {
		entries[entry.Name] = entry
	}
	names := make([]string, 0, len(dirty.children))
	for name := range dirty.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		node := dirty.children[name]
		child := path.Join(p, name)
		childInfo, err := c.lstat(child)
		if os.IsNotExist(err) {
			delete(entries, name)
			continue
		}
		if err != nil {
			return Descriptor{}, err
		}
		if node.terminal {
			entry, err := c.captureEntry(child, name, depth+1)
			if err != nil {
				return Descriptor{}, err
			}
			entries[name] = entry
		} else {
			if !childInfo.IsDir() || childInfo.Mode()&os.ModeSymlink != 0 {
				return Descriptor{}, fmt.Errorf("dirty ancestor is not a directory: %q", child)
			}
			var previous *Descriptor
			if entry, ok := entries[name]; ok && entry.Type == TypeDirectory {
				previous = entry.Object
			}
			d, err := c.updateDirectory(child, previous, node, depth+1)
			if err != nil {
				return Descriptor{}, err
			}
			entries[name] = Entry{Name: name, Type: TypeDirectory, Mode: uint32(childInfo.Mode().Perm()), Object: &d}
		}
	}
	t.Entries = make([]Entry, 0, len(entries))
	for _, entry := range entries {
		t.Entries = append(t.Entries, entry)
	}
	return c.putTree(t, old)
}

func (c *capture) putTree(t Tree, old *Descriptor) (Descriptor, error) {
	b, d, err := EncodeTree(t)
	if err != nil {
		return d, err
	}
	c.stats.MetadataBytesHashed += int64(len(b))
	if old != nil && *old == d {
		return d, nil
	}
	c.stats.TreeNodesRebuilt++
	return d, c.stage(pendingObject{descriptor: d, data: b})
}

func (c *capture) stage(object pendingObject) error {
	c.pending = append(c.pending, object)
	// A bounded transfer batch, never a persisted flat workspace index.
	if len(c.pending) >= 128 {
		return c.flush()
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
	count  *int64
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(b)
	if r.count != nil {
		*r.count += int64(n)
	}
	return n, err
}

func (c *capture) flush() error {
	if len(c.pending) == 0 {
		return nil
	}
	descriptors := make([]Descriptor, 0, len(c.pending))
	seen := make(map[Digest]bool)
	for _, object := range c.pending {
		if !seen[object.descriptor.Digest] {
			descriptors = append(descriptors, object.descriptor)
			seen[object.descriptor.Digest] = true
		}
	}
	started := time.Now()
	missing, err := c.engine.Store.MissingObjects(c.ctx, descriptors)
	c.stats.MissingObjectsNS += time.Since(started).Nanoseconds()
	if err != nil {
		return err
	}
	needed := make(map[Digest]bool, len(missing))
	for _, digest := range missing {
		if !seen[digest] {
			return fmt.Errorf("provider returned unsolicited missing digest")
		}
		needed[digest] = true
	}
	for _, object := range c.pending {
		if !needed[object.descriptor.Digest] {
			continue
		}
		var reader io.Reader = bytes.NewReader(object.data)
		var file *os.File
		if object.source != "" {
			file, err = c.source.Open(object.source)
			if err != nil {
				return err
			}
			info, statErr := file.Stat()
			if statErr != nil {
				file.Close()
				return statErr
			}
			if !stableFile(object.info, info) {
				file.Close()
				return fmt.Errorf("source changed before CAS write: %q", object.source)
			}
			reader = &contextReader{ctx: c.ctx, reader: file, count: &c.stats.SourceBytesRead}
		}
		started = time.Now()
		err = c.engine.Store.PutObject(c.ctx, artifactprovider.Blob{Descriptor: object.descriptor, Reader: reader})
		c.stats.WriteObjectsNS += time.Since(started).Nanoseconds()
		if file != nil {
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			return err
		}
		c.stats.ObjectsWritten++
		c.stats.BytesWritten += object.descriptor.Size
		delete(needed, object.descriptor.Digest)
	}
	c.pending = nil
	return nil
}
