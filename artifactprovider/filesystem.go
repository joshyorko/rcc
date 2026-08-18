package artifactprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/joshyorko/rcc/environmentartifact"
)

type Filesystem struct {
	root string
}

func NewFilesystem(root string) (*Filesystem, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve provider root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create provider root: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect provider root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("provider root must be a real directory")
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

func (it *Filesystem) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}}, nil
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
