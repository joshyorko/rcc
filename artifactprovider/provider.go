package artifactprovider

import (
	"context"
	"errors"
	"io"

	"github.com/joshyorko/rcc/environmentartifact"
)

type Capabilities struct {
	SchemaVersions   []int    `json:"schemaVersions"`
	DigestAlgorithms []string `json:"digestAlgorithms"`
	Encodings        []string `json:"encodings"`
}

type ProtocolCapabilities struct {
	Protocol string `json:"protocol"`
	Versions []int `json:"versions"`
	Capabilities Capabilities `json:"capabilities"`
}

var (
	ErrQuotaExceeded = errors.New("artifact provider quota exceeded")
	ErrRateLimited = errors.New("artifact provider rate limited")
	ErrNotReady = errors.New("artifact provider not ready")
)

type Health struct {
	Ready bool `json:"ready"`
	Storage string `json:"storage"`
	Capability string `json:"capability"`
	Auth string `json:"auth"`
	Quota string `json:"quota"`
	Degraded bool `json:"degraded"`
	Corrupt bool `json:"corrupt"`
	GC string `json:"gc"`
	Objects int64 `json:"objects"`
	Manifests int64 `json:"manifests"`
	Bytes int64 `json:"bytes"`
}

type HealthProvider interface { Health(context.Context) (Health, error) }

func ValidateCapabilities(c Capabilities) error {
	if len(c.SchemaVersions) > 8 || len(c.DigestAlgorithms) > 8 || len(c.Encodings) > 8 { return errors.New("artifact provider capabilities exceed bounds") }
	return ValidateV1Capabilities(c)
}

type Blob struct {
	Descriptor environmentartifact.Descriptor
	Reader     io.Reader
}

type Provider interface {
	Capabilities(context.Context) (Capabilities, error)
	ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error)
	MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error)
	PutObject(context.Context, Blob) error
	GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error)
	CommitManifest(context.Context, []byte) error
}

var _ Provider = (*Filesystem)(nil)
