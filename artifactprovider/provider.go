package artifactprovider

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type Capabilities struct {
	SchemaVersions   []int    `json:"schemaVersions"`
	DigestAlgorithms []string `json:"digestAlgorithms"`
	Encodings        []string `json:"encodings"`
	MaxObjectBytes   int64    `json:"maxObjectBytes,omitempty"`
	MaxManifestBytes int64    `json:"maxManifestBytes,omitempty"`
	MaxRequestBytes  int64    `json:"maxRequestBytes,omitempty"`
	RangeSupport     bool     `json:"rangeSupport,omitempty"`
	ResumeSupport    bool     `json:"resumeSupport,omitempty"`
	SafeRestart      bool     `json:"safeRestart,omitempty"`
	RequiredVersions []int    `json:"requiredVersions,omitempty"`
	RequiredFeatures []string `json:"requiredFeatures,omitempty"`
}

type ProtocolCapabilities struct {
	Protocol     string       `json:"protocol"`
	Versions     []int        `json:"versions"`
	Capabilities Capabilities `json:"capabilities"`
}

var (
	ErrQuotaExceeded = errors.New("artifact provider quota exceeded")
	ErrRateLimited   = errors.New("artifact provider rate limited")
	ErrNotReady      = errors.New("artifact provider not ready")
)

type Health struct {
	Ready      bool   `json:"ready"`
	Storage    string `json:"storage"`
	Capability string `json:"capability"`
	Auth       string `json:"auth"`
	Quota      string `json:"quota"`
	Degraded   bool   `json:"degraded"`
	Corrupt    bool   `json:"corrupt"`
	GC         string `json:"gc"`
	Objects    int64  `json:"objects"`
	Manifests  int64  `json:"manifests"`
	Bytes      int64  `json:"bytes"`
}

type HealthProvider interface {
	Health(context.Context) (Health, error)
}

type ObjectInfo struct {
	Digest     environmentartifact.Digest `json:"digest"`
	Size       int64                      `json:"size"`
	ModifiedAt time.Time                  `json:"modifiedAt"`
}
type ManifestInfo struct {
	Digest     environmentartifact.Digest `json:"digest"`
	Size       int64                      `json:"size"`
	ModifiedAt time.Time                  `json:"modifiedAt"`
}
type Retention struct {
	MaxAge        time.Duration
	KeepManifests int
}
type GCReport struct {
	ObjectsScanned     int
	ManifestsScanned   int
	ObjectsRemoved     int
	ManifestsRemoved   int
	BytesReclaimed     int64
	ProvisionalRemoved int
}
type ProviderV1Enumerable interface {
	ListObjects(context.Context) ([]ObjectInfo, error)
	ListManifests(context.Context) ([]ManifestInfo, error)
}
type ProviderV1Admin interface {
	ProviderV1Enumerable
	GarbageCollect(context.Context, Retention) (GCReport, error)
	Cleanup(context.Context) (int, error)
	Repair(context.Context) (Health, error)
}
type ProviderV1Backup interface {
	Backup(context.Context, io.Writer) error
	Restore(context.Context, io.Reader) error
}
type ProviderV1ReadOnly interface{ ReadOnly() bool }
type ObjectReaderProvider interface {
	GetObjectByDigest(context.Context, environmentartifact.Digest) (io.ReadCloser, int64, error)
}

func ValidateCapabilities(c Capabilities) error {
	if len(c.SchemaVersions) > 8 || len(c.DigestAlgorithms) > 8 || len(c.Encodings) > 8 {
		return errors.New("artifact provider capabilities exceed bounds")
	}
	if c.MaxObjectBytes < 0 || c.MaxManifestBytes < 0 || c.MaxRequestBytes < 0 {
		return errors.New("artifact provider capability limits must be non-negative")
	}
	if (c.RangeSupport || c.ResumeSupport) && !c.SafeRestart {
		return errors.New("restart safety is required for range or resume support")
	}
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
