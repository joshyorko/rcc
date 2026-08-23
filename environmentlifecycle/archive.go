package environmentlifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

// ImportArchiveRequest describes an offline carrier import. Import validates
// the complete closure before writing any content to the local provider.
type ImportArchiveRequest struct {
	Path      string
	PutObject func(context.Context, artifactprovider.Blob) error
}

// ImportArchive verifies and stores a canonical archive in the local content
// cache. The returned Manifest is the same identity an HTTP provider resolves.
func ImportArchive(ctx context.Context, request ImportArchiveRequest) (environmentartifact.Manifest, error) {
	if request.Path == "" {
		return environmentartifact.Manifest{}, fmt.Errorf("archive path is required")
	}
	content, err := os.ReadFile(filepath.Clean(request.Path))
	if err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("read environment archive: %w", err)
	}
	entries, err := environmentartifact.ReadArchive(content)
	if err != nil {
		return environmentartifact.Manifest{}, err
	}
	manifest, err := environmentartifact.ValidateArchive(entries)
	if err != nil {
		return environmentartifact.Manifest{}, err
	}
	if err := manifest.Platform.CompatibleWithCurrent(); err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("reject incompatible environment archive: %w", err)
	}
	local, err := artifactprovider.NewFilesystem(filepath.Join(common.Product.Home(), "artifacts", "v1", "content"))
	if err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("initialize local artifact cache: %w", err)
	}
	descriptors := []struct {
		descriptor environmentartifact.Descriptor
		content    []byte
	}{
		{manifest.Specification.Descriptor, entries[environmentartifact.ArchiveSpecificationDir+manifest.Specification.Digest.Hex()]},
		{manifest.LegacyBlueprint.Descriptor, entries[environmentartifact.ArchiveBlueprintDir+manifest.LegacyBlueprint.Digest.Hex()]},
		{manifest.Catalogs[0].Descriptor, entries[environmentartifact.ArchiveCatalogDirectory+manifest.Catalogs[0].Digest.Hex()]},
		{manifest.ObjectIndex, entries[environmentartifact.ArchiveObjectIndex]},
	}
	index, err := environmentartifact.DecodeObjectIndex(entries[environmentartifact.ArchiveObjectIndex])
	if err != nil {
		return environmentartifact.Manifest{}, err
	}
	for _, entry := range index.Entries {
		descriptor := environmentartifact.Descriptor{MediaType: "application/vnd.rcc.hololib.object.v12+gzip", Digest: entry.StoredDigest, Size: entry.StoredSize}
		descriptors = append(descriptors, struct {
			descriptor environmentartifact.Descriptor
			content    []byte
		}{descriptor, entries[environmentartifact.ArchiveObjectDirectory+entry.StoredDigest.Hex()]})
	}
	allDescriptors := make([]environmentartifact.Descriptor, 0, len(descriptors))
	putObject := local.PutObject
	if request.PutObject != nil {
		putObject = request.PutObject
	}
	for _, item := range descriptors {
		allDescriptors = append(allDescriptors, item.descriptor)
	}
	missing, err := artifactprovider.MissingObjectsBatched(ctx, local, allDescriptors)
	if err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("check archive rollback set: %w", err)
	}
	missingSet := make(map[string]bool, len(missing))
	for _, digest := range missing {
		missingSet[digest.Hex()] = true
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, item := range descriptors {
			if missingSet[item.descriptor.Digest.Hex()] {
				_ = local.RemoveObject(item.descriptor.Digest)
			}
		}
	}()
	for _, item := range descriptors {
		if err := ctx.Err(); err != nil {
			return environmentartifact.Manifest{}, err
		}
		if err := putObject(ctx, artifactprovider.Blob{Descriptor: item.descriptor, Reader: io.NopCloser(bytesReader(item.content))}); err != nil {
			return environmentartifact.Manifest{}, fmt.Errorf("cache archive object %s: %w", item.descriptor.Digest, err)
		}
	}
	if err := local.CommitManifest(ctx, entries[environmentartifact.ArchiveManifest]); err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("commit imported manifest: %w", err)
	}
	committed = true
	return manifest, nil
}

func bytesReader(content []byte) io.Reader { return &sliceReader{content: content} }

type sliceReader struct {
	content []byte
	offset  int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.content) {
		return 0, io.EOF
	}
	n := copy(p, r.content[r.offset:])
	r.offset += n
	return n, nil
}
