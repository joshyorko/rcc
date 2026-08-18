package artifactprovider

import (
	"context"
	"io"

	"github.com/joshyorko/rcc/environmentartifact"
)

type Capabilities struct {
	SchemaVersions   []int    `json:"schemaVersions"`
	DigestAlgorithms []string `json:"digestAlgorithms"`
	Encodings        []string `json:"encodings"`
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
