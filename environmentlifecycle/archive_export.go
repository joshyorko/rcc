package environmentlifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
)

type ExportArchiveRequest struct {
	ArtifactDigest environmentartifact.Digest
	Provider       artifactprovider.Provider
	OutputPath     string
}

// ExportArchive retrieves and verifies one provider artifact, then emits the
// canonical offline carrier. It uses the same descriptor closure as acquire.
func ExportArchive(ctx context.Context, request ExportArchiveRequest) (environmentartifact.Manifest, error) {
	if request.Provider == nil || request.OutputPath == "" {
		return environmentartifact.Manifest{}, fmt.Errorf("archive export requires provider and output path")
	}
	manifestBytes, err := request.Provider.ResolveManifest(ctx, request.ArtifactDigest)
	if err != nil { return environmentartifact.Manifest{}, fmt.Errorf("resolve artifact manifest: %w", err) }
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil { return environmentartifact.Manifest{}, err }
	if manifest.ArtifactDigest != request.ArtifactDigest { return environmentartifact.Manifest{}, fmt.Errorf("resolved manifest identity does not match requested artifact") }
	entries := map[string][]byte{environmentartifact.ArchiveManifest: manifestBytes}
	for _, item := range []struct{ desc environmentartifact.Descriptor; name string }{
		{manifest.Specification.Descriptor, environmentartifact.ArchiveSpecificationDir + manifest.Specification.Digest.Hex()},
		{manifest.LegacyBlueprint.Descriptor, environmentartifact.ArchiveBlueprintDir + manifest.LegacyBlueprint.Digest.Hex()},
		{manifest.Catalogs[0].Descriptor, environmentartifact.ArchiveCatalogDirectory + manifest.Catalogs[0].Digest.Hex()},
		{manifest.ObjectIndex, environmentartifact.ArchiveObjectIndex},
	} {
		content, err := readProviderObject(ctx, request.Provider, item.desc)
		if err != nil { return environmentartifact.Manifest{}, fmt.Errorf("fetch archive member %s: %w", item.name, err) }
		entries[item.name] = content
	}
	index, err := environmentartifact.DecodeObjectIndex(entries[environmentartifact.ArchiveObjectIndex])
	if err != nil { return environmentartifact.Manifest{}, err }
	for _, object := range index.Entries {
		desc := environmentartifact.Descriptor{MediaType: "application/vnd.rcc.hololib.object.v12+gzip", Digest: object.StoredDigest, Size: object.StoredSize}
		content, err := readProviderObject(ctx, request.Provider, desc)
		if err != nil { return environmentartifact.Manifest{}, fmt.Errorf("fetch archive object %s: %w", object.LegacyObjectID, err) }
		entries[environmentartifact.ArchiveObjectDirectory+object.StoredDigest.Hex()] = content
	}
	if _, err := environmentartifact.ValidateArchive(entries); err != nil { return environmentartifact.Manifest{}, err }
	if err := writeArchiveAtomically(request.OutputPath, entries); err != nil { return environmentartifact.Manifest{}, err }
	return manifest, nil
}

func writeArchiveAtomically(output string, entries map[string][]byte) error {
	directory := filepath.Dir(output)
	temporary, err := os.CreateTemp(directory, ".rcc-environment-archive-*")
	if err != nil { return fmt.Errorf("create archive temporary file: %w", err) }
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := environmentartifact.WriteArchive(temporary, entries); err != nil { _ = temporary.Close(); return err }
	if err := temporary.Close(); err != nil { return err }
	if err := os.Rename(temporaryName, output); err != nil { return fmt.Errorf("publish environment archive: %w", err) }
	return nil
}
