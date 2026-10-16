// Package treeartifact is an EXPERIMENTAL generic workspace artifact engine.
// It deliberately does not use the Environment Artifact v1 catalog, manifest,
// archive, or GC contracts. Encryption-domain wrapping is required before Room
// product integration. Sources and existing materializations must be quiescent.
package treeartifact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
)

type Digest = environmentartifact.Digest
type Descriptor = environmentartifact.Descriptor

const (
	Version           = 1
	TypeFile          = "file"
	TypeDirectory     = "directory"
	TypeSymlink       = "symlink"
	FileMediaType     = "application/vnd.rcc.experimental.tree.file.v1"
	TreeMediaType     = "application/vnd.rcc.experimental.tree.directory.v1+json"
	SnapshotMediaType = "application/vnd.rcc.experimental.tree.snapshot.v1+json"
	maxTreeBytes      = 16 << 20
	maxTreeEntries    = 65536
	maxDepth          = 256
)

// Store is the existing RCC generic object seam. A dedicated provider root is
// mandatory: v1 provider GC cannot trace experimental tree/snapshot objects.
// PutObject must verify bytes and publish atomically without replacement.
type Store interface {
	MissingObjects(context.Context, []Descriptor) ([]Digest, error)
	PutObject(context.Context, artifactprovider.Blob) error
	GetObjectByDigest(context.Context, Digest) (io.ReadCloser, int64, error)
}

type Engine struct{ Store Store }

type Entry struct {
	Name   string      `json:"name"`
	Type   string      `json:"type"`
	Mode   uint32      `json:"mode"`
	Size   int64       `json:"size"`
	Object *Descriptor `json:"object,omitempty"`
	Target string      `json:"target,omitempty"`
}

type Tree struct {
	Version int     `json:"version"`
	Mode    uint32  `json:"mode"`
	Entries []Entry `json:"entries"`
}

// Root is the content identity. Parent is the previous snapshot identity; it
// does not influence any directory identity. No timestamps or mutable heads.
type Snapshot struct {
	Version int        `json:"version"`
	Root    Descriptor `json:"root"`
	Parent  *Digest    `json:"parent,omitempty"`
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\:\x00") {
		return fmt.Errorf("unsafe path segment %q", name)
	}
	return nil
}

func validateRelativePath(p string) error {
	if p == "" || path.Clean(p) != p || strings.HasPrefix(p, "/") {
		return fmt.Errorf("unsafe relative path %q", p)
	}
	parts := strings.Split(p, "/")
	if len(parts) > maxDepth {
		return fmt.Errorf("path depth exceeds bound")
	}
	for _, part := range parts {
		if err := validateName(part); err != nil {
			return err
		}
	}
	return nil
}

func validateLink(p, target string) error {
	if target == "" || !utf8.ValidString(target) || len(target) > 4096 || strings.ContainsAny(target, "\\:\x00") || path.IsAbs(target) {
		return fmt.Errorf("unsafe symlink target %q", target)
	}
	// Prototype links may refer to siblings/descendants, but not parents. A
	// lexical path.Clean check alone is insufficient when a preceding component
	// is another symlink: link/../outside can escape despite lexical confinement.
	for _, component := range strings.Split(target, "/") {
		if component == ".." {
			return fmt.Errorf("parent-traversing symlinks unsupported: %q", target)
		}
	}
	resolved := path.Join(path.Dir(p), target)
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("symlink %q escapes workspace", p)
	}
	return nil
}

func validateDescriptor(d Descriptor, media string) error {
	if d.MediaType != media || len(d.Digest.Hex()) != 64 || d.Size < 0 {
		return fmt.Errorf("invalid %s descriptor", media)
	}
	if media != FileMediaType && d.Size > maxTreeBytes {
		return fmt.Errorf("metadata object exceeds bound")
	}
	return nil
}

