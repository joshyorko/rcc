package treeartifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

type MaterializeStats struct {
	TotalNS            int64 `json:"total_ns"`
	TreeObjectsRead    int64 `json:"tree_objects_read"`
	FilesTouched       int64 `json:"files_touched"`
	DirectoriesTouched int64 `json:"directories_touched"`
	PathsDeleted       int64 `json:"paths_deleted"`
	BytesWritten       int64 `json:"bytes_written"`
}

// Materialize applies only changed branches. With a parent, destination must
// exactly represent that parent and remain quiescent. Without a parent it must
// be an existing empty directory. Files are verified before atomic replacement;
// the whole update is not a transaction. On failure, rebuild the destination
// before using it or attempting another incremental update.
func (e *Engine) Materialize(ctx context.Context, destination string, parent *Digest, child Digest) (stats MaterializeStats, err error) {
	started := time.Now()
	defer func() { stats.TotalNS = time.Since(started).Nanoseconds() }()
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	info, err := os.Lstat(destination)
	if err != nil {
		return stats, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return stats, fmt.Errorf("destination must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return stats, err
	}
	defer root.Close()
	if parent == nil {
		dir, err := root.Open(".")
		if err != nil {
			return stats, err
		}
		names, readErr := dir.Readdirnames(1)
		closeErr := dir.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return stats, readErr
		}
		if closeErr != nil {
			return stats, closeErr
		}
		if len(names) != 0 {
			return stats, fmt.Errorf("initial destination must be empty")
		}
	}
	newSnapshot, err := e.LoadSnapshot(ctx, child)
	if err != nil {
		return stats, fmt.Errorf("load child snapshot: %w", err)
	}
	var oldRoot *Descriptor
	if parent != nil {
		oldSnapshot, err := e.LoadSnapshot(ctx, *parent)
		if err != nil {
			return stats, fmt.Errorf("load parent snapshot: %w", err)
		}
		oldRoot = &oldSnapshot.Root
	}
	m := materialization{engine: e, root: root, stats: &stats, touchedDirs: make(map[string]bool)}
	err = m.directory(ctx, ".", oldRoot, newSnapshot.Root, nil, nil)
	if err != nil {
		err = fmt.Errorf("materialization may be partial; rebuild destination: %w", err)
	}
	return stats, err
}

type materialization struct {
	engine      *Engine
	root        *os.Root
	stats       *MaterializeStats
	touchedDirs map[string]bool
}

func (m *materialization) touchDir(name string) {
	if !m.touchedDirs[name] {
		m.touchedDirs[name] = true
		m.stats.DirectoriesTouched++
	}
}

func (m *materialization) loadTree(ctx context.Context, d Descriptor, mode *uint32) (Tree, error) {
	m.stats.TreeObjectsRead++
	t, err := m.engine.LoadTree(ctx, d)
	if err == nil && mode != nil && t.Mode != *mode {
		err = fmt.Errorf("directory entry mode differs from its tree")
	}
	return t, err
}

// directoryPaths checks only ancestors of changed paths. In particular, Root's
// permitted in-root symlink traversal is not accepted for destination writes.
func (m *materialization) directoryPaths(name string) error {
	if name != "." {
		if err := validateRelativePath(name); err != nil {
			return err
		}
	}
	parts := []string{"."}
	if name != "." {
		current := ""
		for _, part := range strings.Split(name, "/") {
			current = path.Join(current, part)
			parts = append(parts, current)
		}
	}
	for _, part := range parts {
		info, err := m.root.Lstat(part)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("destination ancestor %q is not a real directory", part)
		}
	}
	return nil
}

func (m *materialization) openDirectory(name string) (*os.File, error) {
	if err := m.directoryPaths(name); err != nil {
		return nil, err
	}
	f, err := m.root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		f.Close()
		if err == nil {
			err = fmt.Errorf("%q is not a directory", name)
		}
		return nil, err
	}
	return f, nil
}

