package environmentartifact

import "fmt"

// LegacyV12Input describes already-validated v12 bytes. Wrapping preserves
// every catalog/object byte; only Manifest v1 metadata is constructed locally.
type LegacyV12Input struct {
	Specification Specification
	SpecificationBytes []byte
	LegacyBlueprint LegacyBlueprint
	LegacyBlueprintBytes []byte
	Catalog CatalogDescriptor
	CatalogBytes []byte
	Index ObjectIndex
	IndexBytes []byte
	Objects map[Digest][]byte
	Platform Platform
	Builder Builder
}

func WrapLegacyV12(input LegacyV12Input) (Manifest, []byte, map[string][]byte, error) {
	if err := input.Platform.Validate(); err != nil { return Manifest{}, nil, nil, err }
	if len(input.SpecificationBytes) == 0 || len(input.LegacyBlueprintBytes) == 0 || len(input.CatalogBytes) == 0 { return Manifest{}, nil, nil, fmt.Errorf("legacy v12 wrapper requires complete source bytes") }
	if err := VerifyDescriptor(input.Specification.Descriptor, input.SpecificationBytes); err != nil { return Manifest{}, nil, nil, err }
	if err := VerifyDescriptor(input.LegacyBlueprint.Descriptor, input.LegacyBlueprintBytes); err != nil { return Manifest{}, nil, nil, err }
	if err := VerifyDescriptor(input.Catalog.Descriptor, input.CatalogBytes); err != nil { return Manifest{}, nil, nil, err }
	indexBytes, err := canonicalEncode(input.Index); if err != nil { return Manifest{}, nil, nil, err }
	if err := VerifyDescriptor(Descriptor{Digest: DigestBytes(indexBytes), Size: int64(len(indexBytes))}, input.IndexBytes); err != nil { return Manifest{}, nil, nil, err }
	manifest, manifestBytes, err := NewManifest(ManifestInput{Specification: input.Specification, LegacyBlueprint: input.LegacyBlueprint, Platform: input.Platform, Builder: input.Builder, Catalogs: []CatalogDescriptor{input.Catalog}, ObjectIndex: Descriptor{MediaType: ObjectIndexMediaType, Digest: DigestBytes(input.IndexBytes), Size: int64(len(input.IndexBytes))}, Requirements: Requirements{CatalogReader:"v12", Encoding:"gzip", LegacyLogicalDigestAlgorithm:"sha256", RequiredFeatures: []string{}}})
	if err != nil { return Manifest{}, nil, nil, err }
	entries := map[string][]byte{ArchiveManifest: manifestBytes, ArchiveObjectIndex: input.IndexBytes, ArchiveSpecificationDir+input.Specification.Digest.Hex(): input.SpecificationBytes, ArchiveBlueprintDir+input.LegacyBlueprint.Digest.Hex(): input.LegacyBlueprintBytes, ArchiveCatalogDirectory+input.Catalog.Digest.Hex(): input.CatalogBytes}
	for digest, content := range input.Objects { entries[ArchiveObjectDirectory+digest.Hex()] = content }
	return manifest, manifestBytes, entries, nil
}
