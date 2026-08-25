package environmentlifecycle

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
)

type verifiedContent struct {
	manifest      environmentartifact.Manifest
	index         environmentartifact.ObjectIndex
	compatibility CompatibilityReceipt
}

var collectWorkerCapabilities = currentWorkerCapabilities

func localContentRoot() string {
	return filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
}

func newLocalContentProvider() (artifactprovider.Provider, error) {
	return artifactprovider.NewFilesystem(localContentRoot())
}

func acquireVerifiedContent(ctx context.Context, artifactDigest environmentartifact.Digest, remote artifactprovider.Provider) (verifiedContent, error) {
	var content verifiedContent
	err := withContentTransaction(ctx, localContentRoot(), func(ctx context.Context) error {
		var err error
		content, err = acquireVerifiedContentLocked(ctx, artifactDigest, remote)
		return err
	})
	return content, err
}

func acquireVerifiedContentLocked(ctx context.Context, artifactDigest environmentartifact.Digest, remote artifactprovider.Provider) (verifiedContent, error) {
	return acquireVerifiedContentLockedWithLocal(ctx, artifactDigest, remote, nil)
}

func acquireVerifiedContentLockedWithLocal(ctx context.Context, artifactDigest environmentartifact.Digest, remote, local artifactprovider.Provider) (verifiedContent, error) {
	if remote == nil || len(artifactDigest.Hex()) != 64 {
		return verifiedContent{}, fmt.Errorf("acquire requires a provider and canonical artifact digest")
	}
	if _, err := os.Lstat(common.HololibCompressMarker()); err == nil {
		return verifiedContent{}, fmt.Errorf("artifact v1 does not support a consumer with compress.no active")
	} else if !os.IsNotExist(err) {
		return verifiedContent{}, fmt.Errorf("inspect consumer Hololib compression mode: %w", err)
	}

	manifestBytes, err := remote.ResolveManifest(ctx, artifactDigest)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("resolve manifest %s: %w", artifactDigest, err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("validate resolved manifest: %w", err)
	}
	if manifest.ArtifactDigest != artifactDigest {
		return verifiedContent{}, fmt.Errorf("resolved manifest identity does not match requested artifact")
	}
	compatibility, err := preflightCompatibility(ctx, manifest)
	if err != nil {
		return verifiedContent{}, err
	}

	if local == nil {
		local, err = newLocalContentProvider()
		if err != nil {
			return verifiedContent{}, fmt.Errorf("initialize local artifact content cache: %w", err)
		}
	}
	primary := []environmentartifact.Descriptor{
		manifest.Specification.Descriptor,
		manifest.LegacyBlueprint.Descriptor,
		manifest.Catalogs[0].Descriptor,
		manifest.ObjectIndex,
	}
	for _, descriptor := range primary {
		if err := cacheProviderObject(ctx, remote, local, descriptor); err != nil {
			return verifiedContent{}, err
		}
	}

	indexBytes, err := readProviderObject(ctx, local, manifest.ObjectIndex)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read cached object index: %w", err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("validate object index: %w", err)
	}
	objectDescriptors := make([]environmentartifact.Descriptor, 0, len(index.Entries))
	for _, entry := range index.Entries {
		descriptor := environmentartifact.Descriptor{
			MediaType: "application/vnd.rcc.hololib.object.v12+gzip",
			Digest:    entry.StoredDigest, Size: entry.StoredSize,
		}
		if err := cacheProviderObject(ctx, remote, local, descriptor); err != nil {
			return verifiedContent{}, err
		}
		objectDescriptors = append(objectDescriptors, descriptor)
	}

	specificationBytes, err := readProviderObject(ctx, local, manifest.Specification.Descriptor)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read cached semantic specification: %w", err)
	}
	if len(specificationBytes) == 0 {
		return verifiedContent{}, fmt.Errorf("semantic specification is empty")
	}
	if err := environmentartifact.ValidateSpecificationBytes(specificationBytes); err != nil {
		return verifiedContent{}, err
	}
	legacyBlueprint, err := readProviderObject(ctx, local, manifest.LegacyBlueprint.Descriptor)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read cached legacy blueprint: %w", err)
	}
	if common.BlueprintHash(legacyBlueprint) != manifest.LegacyBlueprint.LegacyBlueprintKey {
		return verifiedContent{}, fmt.Errorf("legacy blueprint bytes do not match compatibility key")
	}
	catalogBytes, err := readProviderObject(ctx, local, manifest.Catalogs[0].Descriptor)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read cached v12 catalog: %w", err)
	}
	if err := validateAcquiredCatalog(catalogBytes, manifest, index); err != nil {
		return verifiedContent{}, err
	}
	for position, descriptor := range objectDescriptors {
		content, err := readProviderObject(ctx, local, descriptor)
		if err != nil {
			return verifiedContent{}, fmt.Errorf("read cached legacy object %s: %w", descriptor.Digest, err)
		}
		if err := verifyLogicalObject(index.Entries[position], content); err != nil {
			return verifiedContent{}, err
		}
	}
	if err := local.CommitManifest(ctx, manifestBytes); err != nil {
		return verifiedContent{}, fmt.Errorf("commit local verified content: %w", err)
	}

	for position, descriptor := range objectDescriptors {
		content, err := readProviderObject(ctx, local, descriptor)
		if err != nil {
			return verifiedContent{}, fmt.Errorf("re-open verified legacy object %s: %w", descriptor.Digest, err)
		}
		legacyID := index.Entries[position].LegacyObjectID
		components := []string{"library", legacyID[:2], legacyID[2:4], legacyID[4:6], legacyID}
		if err := installLegacyImmutable(common.HololibLocation(), components, descriptor, content); err != nil {
			return verifiedContent{}, fmt.Errorf("install legacy object %s: %w", legacyID, err)
		}
	}
	if err := registerConsumerLegacyCatalog(manifest, catalogBytes); err != nil {
		return verifiedContent{}, fmt.Errorf("register consumer legacy catalog: %w", err)
	}
	if err := writeReferenceRoot(manifest, index); err != nil {
		return verifiedContent{}, fmt.Errorf("persist manifest reference root: %w", err)
	}
	return verifiedContent{manifest: manifest, index: index, compatibility: compatibility}, nil
}