func (m *materialization) writableDirectory(name string) (*os.File, os.FileMode, error) {
	f, err := m.openDirectory(name)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	mode := info.Mode().Perm()
	if mode|0700 != mode {
		if err := f.Chmod(mode | 0700); err != nil {
			f.Close()
			return nil, 0, err
		}
		m.touchDir(name)
	}
	return f, mode, nil
}

func sameEntry(a, b Entry) bool {
	if a.Name != b.Name || a.Type != b.Type || a.Mode != b.Mode || a.Size != b.Size || a.Target != b.Target {
		return false
	}
	if a.Object == nil || b.Object == nil {
		return a.Object == nil && b.Object == nil
	}
	return *a.Object == *b.Object
}

func (m *materialization) directory(ctx context.Context, name string, old *Descriptor, next Descriptor, oldMode, nextMode *uint32) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if old != nil && *old == next {
		return nil
	}
	if err := m.directoryPaths(name); err != nil {
		return err
	}
	nextTree, err := m.loadTree(ctx, next, nextMode)
	if err != nil {
		return err
	}
	var oldTree Tree
	if old != nil {
		oldTree, err = m.loadTree(ctx, *old, oldMode)
		if err != nil {
			return err
		}
	}
	for _, entry := range nextTree.Entries {
		entryPath := path.Join(name, entry.Name)
		if err := validateRelativePath(entryPath); err != nil {
			return err
		}
		if entry.Type == TypeSymlink {
			if err := validateLink(entryPath, entry.Target); err != nil {
				return err
			}
		}
	}
	dir, originalMode, err := m.writableDirectory(name)
	if err != nil {
		return err
	}
	// On an error, restore this directory's prior mode, but do not claim that
	// already-applied children have been rolled back.
	defer func() {
		mode := os.FileMode(nextTree.Mode)
		if err != nil {
			mode = originalMode
		}
		if originalMode != mode {
			m.touchDir(name)
		}
		if mode != originalMode|0700 {
			if chmodErr := dir.Chmod(mode); err == nil && chmodErr != nil {
				err = chmodErr
			}
		}
		if syncErr := dir.Sync(); err == nil && syncErr != nil {
			err = syncErr
		}
		if closeErr := dir.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	for i, j := 0, 0; i < len(oldTree.Entries) || j < len(nextTree.Entries); {
		if err := ctx.Err(); err != nil {
			return err
		}
		if j == len(nextTree.Entries) || (i < len(oldTree.Entries) && oldTree.Entries[i].Name < nextTree.Entries[j].Name) {
			entry := oldTree.Entries[i]
			if err := m.remove(ctx, path.Join(name, entry.Name), entry); err != nil {
				return err
			}
			i++
			continue
		}
		entry := nextTree.Entries[j]
		var previous *Entry
		if i < len(oldTree.Entries) && oldTree.Entries[i].Name == entry.Name {
			previous = &oldTree.Entries[i]
			i++
		}
		if previous == nil || !sameEntry(*previous, entry) {
			if err := m.entry(ctx, path.Join(name, entry.Name), previous, entry); err != nil {
				return err
			}
		}
		j++
	}
	return nil
}

func (m *materialization) entry(ctx context.Context, name string, old *Entry, next Entry) error {
	if err := validateRelativePath(name); err != nil {
		return err
	}
	if err := m.directoryPaths(path.Dir(name)); err != nil {
		return err
	}
	if next.Type == TypeFile {
		if old != nil && old.Type == TypeFile && *old.Object == *next.Object {
			return m.fileMode(name, next.Mode)
		}
		// Stage before deleting a previous type, so corrupt bytes cannot erase
		// even a directory or symlink occupying the target.
		temporary, err := m.stageFile(ctx, name, next)
		if err != nil {
			return err
		}
		defer m.root.Remove(temporary)
		if old != nil && old.Type == TypeDirectory {
			if err := m.remove(ctx, name, *old); err != nil {
				return err
			}
		}
		if err := m.root.Rename(temporary, name); err != nil {
			return err
		}
		m.stats.FilesTouched++
		return m.syncDirectory(path.Dir(name))
	}
	if next.Type == TypeDirectory {
		if old != nil && old.Type == TypeDirectory {
			if *old.Object == *next.Object {
				return fmt.Errorf("directory mode changed without changing its tree")
			}
			return m.directory(ctx, name, old.Object, *next.Object, &old.Mode, &next.Mode)
		}
		if old != nil {
			if err := m.remove(ctx, name, *old); err != nil {
				return err
			}
		}
		if err := m.root.Mkdir(name, 0700); err != nil {
			return err
		}
		m.touchDir(name)
		return m.directory(ctx, name, nil, *next.Object, nil, &next.Mode)
	}
	if err := validateLink(name, next.Target); err != nil {
		return err
	}
	if old != nil {
		if err := m.remove(ctx, name, *old); err != nil {
			return err
		}
	}
	if err := m.root.Symlink(next.Target, name); err != nil {
		return err
	}
	// FilesTouched counts regular files and symlinks; directory mutations have
	// their own counter. Unchanged leaves do not increment either counter.
	m.stats.FilesTouched++
	return m.syncDirectory(path.Dir(name))
}

