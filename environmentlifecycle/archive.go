package environmentlifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

// ImportArchiveRequest describes an offline carrier import. Import validates
// the complete closure before writing any content to the local provider.
type ImportArchiveRequest struct {
	Path         string
	PutObject    func(context.Context, artifactprovider.Blob) error
	TrustCarrier artifacttrust.Carrier
}

// ImportArchive verifies and stores a canonical archive in the local content
// cache. The returned Manifest is the same identity an HTTP provider resolves.
func ImportArchive(ctx context.Context, request ImportArchiveRequest) (environmentartifact.Manifest, error) {
	if request.Path == "" {
		return environmentartifact.Manifest{}, fmt.Errorf("archive path is required")
	}
	archive, err := environmentartifact.OpenArchiveFile(filepath.Clean(request.Path))
	if err != nil {
		return environmentartifact.Manifest{}, err
	}
	defer func() { _ = archive.Close() }()
	manifestBytes, err := archive.ReadMember(environmentartifact.ArchiveManifest)
	if err != nil {
		return environmentartifact.Manifest{}, err
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("decode environment manifest: %w", err)
	}
	indexBytes, err := archive.ReadMember(environmentartifact.ArchiveObjectIndex)
	if err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("read object index: %w", err)
	}
	if err := environmentartifact.VerifyDescriptor(manifest.ObjectIndex, indexBytes); err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("verify object index: %w", err)
	}
	objectIndex, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		return environmentartifact.Manifest{}, err
	}
	platformIndexBytes, found, optionalErr := readOptionalArchiveMember(archive, environmentartifact.ArchivePlatformIndex)
	if optionalErr != nil {
		return environmentartifact.Manifest{}, optionalErr
	}
	if found {
		platformIndex, indexErr := environmentartifact.DecodePlatformIndex(platformIndexBytes)
		if indexErr != nil {
			return environmentartifact.Manifest{}, fmt.Errorf("decode platform index: %w", indexErr)
		}
		if platformIndex.Specification != manifest.Specification.Digest {
			return environmentartifact.Manifest{}, fmt.Errorf("platform index specification does not match manifest")
		}
		selected, selectErr := platformIndex.Select(environmentartifact.CurrentPlatform())
		if selectErr != nil || selected != manifest.ArtifactDigest {
			return environmentartifact.Manifest{}, fmt.Errorf("platform index selected artifact does not match archive manifest")
		}
	}
	for _, item := range []struct {
		name string
		desc environmentartifact.Descriptor
	}{
		{environmentartifact.ArchiveSpecificationDir + manifest.Specification.Digest.Hex(), manifest.Specification.Descriptor},
		{environmentartifact.ArchiveBlueprintDir + manifest.LegacyBlueprint.Digest.Hex(), manifest.LegacyBlueprint.Descriptor},
		{environmentartifact.ArchiveCatalogDirectory + manifest.Catalogs[0].Digest.Hex(), manifest.Catalogs[0].Descriptor},
	} {
		content, readErr := archive.ReadMember(item.name)
		if readErr != nil {
			return environmentartifact.Manifest{}, fmt.Errorf("read archive member %s: %w", item.name, readErr)
		}
		if verifyErr := environmentartifact.VerifyDescriptor(item.desc, content); verifyErr != nil {
			return environmentartifact.Manifest{}, fmt.Errorf("verify %s: %w", item.name, verifyErr)
		}
	}
	for _, entry := range objectIndex.Entries {
		name := environmentartifact.ArchiveObjectDirectory + entry.StoredDigest.Hex()
		size, found := archive.MemberSize(name)
		if !found {
			return environmentartifact.Manifest{}, fmt.Errorf("environment archive is missing %s", name)
		}
		if size != entry.StoredSize {
			return environmentartifact.Manifest{}, fmt.Errorf("verify object %s: size %d does not match descriptor size %d", entry.LegacyObjectID, size, entry.StoredSize)
		}
	}
	trustEntries := make(map[string][]byte)
	for _, kind := range []string{"provenance", "sbom", "signature", "revocations"} {
		name := environmentartifact.ArchiveAttestationDir + kind + ".json"
		content, found, readErr := readOptionalArchiveMember(archive, name)
		if readErr != nil {
			return environmentartifact.Manifest{}, fmt.Errorf("read archive trust attachment: %w", readErr)
		}
		if found {
			trustEntries[name] = content
		}
	}
	if err := importArchiveTrust(trustEntries, manifest.ArtifactDigest.String(), nil); err != nil {
		return environmentartifact.Manifest{}, err
	}
	if err := manifest.Platform.CompatibleWithCurrent(); err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("reject incompatible environment archive: %w", err)
	}
	local, err := artifactprovider.NewFilesystem(filepath.Join(common.Product.Home(), "artifacts", "v1", "content"))
	if err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("initialize local artifact cache: %w", err)
	}
	descriptors := []environmentartifact.Descriptor{manifest.Specification.Descriptor, manifest.LegacyBlueprint.Descriptor, manifest.Catalogs[0].Descriptor, manifest.ObjectIndex}
	for _, entry := range objectIndex.Entries {
		descriptor := environmentartifact.Descriptor{MediaType: "application/vnd.rcc.hololib.object.v12+gzip", Digest: entry.StoredDigest, Size: entry.StoredSize}
		descriptors = append(descriptors, descriptor)
	}
	allDescriptors := make([]environmentartifact.Descriptor, 0, len(descriptors))
	putObject := local.PutObject
	if request.PutObject != nil {
		putObject = request.PutObject
	}
	for _, descriptor := range descriptors {
		allDescriptors = append(allDescriptors, descriptor)
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
		for _, descriptor := range descriptors {
			if missingSet[descriptor.Digest.Hex()] {
				_ = local.RemoveObject(descriptor.Digest)
			}
		}
	}()
	for descriptorIndex, descriptor := range descriptors {
		if err := ctx.Err(); err != nil {
			return environmentartifact.Manifest{}, err
		}
		var reader io.Reader
		if descriptorIndex < 4 {
			var content []byte
			switch descriptorIndex {
			case 0:
				content, err = archive.ReadMember(environmentartifact.ArchiveSpecificationDir + manifest.Specification.Digest.Hex())
			case 1:
				content, err = archive.ReadMember(environmentartifact.ArchiveBlueprintDir + manifest.LegacyBlueprint.Digest.Hex())
			case 2:
				content, err = archive.ReadMember(environmentartifact.ArchiveCatalogDirectory + manifest.Catalogs[0].Digest.Hex())
			case 3:
				content = indexBytes
			}
			if err != nil {
				return environmentartifact.Manifest{}, err
			}
			reader = bytes.NewReader(content)
		} else {
			entry := objectIndex.Entries[descriptorIndex-4]
			member, openErr := archive.OpenMember(environmentartifact.ArchiveObjectDirectory + entry.StoredDigest.Hex())
			if openErr != nil {
				return environmentartifact.Manifest{}, openErr
			}
			reader = &verifiedArchiveReader{reader: member, closer: member, digest: descriptor.Digest.Hex(), size: descriptor.Size}
		}
		putErr := putObject(ctx, artifactprovider.Blob{Descriptor: descriptor, Reader: reader})
		if closer, ok := reader.(io.Closer); ok {
			if closeErr := closer.Close(); putErr == nil {
				putErr = closeErr
			}
		}
		if putErr != nil {
			return environmentartifact.Manifest{}, fmt.Errorf("cache archive object %s: %w", descriptor.Digest, putErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return environmentartifact.Manifest{}, err
	}
	if err := importArchiveTrust(trustEntries, manifest.ArtifactDigest.String(), request.TrustCarrier); err != nil {
		return environmentartifact.Manifest{}, err
	}
	if err := local.CommitManifest(ctx, manifestBytes); err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("commit imported manifest: %w", err)
	}
	committed = true
	return manifest, nil
}

