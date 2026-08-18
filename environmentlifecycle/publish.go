package environmentlifecycle

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/environmentartifact"
)

type Builder interface {
	Build(context.Context, string) (BuildResult, error)
}

type BuildResult struct {
	LegacyBlueprint    []byte
	CatalogPath        string
	SpecificationBytes []byte
	SourceKind         string
	Platform           environmentartifact.Platform
	Builder            environmentartifact.Builder
}

type PublishRequest struct {
	RobotFile string
	Provider  artifactprovider.Provider
	Builder   Builder
}

type PublishResult struct {
	ArtifactDigest      environmentartifact.Digest
	SpecificationDigest environmentartifact.Digest
	LegacyBlueprintKey  string
	ObjectCount         int
	UploadedBytes       int64
	ReusedBytes         int64
}

type publishBlob struct {
	descriptor environmentartifact.Descriptor
	open       func() (io.ReadCloser, error)
}

func Publish(ctx context.Context, request PublishRequest) (PublishResult, error) {
	if request.Builder == nil || request.Provider == nil {
		return PublishResult{}, fmt.Errorf("publish requires a builder and provider")
	}
	build, err := request.Builder.Build(ctx, request.RobotFile)
	if err != nil {
		return PublishResult{}, fmt.Errorf("build environment: %w", err)
	}
	if err := environmentartifact.ValidateSpecificationBytes(build.SpecificationBytes); err != nil {
		return PublishResult{}, err
	}
	if environmentartifact.DigestBytes(build.SpecificationBytes) == environmentartifact.DigestBytes(build.LegacyBlueprint) {
		return PublishResult{}, fmt.Errorf("semantic specification and legacy blueprint bytes are conflated")
	}
	inventory, err := environmentartifact.InventoryV12(environmentartifact.InventoryInput{
		CatalogPath:      build.CatalogPath,
		LegacyBlueprint:  build.LegacyBlueprint,
		ExpectedPlatform: build.Platform.RCCPlatform,
	})
	if err != nil {
		return PublishResult{}, fmt.Errorf("inventory built environment: %w", err)
	}

	capabilities, err := request.Provider.Capabilities(ctx)
	if err != nil {
		return PublishResult{}, fmt.Errorf("discover provider capabilities: %w", err)
	}
	if err := artifactprovider.ValidateV1Capabilities(capabilities); err != nil {
		return PublishResult{}, err
	}

	specificationDescriptor := descriptorFor(environmentartifact.SpecificationMediaType, build.SpecificationBytes)
	indexDescriptor := descriptorFor(environmentartifact.ObjectIndexMediaType, inventory.IndexBytes)
	manifest, manifestBytes, err := environmentartifact.NewManifest(environmentartifact.ManifestInput{
		Specification: environmentartifact.Specification{
			Descriptor: specificationDescriptor,
			SourceKind: build.SourceKind,
			Platform:   build.Platform,
			Builder:    build.Builder,
		},
		LegacyBlueprint: environmentartifact.LegacyBlueprint{
			Descriptor:         inventory.LegacyBlueprint,
			LegacyBlueprintKey: inventory.LegacyBlueprintKey,
		},
		Platform:    build.Platform,
		Builder:     build.Builder,
		Catalogs:    []environmentartifact.CatalogDescriptor{inventory.Catalog},
		ObjectIndex: indexDescriptor,
		Requirements: environmentartifact.Requirements{
			CatalogReader: "v12", Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256", RequiredFeatures: []string{},
		},
	})
	if err != nil {
		return PublishResult{}, fmt.Errorf("construct manifest: %w", err)
	}

	blobs := []publishBlob{
		bytesBlob(specificationDescriptor, build.SpecificationBytes),
		bytesBlob(inventory.LegacyBlueprint, build.LegacyBlueprint),
		fileBlob(inventory.Catalog.Descriptor, build.CatalogPath),
		bytesBlob(indexDescriptor, inventory.IndexBytes),
	}
	objectDigests := make([]environmentartifact.Digest, 0, len(inventory.Objects))
	for digest := range inventory.Objects {
		objectDigests = append(objectDigests, digest)
	}
	sort.Slice(objectDigests, func(left, right int) bool { return objectDigests[left].String() < objectDigests[right].String() })
	entryByDigest := make(map[environmentartifact.Digest]environmentartifact.ObjectEntry, len(inventory.Index.Entries))
	for _, entry := range inventory.Index.Entries {
		entryByDigest[entry.StoredDigest] = entry
	}
	for _, digest := range objectDigests {
		entry := entryByDigest[digest]
		blobs = append(blobs, fileBlob(environmentartifact.Descriptor{
			MediaType: "application/vnd.rcc.hololib.object.v12+gzip", Digest: digest, Size: entry.StoredSize,
		}, inventory.Objects[digest]))
	}

	descriptors := make([]environmentartifact.Descriptor, len(blobs))
	var totalBytes int64
	for index, blob := range blobs {
		descriptors[index] = blob.descriptor
		totalBytes += blob.descriptor.Size
	}
	missing, err := request.Provider.MissingObjects(ctx, descriptors)
	if err != nil {
		return PublishResult{}, fmt.Errorf("negotiate missing objects: %w", err)
	}
	missingSet := make(map[environmentartifact.Digest]struct{}, len(missing))
	for _, digest := range missing {
		missingSet[digest] = struct{}{}
	}
	var uploadedBytes int64
	for _, blob := range blobs {
		if _, upload := missingSet[blob.descriptor.Digest]; !upload {
			continue
		}
		reader, err := blob.open()
		if err != nil {
			return PublishResult{}, fmt.Errorf("open object %s: %w", blob.descriptor.Digest, err)
		}
		err = request.Provider.PutObject(ctx, artifactprovider.Blob{Descriptor: blob.descriptor, Reader: reader})
		closeErr := reader.Close()
		if err != nil {
			return PublishResult{}, fmt.Errorf("upload object %s: %w", blob.descriptor.Digest, err)
		}
		if closeErr != nil {
			return PublishResult{}, fmt.Errorf("close object %s: %w", blob.descriptor.Digest, closeErr)
		}
		uploadedBytes += blob.descriptor.Size
	}
	if err := request.Provider.CommitManifest(ctx, manifestBytes); err != nil {
		return PublishResult{}, fmt.Errorf("commit manifest %s: %w", manifest.ArtifactDigest, err)
	}
	return PublishResult{
		ArtifactDigest: manifest.ArtifactDigest, SpecificationDigest: specificationDescriptor.Digest,
		LegacyBlueprintKey: inventory.LegacyBlueprintKey, ObjectCount: inventory.Index.Count,
		UploadedBytes: uploadedBytes, ReusedBytes: totalBytes - uploadedBytes,
	}, nil
}

func descriptorFor(mediaType string, content []byte) environmentartifact.Descriptor {
	return environmentartifact.Descriptor{MediaType: mediaType, Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
}

func bytesBlob(descriptor environmentartifact.Descriptor, content []byte) publishBlob {
	immutable := append([]byte(nil), content...)
	return publishBlob{descriptor: descriptor, open: func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(immutable)), nil
	}}
}

func fileBlob(descriptor environmentartifact.Descriptor, path string) publishBlob {
	return publishBlob{descriptor: descriptor, open: func() (io.ReadCloser, error) { return os.Open(path) }}
}

func supports[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
