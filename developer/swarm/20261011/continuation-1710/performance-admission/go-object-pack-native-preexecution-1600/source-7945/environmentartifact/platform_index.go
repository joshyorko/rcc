package environmentartifact

import "fmt"

// PlatformArtifact binds one immutable worker platform to one Artifact
// identity. The index itself is only a selector and never an identity alias.
type PlatformArtifact struct {
	Platform Platform `json:"platform"`
	Artifact Digest   `json:"artifact"`
}

type PlatformIndex struct {
	MediaType     string             `json:"mediaType"`
	SchemaVersion int                `json:"schemaVersion"`
	Specification Digest             `json:"specification"`
	Artifacts     []PlatformArtifact `json:"artifacts"`
}

const PlatformIndexMediaType = "application/vnd.rcc.environment.index.v1+json"

func NewPlatformIndex(specification Digest, artifacts []PlatformArtifact) (PlatformIndex, []byte, error) {
	index := PlatformIndex{MediaType: PlatformIndexMediaType, SchemaVersion: 1, Specification: specification, Artifacts: append([]PlatformArtifact(nil), artifacts...)}
	if err := index.Validate(); err != nil {
		return PlatformIndex{}, nil, err
	}
	content, err := canonicalEncode(index)
	return index, content, err
}

func DecodePlatformIndex(content []byte) (PlatformIndex, error) {
	var index PlatformIndex
	if err := strictDecodeCanonical(content, &index); err != nil {
		return PlatformIndex{}, err
	}
	if err := index.Validate(); err != nil {
		return PlatformIndex{}, err
	}
	return index, nil
}

func (index PlatformIndex) Validate() error {
	if index.MediaType != PlatformIndexMediaType || index.SchemaVersion != 1 || index.Specification.Hex() == "" {
		return fmt.Errorf("invalid platform index")
	}
	seen := make(map[string]bool, len(index.Artifacts))
	for _, item := range index.Artifacts {
		if err := item.Platform.Validate(); err != nil {
			return err
		}
		if item.Artifact.Hex() == "" || seen[item.Platform.RCCPlatform] {
			return fmt.Errorf("duplicate or empty platform artifact")
		}
		seen[item.Platform.RCCPlatform] = true
	}
	if len(index.Artifacts) == 0 {
		return fmt.Errorf("platform index has no artifacts")
	}
	return nil
}

func (index PlatformIndex) Select(platform Platform) (Digest, error) {
	if err := platform.Validate(); err != nil {
		return Digest{}, err
	}
	for _, item := range index.Artifacts {
		if item.Platform == platform {
			return item.Artifact, nil
		}
	}
	return Digest{}, fmt.Errorf("no exact environment artifact for platform %s", platform.RCCPlatform)
}