func (m *materialization) fileMode(name string, mode uint32) error {
	info, err := m.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("destination file %q is not regular", name)
	}
	f, err := m.root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(os.FileMode(mode)); err != nil {
		return err
	}
	m.stats.FilesTouched++
	return f.Sync()
}

func (m *materialization) syncDirectory(name string) error {
	f, err := m.openDirectory(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (m *materialization) remove(ctx context.Context, name string, entry Entry) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRelativePath(name); err != nil {
		return err
	}
	if err := m.directoryPaths(path.Dir(name)); err != nil {
		return err
	}
	if entry.Type == TypeDirectory {
		dir, originalMode, openErr := m.writableDirectory(name)
		if openErr != nil {
			return openErr
		}
		defer func() {
			if err != nil {
				_ = dir.Chmod(originalMode)
			}
			dir.Close()
		}()
		tree, err := m.loadTree(ctx, *entry.Object, &entry.Mode)
		if err != nil {
			return err
		}
		for _, child := range tree.Entries {
			if err := m.remove(ctx, path.Join(name, child.Name), child); err != nil {
				return err
			}
		}
	}
	if err := m.root.Remove(name); err != nil {
		return err
	}
	m.stats.PathsDeleted++
	return nil
}

func (m *materialization) stageFile(ctx context.Context, name string, entry Entry) (temporary string, err error) {
	d := *entry.Object
	if err := validateDescriptor(d, FileMediaType); err != nil {
		return "", err
	}
	r, size, err := m.engine.Store.GetObjectByDigest(ctx, d.Digest)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if size != d.Size {
		return "", fmt.Errorf("file size mismatch for %q", name)
	}
	var file *os.File
	for attempt := 0; attempt < 16; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		temporary = path.Join(path.Dir(name), ".rcc-tree-"+hex.EncodeToString(random[:]))
		file, err = m.root.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if !errors.Is(err, os.ErrExist) {
			break
		}
	}
	if err != nil {
		return "", err
	}
	defer func() {
		file.Close()
		if err != nil {
			_ = m.root.Remove(temporary)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), &contextReader{ctx: ctx, reader: io.LimitReader(r, d.Size)})
	m.stats.BytesWritten += n
	if err != nil {
		return temporary, err
	}
	var extra [1]byte
	extraN, extraErr := io.ReadFull(&contextReader{ctx: ctx, reader: r}, extra[:])
	if extraErr != nil && !errors.Is(extraErr, io.EOF) {
		return temporary, extraErr
	}
	if n != d.Size || extraN != 0 || hex.EncodeToString(hash.Sum(nil)) != d.Digest.Hex() {
		return temporary, fmt.Errorf("file descriptor verification failed for %q", name)
	}
	if err := ctx.Err(); err != nil {
		return temporary, err
	}
	if err := file.Chmod(os.FileMode(entry.Mode)); err != nil {
		return temporary, err
	}
	if err := file.Sync(); err != nil {
		return temporary, err
	}
	if err := file.Close(); err != nil {
		return temporary, err
	}
	return temporary, nil
}
