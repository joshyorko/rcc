package artifactprovider

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

type Filesystem struct {
	root     string
	commitMu sync.Mutex
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
	return provider, nil
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
	return Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}}, nil
}

func (it *Filesystem) Health(ctx context.Context) (Health, error) {
	if err := ctx.Err(); err != nil { return Health{}, err }
	if _, err := os.Stat(it.root); err != nil { return Health{Storage: "unavailable", Ready: false}, fmt.Errorf("%w: storage: %v", ErrNotReady, err) }
	return Health{Ready: true, Storage: "ok", Capability: "ok", Auth: "not-applicable", Quota: "ok", GC: "idle"}, nil
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