func registerCachedConsumerLegacyCatalog(ctx context.Context, local artifactprovider.Provider, manifest environmentartifact.Manifest) error {
	catalogBytes, err := readProviderObject(ctx, local, manifest.Catalogs[0].Descriptor)
	if err != nil {
		return fmt.Errorf("read canonical catalog for consumer registration: %w", err)
	}
	return registerConsumerLegacyCatalog(manifest, catalogBytes)
}

func registerConsumerLegacyCatalog(manifest environmentartifact.Manifest, canonicalCatalog []byte) error {
	portable, err := htfs.LoadPortableCatalogBytes(canonicalCatalog)
	if err != nil {
		return err
	}
	root := portable.Root()
	if root.Blueprint != manifest.LegacyBlueprint.LegacyBlueprintKey || root.Platform != manifest.Platform.RCCPlatform {
		return fmt.Errorf("canonical catalog identity does not match manifest")
	}
	producerIdentity := root.Identity
	if err := portable.Rebase(common.HolotreeLocation(), producerIdentity); err != nil {
		return fmt.Errorf("rebase legacy catalog to consumer: %w", err)
	}
	catalogBytes, infoBytes, err := portable.Snapshot()
	if err != nil {
		return err
	}
	legacyName := manifest.Catalogs[0].LegacyName
	if err := writeAtomicMutable(common.HololibLocation(), []string{"catalog", legacyName + ".info"}, infoBytes); err != nil {
		return fmt.Errorf("publish consumer legacy catalog info: %w", err)
	}
	if err := writeAtomicMutable(common.HololibLocation(), []string{"catalog", legacyName}, catalogBytes); err != nil {
		return fmt.Errorf("publish consumer legacy catalog: %w", err)
	}
	return nil
}