func importArchiveTrust(entries map[string][]byte, artifact string, target artifacttrust.Carrier) error {
	archiveCarrier := &artifacttrust.ArchiveCarrier{Files: map[string][]byte{}}
	for _, kind := range []string{"provenance", "sbom", "signature", "revocations"} {
		data, ok := entries[environmentartifact.ArchiveAttestationDir+kind+".json"]
		if !ok {
			continue
		}
		archiveCarrier.Files[artifacttrust.AttachmentName(artifact, kind)] = data
	}
	_, err := artifacttrust.LoadAttachments(archiveCarrier, artifact)
	if err != nil {
		return fmt.Errorf("validate archive trust attachments: %w", err)
	}
	if target == nil {
		return nil
	}
	for _, kind := range []string{"provenance", "sbom", "signature", "revocations"} {
		data, getErr := artifacttrust.GetAttachment(archiveCarrier, artifact, kind)
		if getErr != nil {
			if errors.Is(getErr, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read archive %s trust attachment: %w", kind, getErr)
		}
		if err := target.Write(artifacttrust.AttachmentName(artifact, kind), data); err != nil {
			return fmt.Errorf("store archive %s trust attachment: %w", kind, err)
		}
	}
	return nil
}

func readOptionalArchiveMember(archive *environmentartifact.ArchiveReader, name string) ([]byte, bool, error) {
	if !archive.HasMember(name) {
		return nil, false, nil
	}
	content, err := archive.ReadMember(name)
	if err != nil {
		return nil, true, err
	}
	return content, true, nil
}

type verifiedArchiveReader struct {
	reader io.Reader
	closer io.Closer
	hash   hash.Hash
	digest string
	size   int64
	read   int64
}

func (r *verifiedArchiveReader) Read(p []byte) (int, error) {
	if r.hash == nil {
		r.hash = sha256.New()
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		r.read += int64(n)
		if r.read > r.size {
			return n, fmt.Errorf("archive object exceeds descriptor size")
		}
		_, _ = r.hash.Write(p[:n])
	}
	if err == io.EOF && (r.read != r.size || hex.EncodeToString(r.hash.Sum(nil)) != r.digest) {
		return n, fmt.Errorf("archive object size or digest mismatch")
	}
	return n, err
}

func (r *verifiedArchiveReader) Close() error { return r.closer.Close() }
