package artifactprovider

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

func crashRestoreBoundary(name string) {
	if os.Getenv("RCC_PROVIDER_RESTORE_CRASH") == name {
		os.Exit(86)
	}
}
func sanitizeAuditText(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, value)
}

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

const (
	maxProviderArchiveMembers   = 4096
	maxProviderArchiveBytes     = int64(128 << 30)
	MaxDescriptorFanout         = 4096
	maxProviderDescriptorFanout = MaxDescriptorFanout
)

type ProtocolCapabilities struct {
	Protocol        string       `json:"protocol"`
	Versions        []int        `json:"versions"`
	SelectedVersion int          `json:"selectedVersion"`
	Extensions      []string     `json:"extensions,omitempty"`
	AuthRequired    bool         `json:"authRequired,omitempty"`
	RestartOutcome  string       `json:"restartOutcome,omitempty"`
	TransferOutcome string       `json:"transferOutcome,omitempty"`
	AuthScheme      string       `json:"authScheme,omitempty"`
	AuthChallenge   string       `json:"authChallenge,omitempty"`
	RetentionPolicy string       `json:"retentionPolicy,omitempty"`
	Immutability    string       `json:"immutability,omitempty"`
	Semantics       string       `json:"semantics,omitempty"`
	Capabilities    Capabilities `json:"capabilities"`
}

type MutationError struct {
	Operation string
	Committed bool
	Objects   int64
	Manifests int64
	Bytes     int64
	Err       error
}

func (e *MutationError) Error() string {
	return sanitizeAuditText(e.Operation + " mutation committed but policy state was not durable: " + e.Err.Error())
}
func (e *MutationError) Unwrap() error { return e.Err }

var (
	ErrQuotaExceeded = errors.New("artifact provider quota exceeded")
	ErrRateLimited   = errors.New("artifact provider rate limited")
	ErrNotReady      = errors.New("artifact provider not ready")
)

type Health struct {
	Ready         bool   `json:"ready"`
	Storage       string `json:"storage"`
	Capability    string `json:"capability"`
	Auth          string `json:"auth"`
	Quota         string `json:"quota"`
	Degraded      bool   `json:"degraded"`
	Corrupt       bool   `json:"corrupt"`
	GC            string `json:"gc"`
	Objects       int64  `json:"objects"`
	Manifests     int64  `json:"manifests"`
	Bytes         int64  `json:"bytes"`
	LatencyMS     int64  `json:"latencyMs,omitempty"`
	Process       string `json:"process,omitempty"`
	Audit         string `json:"audit,omitempty"`
	Error         string `json:"error,omitempty"`
	Operations    int64  `json:"operations,omitempty"`
	Errors        int64  `json:"errors,omitempty"`
	Requests      int64  `json:"requests,omitempty"`
	Corruptions   int64  `json:"corruptions,omitempty"`
	QuotaFailures int64  `json:"quotaFailures,omitempty"`
	Repairs       int64  `json:"repairs,omitempty"`
	GCRuns        int64  `json:"gcRuns,omitempty"`
	AuditEvents   int64  `json:"auditEvents,omitempty"`
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
type AuditRecord struct {
	At        time.Time `json:"at"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Provider  string    `json:"provider"`
	Reference string    `json:"reference"`
	Digest    string    `json:"digest,omitempty"`
}
type ProviderV1Audit interface {
	Audit(context.Context) ([]AuditRecord, error)
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

func validateObjectIndexBudget(index environmentartifact.ObjectIndex) error {
	if index.Count < 0 || index.Count > maxProviderArchiveMembers {
		return errors.New("object index entry count exceeds bound")
	}
	if index.TotalStoredBytes < 0 || index.TotalStoredBytes > maxProviderArchiveBytes || index.TotalLogicalBytes < 0 || index.TotalLogicalBytes > maxProviderArchiveBytes {
		return errors.New("object index totals exceed bound")
	}
	for _, entry := range index.Entries {
		if entry.StoredSize > maxProviderObjectBytes || entry.LogicalSize > maxProviderArchiveBytes {
			return errors.New("object index entry exceeds bound")
		}
		if entry.StoredSize > 0 && (entry.StoredSize > (maxProviderArchiveBytes-maxManifestBytes)/1024 || entry.LogicalSize > entry.StoredSize*1024+maxManifestBytes) {
			return errors.New("object index expansion exceeds bound")
		}
	}
	return nil
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

// MissingObjectsBatched negotiates an arbitrarily large object closure while
// preserving the bounded fanout of each provider request.
func MissingObjectsBatched(ctx context.Context, provider Provider, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	if provider == nil {
		return nil, errors.New("artifact provider is required")
	}
	missing := make([]environmentartifact.Digest, 0, len(descriptors))
	for start := 0; start < len(descriptors); start += MaxDescriptorFanout {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + MaxDescriptorFanout
		if end > len(descriptors) {
			end = len(descriptors)
		}
		batch, err := provider.MissingObjects(ctx, descriptors[start:end])
		if err != nil {
			return nil, err
		}
		missing = append(missing, batch...)
	}
	return missing, nil
}

var _ Provider = (*Filesystem)(nil)
var _ ProviderV1Audit = (*Filesystem)(nil)
