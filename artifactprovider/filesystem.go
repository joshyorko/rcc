package artifactprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/joshyorko/rcc/environmentartifact"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (it *contextReader) Read(target []byte) (int, error) {
	if err := it.ctx.Err(); err != nil {
		return 0, err
	}
	return it.reader.Read(target)
}

const maxManifestBytes = 16 << 20
const filesystemRestoreMarker = ".restore-state"

type filesystemRestoreState struct {
	Created []string `json:"created"`
}

var filesystemRestorePublishHook func(string) error

type Filesystem struct {
	root     string
	commitMu sync.Mutex
}

// Cleanup removes only private interrupted-upload files. Immutable objects and
// manifests are never selected, so restart cleanup cannot delete published data.
func (it *Filesystem) Cleanup(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	removed := 0
	err := filepath.Walk(it.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		name := info.Name()
		if len(name) > 0 && (strings.HasPrefix(name, ".upload-") || strings.HasPrefix(name, ".manifest-")) {
			if err := os.Remove(path); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

func NewFilesystem(root string) (*Filesystem, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve provider root: %w", err)
	}
	if absolute == string(filepath.Separator) {
		return nil, fmt.Errorf("provider root cannot be the filesystem root")
	}
	provider := &Filesystem{root: absolute}
	if err := provider.initialize(); err != nil {
		return nil, err
	}
	if err := provider.recoverRestore(); err != nil {
		return nil, err
	}
	return provider, nil
}

func (it *Filesystem) recoverRestore() error {
	b, err := os.ReadFile(filepath.Join(it.root, filesystemRestoreMarker))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state filesystemRestoreState
	if err := json.Unmarshal(b, &state); err != nil {
		return fmt.Errorf("invalid restore marker: %w", err)
	}
	for _, rel := range state.Created {
		if rel == "" || filepath.IsAbs(rel) || rel == ".." || filepath.Clean(rel) != rel || strings.Contains(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid restore marker path")
		}
		if err := os.Remove(filepath.Join(it.root, rel)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(filepath.Join(it.root, filesystemRestoreMarker)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncFilesystemDir(it.root)
}

func syncFilesystemDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (it *Filesystem) objectPath(digest environmentartifact.Digest) string {
	hex := digest.Hex()
	if len(hex) != 64 {
		return ""
	}
	return filepath.Join(it.root, "objects", "sha256", hex[:2], hex[2:4], hex)
}

func (it *Filesystem) manifestPath(digest environmentartifact.Digest) string {
	hex := digest.Hex()
	if len(hex) != 64 {
		return ""
	}
	return filepath.Join(it.root, "manifests", "sha256", hex[:2], hex[2:4], hex)
}

func (it *Filesystem) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}, MaxObjectBytes: maxProviderObjectBytes, MaxManifestBytes: maxManifestBytes, MaxRequestBytes: maxProviderJSONBytes, RangeSupport: false, ResumeSupport: false, SafeRestart: true}, nil
}

func (it *Filesystem) Health(ctx context.Context) (Health, error) {
	if err := ctx.Err(); err != nil {
		return Health{}, err
	}
	if _, err := os.Stat(it.root); err != nil {
		return Health{Storage: "unavailable", Ready: false}, fmt.Errorf("%w: storage: %v", ErrNotReady, err)
	}
	return Health{Ready: true, Storage: "ok", Capability: "ok", Auth: "not-applicable", Quota: "ok", GC: "idle"}, nil
}

func (it *Filesystem) GetObjectByDigest(ctx context.Context, digest environmentartifact.Digest) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if len(digest.Hex()) != 64 {
		return nil, 0, fmt.Errorf("invalid object digest")
	}
	file, err := os.Open(it.objectPath(digest))
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	if info.Size() < 0 || info.Size() > maxProviderObjectBytes {
		_ = file.Close()
		return nil, 0, fmt.Errorf("invalid provider object size")
	}
	return file, info.Size(), nil
}

func (it *Filesystem) MissingObjects(ctx context.Context, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	missing := make([]environmentartifact.Digest, 0)
	for _, descriptor := range descriptors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		found, err := it.hasObject(descriptor)
		if err != nil {
			return nil, err
		}
		if !found {
			missing = append(missing, descriptor.Digest)
		}
	}
	sort.Slice(missing, func(left, right int) bool { return missing[left].String() < missing[right].String() })
	return missing, nil
}
