package environmentartifact

import (
	"fmt"
	"regexp"

	"github.com/joshyorko/rcc/htfs"
)

const (
	ManifestMediaType        = "application/vnd.rcc.environment.manifest.v1+json"
	SpecificationMediaType   = "application/vnd.rcc.environment.specification.v1+json"
	LegacyBlueprintMediaType = "application/vnd.rcc.environment.legacy-blueprint.v1+yaml"
	CatalogV12MediaType      = "application/vnd.rcc.holotree.catalog.v12+gzip"
	ObjectIndexMediaType     = "application/vnd.rcc.environment.object-index.v1+json"
	SchemaVersionV1          = 1
)

var legacyKeyPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

type Platform struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	RCCPlatform string `json:"rccPlatform"`
}

type Builder struct {
	Kind             string `json:"kind"`
	RCCVersion       string `json:"rccVersion"`
	CompatibilityKey string `json:"compatibilityKey"`
}

type Specification struct {
	Descriptor
	SourceKind string   `json:"sourceKind"`
	Platform   Platform `json:"platform"`
	Builder    Builder  `json:"builder"`
}

type LegacyBlueprint struct {
	Descriptor
	LegacyBlueprintKey string `json:"legacyBlueprintKey"`
}

type CatalogDescriptor struct {
	Descriptor
	LegacyName string `json:"legacyName"`
}

type Requirements struct {
	CatalogReader                string   `json:"catalogReader"`
	Encoding                     string   `json:"encoding"`
	LegacyLogicalDigestAlgorithm string   `json:"legacyLogicalDigestAlgorithm"`
	RequiredFeatures             []string `json:"requiredFeatures"`
}

type Manifest struct {
	MediaType       string              `json:"mediaType"`
	SchemaVersion   int                 `json:"schemaVersion"`
	ArtifactDigest  Digest              `json:"artifactDigest"`
	Specification   Specification       `json:"specification"`
	LegacyBlueprint LegacyBlueprint     `json:"legacyBlueprint"`
	Platform        Platform            `json:"platform"`
	Builder         Builder             `json:"builder"`
	Catalogs        []CatalogDescriptor `json:"catalogs"`
	ObjectIndex     Descriptor          `json:"objectIndex"`
	Requirements    Requirements        `json:"requirements"`
}

type ManifestInput struct {
	Specification   Specification
	LegacyBlueprint LegacyBlueprint
	Platform        Platform
	Builder         Builder
	Catalogs        []CatalogDescriptor
	ObjectIndex     Descriptor
	Requirements    Requirements
}

type manifestIdentity struct {
	MediaType       string              `json:"mediaType"`
	SchemaVersion   int                 `json:"schemaVersion"`
	Specification   Specification       `json:"specification"`
	LegacyBlueprint LegacyBlueprint     `json:"legacyBlueprint"`
	Platform        Platform            `json:"platform"`
	Builder         Builder             `json:"builder"`
	Catalogs        []CatalogDescriptor `json:"catalogs"`
	ObjectIndex     Descriptor          `json:"objectIndex"`
	Requirements    Requirements        `json:"requirements"`
}

func NewManifest(input ManifestInput) (Manifest, []byte, error) {
	manifest := Manifest{
		MediaType:       ManifestMediaType,
		SchemaVersion:   SchemaVersionV1,
		Specification:   input.Specification,
		LegacyBlueprint: input.LegacyBlueprint,
		Platform:        input.Platform,
		Builder:         input.Builder,
		Catalogs:        append([]CatalogDescriptor(nil), input.Catalogs...),
		ObjectIndex:     input.ObjectIndex,
		Requirements:    input.Requirements,
	}
	manifest.Requirements.RequiredFeatures = append([]string{}, input.Requirements.RequiredFeatures...)
	identity, err := manifest.IdentityBytes()
	if err != nil {
		return Manifest{}, nil, err
	}
	manifest.ArtifactDigest = DigestBytes(identity)
	if err := manifest.Validate(); err != nil {
		return Manifest{}, nil, err
	}
	content, err := manifest.CanonicalBytes()
	return manifest, content, err
}

func DecodeManifest(content []byte) (Manifest, error) {
	var manifest Manifest
	if err := strictDecodeCanonical(content, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (it Manifest) CanonicalBytes() ([]byte, error) {
	return canonicalEncode(it)
}

func (it Manifest) IdentityBytes() ([]byte, error) {
	return canonicalEncode(manifestIdentity{
		MediaType: it.MediaType, SchemaVersion: it.SchemaVersion,
		Specification: it.Specification, LegacyBlueprint: it.LegacyBlueprint,
		Platform: it.Platform, Builder: it.Builder, Catalogs: it.Catalogs,
		ObjectIndex: it.ObjectIndex, Requirements: it.Requirements,
	})
}

func (it Manifest) Validate() error {
	if it.MediaType != ManifestMediaType || it.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("unsupported manifest media type or schema version")
	}
	if it.Platform != (Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}) {
		return fmt.Errorf("unsupported artifact platform: %+v", it.Platform)
	}
	if it.Specification.MediaType != SpecificationMediaType || it.LegacyBlueprint.MediaType != LegacyBlueprintMediaType || it.ObjectIndex.MediaType != ObjectIndexMediaType {
		return fmt.Errorf("unsupported manifest descriptor media type")
	}
	if !legacyKeyPattern.MatchString(it.LegacyBlueprint.LegacyBlueprintKey) {
		return fmt.Errorf("invalid legacy blueprint key")
	}
	if len(it.Catalogs) != 1 || it.Catalogs[0].MediaType != CatalogV12MediaType {
		return fmt.Errorf("manifest v1 requires exactly one v12 gzip catalog")
	}
	if it.Catalogs[0].LegacyName != htfs.CatalogName(it.LegacyBlueprint.LegacyBlueprintKey) {
		return fmt.Errorf("catalog legacy name does not match legacy blueprint key")
	}
	if it.Requirements.CatalogReader != "v12" || it.Requirements.Encoding != "gzip" || it.Requirements.LegacyLogicalDigestAlgorithm != "sha256" || len(it.Requirements.RequiredFeatures) != 0 {
		return fmt.Errorf("unsupported manifest requirements")
	}
	if it.Requirements.RequiredFeatures == nil {
		return fmt.Errorf("requiredFeatures must be a canonical empty array")
	}
	for _, descriptor := range []Descriptor{it.Specification.Descriptor, it.LegacyBlueprint.Descriptor, it.Catalogs[0].Descriptor, it.ObjectIndex} {
		if descriptor.Digest.hex == "" || descriptor.Size < 0 || descriptor.MediaType == "" {
			return fmt.Errorf("invalid descriptor")
		}
	}
	identity, err := it.IdentityBytes()
	if err != nil {
		return err
	}
	if it.ArtifactDigest != DigestBytes(identity) {
		return fmt.Errorf("artifact digest does not match manifest identity")
	}
	return nil
}
