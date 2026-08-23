package environmentlifecycle

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
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
	Compatibility      environmentartifact.CompatibilityRequirements
}

type PublishRequest struct {
	RobotFile         string
	Provider          artifactprovider.Provider
	Builder           Builder
	TrustCarrier      artifacttrust.Carrier
	TrustProvenance   *artifacttrust.Provenance
	TrustSBOM         *artifacttrust.SBOM
	TrustSignatures   []artifacttrust.Signature
	TrustRevocations  []artifacttrust.Revocation
	TrustSigningKey   ed25519.PrivateKey
	TrustSigningKeyID string
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
	if err := build.Compatibility.Validate(); err != nil {
		return PublishResult{}, fmt.Errorf("validate build compatibility: %w", err)
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
			Compatibility: build.Compatibility,
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
	if err := publishTrustAttachments(request, manifest, build, inventory); err != nil {
		return PublishResult{}, err
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

func publishTrustAttachments(request PublishRequest, manifest environmentartifact.Manifest, _ BuildResult, inventory environmentartifact.Inventory) error {
	if request.TrustCarrier == nil {
		return nil
	}
	artifact := manifest.ArtifactDigest.String()
	provenance := request.TrustProvenance
	if provenance == nil {
		generated := artifacttrust.Provenance{
			MediaType: artifacttrust.ProvenanceMediaType, ArtifactDigest: artifact,
			SpecificationDigest: manifest.Specification.Digest.String(), Platform: manifest.Platform.RCCPlatform,
			Builder: manifest.Builder.Kind, RCCVersion: manifest.Builder.RCCVersion,
			CreatedAt:             artifacttrust.FreshTimestamp(time.Now().UTC()),
			LegacyBlueprintDigest: manifest.LegacyBlueprint.Digest.String(), LegacyBlueprintKey: manifest.LegacyBlueprint.LegacyBlueprintKey,
			CatalogDigest: manifest.Catalogs[0].Digest.String(), ManifestDigest: artifact,
			BuildIdentity: manifest.Builder.Kind + "@" + manifest.Builder.CompatibilityKey,
		}
		provenance = &generated
	}
	if provenance != nil {
		if provenance.ArtifactDigest != artifact || (provenance.Platform != "" && provenance.Platform != manifest.Platform.RCCPlatform) || (provenance.Builder != "" && provenance.Builder != manifest.Builder.Kind) {
			return fmt.Errorf("provenance does not match manifest identity")
		}
		data, err := artifacttrust.CanonicalProvenance(*provenance)
		if err != nil {
			return fmt.Errorf("construct provenance attachment: %w", err)
		}
		if err := artifacttrust.PutAttachment(request.TrustCarrier, artifact, "provenance", data); err != nil {
			return fmt.Errorf("publish provenance attachment: %w", err)
		}
	}
	sbom := request.TrustSBOM
	if sbom == nil {
		generated, err := generatedSBOM(manifest, inventory)
		if err != nil {
			return fmt.Errorf("construct generated SBOM: %w", err)
		}
		sbom = &generated
	}
	if sbom != nil {
		data, err := artifacttrust.CanonicalSBOM(*sbom)
		if err != nil {
			return fmt.Errorf("construct SBOM attachment: %w", err)
		}
		if err := artifacttrust.PutAttachment(request.TrustCarrier, artifact, "sbom", data); err != nil {
			return fmt.Errorf("publish SBOM attachment: %w", err)
		}
	}
	signatures := append([]artifacttrust.Signature(nil), request.TrustSignatures...)
	if len(request.TrustSigningKey) > 0 {
		if request.TrustSigningKeyID == "" {
			return fmt.Errorf("trust signing key ID is required")
		}
		signature, err := artifacttrust.Sign(artifact, request.TrustSigningKeyID, request.TrustSigningKey)
		if err != nil {
			return fmt.Errorf("sign artifact trust: %w", err)
		}
		signatures = append(signatures, signature)
	}
	if len(signatures) > 0 {
		_, data, err := artifacttrust.NewSignatureBundle(artifact, signatures)
		if err != nil {
			return fmt.Errorf("construct signature attachment: %w", err)
		}
		if err := artifacttrust.PutAttachment(request.TrustCarrier, artifact, "signature", data); err != nil {
			return fmt.Errorf("publish signature attachment: %w", err)
		}
	}
	_, data, err := artifacttrust.NewRevocationBundleAt(artifact, request.TrustRevocations, time.Now().UTC(), "rcc-publish")
	if err != nil {
		return fmt.Errorf("construct revocation attachment: %w", err)
	}
	if err := artifacttrust.PutAttachment(request.TrustCarrier, artifact, "revocations", data); err != nil {
		return fmt.Errorf("publish revocation attachment: %w", err)
	}
	return nil
}

func generatedSBOM(manifest environmentartifact.Manifest, inventory environmentartifact.Inventory) (artifacttrust.SBOM, error) {
	components := []artifacttrust.Component{{
		Name: "rcc", Version: manifest.Builder.RCCVersion, PackageType: "rcc-builder", Source: manifest.Builder.Kind,
	}}
	for _, entry := range inventory.Index.Entries {
		components = append(components, artifacttrust.Component{
			Name: entry.LegacyObjectID, Version: entry.StoredDigest.String(), PackageType: "rcc-hololib-object-v12",
			Hash: entry.StoredDigest.String(),
		})
	}
	sbom, _, err := artifacttrust.NewSBOM(manifest.ArtifactDigest.String(), components)
	return sbom, err
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