func (t Tree) Validate() error {
	if t.Version != Version || t.Mode > 0777 || t.Entries == nil || len(t.Entries) > maxTreeEntries {
		return fmt.Errorf("invalid tree header or entry count")
	}
	previous := ""
	for _, entry := range t.Entries {
		if err := validateName(entry.Name); err != nil {
			return err
		}
		if entry.Name <= previous {
			return fmt.Errorf("tree entries must be uniquely sorted")
		}
		previous = entry.Name
		if entry.Mode > 0777 || entry.Size < 0 {
			return fmt.Errorf("invalid entry metadata")
		}
		switch entry.Type {
		case TypeFile, TypeDirectory:
			media := FileMediaType
			if entry.Type == TypeDirectory {
				media = TreeMediaType
				if entry.Size != 0 {
					return fmt.Errorf("directory size must be zero")
				}
			}
			if entry.Object == nil || entry.Target != "" {
				return fmt.Errorf("invalid object entry")
			}
			if err := validateDescriptor(*entry.Object, media); err != nil {
				return err
			}
			if entry.Type == TypeFile && entry.Size != entry.Object.Size {
				return fmt.Errorf("file size mismatch")
			}
		case TypeSymlink:
			if entry.Object != nil || entry.Mode != 0777 || entry.Size != int64(len(entry.Target)) {
				return fmt.Errorf("invalid symlink entry")
			}
			// Location-dependent confinement is checked during traversal.
			if err := validateLink(strings.Repeat("d/", maxDepth)+entry.Name, entry.Target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported entry type %q", entry.Type)
		}
	}
	return nil
}

func EncodeTree(t Tree) ([]byte, Descriptor, error) {
	t.Entries = append([]Entry{}, t.Entries...)
	sort.Slice(t.Entries, func(i, j int) bool { return t.Entries[i].Name < t.Entries[j].Name })
	if err := t.Validate(); err != nil {
		return nil, Descriptor{}, err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return nil, Descriptor{}, err
	}
	if len(b) > maxTreeBytes {
		return nil, Descriptor{}, fmt.Errorf("tree exceeds metadata bound")
	}
	return b, descriptor(TreeMediaType, b), nil
}

func descriptor(media string, b []byte) Descriptor {
	return Descriptor{MediaType: media, Digest: environmentartifact.DigestBytes(b), Size: int64(len(b))}
}

func decodeCanonical(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if !bytes.Equal(b, encoded) {
		return fmt.Errorf("noncanonical metadata encoding")
	}
	return nil
}

func (e *Engine) readMetadata(ctx context.Context, d Descriptor) ([]byte, error) {
	if e.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	r, size, err := e.Store.GetObjectByDigest(ctx, d.Digest)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if size != d.Size || size > maxTreeBytes {
		return nil, fmt.Errorf("metadata size mismatch")
	}
	b, err := io.ReadAll(io.LimitReader(r, d.Size+1))
	if err != nil {
		return nil, err
	}
	if err := environmentartifact.VerifyDescriptor(d, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (e *Engine) LoadTree(ctx context.Context, d Descriptor) (Tree, error) {
	var t Tree
	if err := validateDescriptor(d, TreeMediaType); err != nil {
		return t, err
	}
	b, err := e.readMetadata(ctx, d)
	if err != nil {
		return t, err
	}
	if err := decodeCanonical(b, &t); err != nil {
		return t, err
	}
	return t, t.Validate()
}

func (e *Engine) LoadSnapshot(ctx context.Context, digest Digest) (Snapshot, error) {
	var s Snapshot
	if e.Store == nil || len(digest.Hex()) != 64 {
		return s, fmt.Errorf("invalid store or snapshot digest")
	}
	r, size, err := e.Store.GetObjectByDigest(ctx, digest)
	if err != nil {
		return s, err
	}
	defer r.Close()
	if size < 0 || size > maxTreeBytes {
		return s, fmt.Errorf("snapshot exceeds bound")
	}
	b, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return s, err
	}
	if err := environmentartifact.VerifyDescriptor(Descriptor{Digest: digest, Size: size}, b); err != nil {
		return s, err
	}
	if err := decodeCanonical(b, &s); err != nil {
		return s, err
	}
	if s.Version != Version {
		return s, fmt.Errorf("unsupported snapshot version")
	}
	return s, validateDescriptor(s.Root, TreeMediaType)
}