func preflightCompatibility(ctx context.Context, manifest environmentartifact.Manifest) (CompatibilityReceipt, error) {
	if err := manifest.Platform.CompatibleWithCurrent(); err != nil {
		return CompatibilityReceipt{}, fmt.Errorf("reject incompatible environment artifact: %w", err)
	}
	worker, err := collectWorkerCapabilities(ctx, manifest.Requirements.Compatibility)
	if err != nil {
		return CompatibilityReceipt{}, fmt.Errorf("inspect worker compatibility: %w", err)
	}
	if err := environmentartifact.EvaluateCompatibility(manifest.Requirements.Compatibility, worker); err != nil {
		return CompatibilityReceipt{}, fmt.Errorf("reject incompatible environment artifact: %w", err)
	}
	return CompatibilityReceipt{
		SchemaVersion: 1, Status: "compatible", Requirements: manifest.Requirements.Compatibility, Worker: worker,
	}, nil
}

func cacheProviderObject(ctx context.Context, remote, local artifactprovider.Provider, descriptor environmentartifact.Descriptor) error {
	missing, err := local.MissingObjects(ctx, []environmentartifact.Descriptor{descriptor})
	if err != nil {
		return fmt.Errorf("inspect local content %s: %w", descriptor.Digest, err)
	}
	if len(missing) == 0 {
		return nil
	}
	reader, err := remote.GetObject(ctx, descriptor)
	if err != nil {
		return fmt.Errorf("fetch provider object %s: %w", descriptor.Digest, err)
	}
	putErr := local.PutObject(ctx, artifactprovider.Blob{Descriptor: descriptor, Reader: reader})
	closeErr := reader.Close()
	if putErr != nil {
		return fmt.Errorf("local cache write object %s: %w", descriptor.Digest, putErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close provider object %s: %w", descriptor.Digest, closeErr)
	}
	return nil
}

func readProviderObject(ctx context.Context, provider artifactprovider.Provider, descriptor environmentartifact.Descriptor) ([]byte, error) {
	reader, err := provider.GetObject(ctx, descriptor)
	if err != nil {
		return nil, err
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, descriptor.Size+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := environmentartifact.VerifyDescriptor(descriptor, content); err != nil {
		return nil, err
	}
	return content, nil
}

func validateAcquiredCatalog(content []byte, manifest environmentartifact.Manifest, index environmentartifact.ObjectIndex) error {
	directory, err := os.MkdirTemp("", "rcc-environment-catalog-")
	if err != nil {
		return fmt.Errorf("create catalog validation directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()
	path := filepath.Join(directory, "catalog.gz")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("stage catalog for validation: %w", err)
	}
	root, err := htfs.NewRoot(".")
	if err != nil {
		return fmt.Errorf("create catalog validation view: %w", err)
	}
	if err := root.LoadFrom(path); err != nil {
		return fmt.Errorf("decode acquired v12 catalog: %w", err)
	}
	if root.Platform != manifest.Platform.RCCPlatform || root.Blueprint != manifest.LegacyBlueprint.LegacyBlueprintKey {
		return fmt.Errorf("acquired catalog platform or legacy blueprint key mismatch")
	}
	if err := environmentartifact.ValidateV12Catalog(root, index, root.Identity); err != nil {
		return fmt.Errorf("validate acquired v12 catalog: %w", err)
	}
	return nil
}

func verifyLogicalObject(entry environmentartifact.ObjectEntry, stored []byte) error {
	reader, err := gzip.NewReader(bytes.NewReader(stored))
	if err != nil {
		return fmt.Errorf("legacy object %s is not gzip: %w", entry.LegacyObjectID, err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(hasher, io.LimitReader(reader, entry.LogicalSize+1))
	closeErr := reader.Close()
	if copyErr != nil {
		return fmt.Errorf("decompress legacy object %s: %w", entry.LegacyObjectID, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close legacy object %s: %w", entry.LegacyObjectID, closeErr)
	}
	if written != entry.LogicalSize || hex.EncodeToString(hasher.Sum(nil)) != entry.LegacyObjectID {
		return fmt.Errorf("legacy object %s logical size or digest mismatch", entry.LegacyObjectID)
	}
	return nil
}
