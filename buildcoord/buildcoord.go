// Package buildcoord provides optional, filesystem-backed cold-build coordination.
package buildcoord

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
)

var (
	ErrStaleClaim         = errors.New("stale or expired build claim")
	ErrDivergentArtifact  = errors.New("divergent artifact for equivalent build key")
	ErrUnverifiedArtifact = errors.New("artifact is not verified")
	ErrClaimBusy          = errors.New("build claim is owned by another live builder")
	ErrUnsafeState        = errors.New("unsafe coordinator state")
	ErrWaitTimeout        = errors.New("timed out waiting for build artifact")
	ErrStagingBoundary    = errors.New("executor did not use the claimed staging boundary")
)

type Clock interface{ Now() time.Time }
type RealClock struct{}
type ArtifactVerifier interface{ VerifyArtifact(Artifact) error }
type ArtifactVerifierFunc func(Artifact) error

func (f ArtifactVerifierFunc) VerifyArtifact(a Artifact) error { return f(a) }

// TrustVerifier binds publication to RCC's keyed artifact trust policy.
type TrustVerifier struct {
	Policy      artifacttrust.Policy
	Keys        map[string]ed25519.PublicKey
	Revocations []artifacttrust.Revocation
	Local       bool
	Platform    string
	Builder     string
	keyed       bool
}

func (v TrustVerifier) VerifyArtifact(a Artifact) error {
	if !v.keyed {
		return fmt.Errorf("keyed artifact trust verifier is required")
	}
	if err := VerifyArtifactProof(a); err != nil {
		return err
	}
	return v.Policy.Evaluate(v.Local, ArtifactTrustDigest(a), v.Platform, v.Builder, a.Signatures, v.Revocations, v.Keys)
}

// NewTrustVerifier creates the only production trust verifier accepted by the
// coordinator. The key set and policy are copied and validated so callers
// cannot later mutate the authorization boundary behind the coordinator.
func NewTrustVerifier(policy artifacttrust.Policy, keys map[string]ed25519.PublicKey, revocations []artifacttrust.Revocation, local bool, platform, builder string) (*TrustVerifier, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("at least one trusted artifact key is required")
	}
	trusted := make(map[string]ed25519.PublicKey, len(keys))
	for id, key := range keys {
		if id == "" || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid trusted artifact key %q", id)
		}
		trusted[id] = append(ed25519.PublicKey(nil), key...)
	}
	if policy.RequireSignature {
		if len(policy.AcceptedKeys) == 0 {
			return nil, fmt.Errorf("signature policy must name accepted keys")
		}
		for _, id := range policy.AcceptedKeys {
			if _, ok := trusted[id]; !ok {
				return nil, fmt.Errorf("signature policy key %q is not trusted", id)
			}
		}
	}
	if platform == "" || builder == "" {
		return nil, fmt.Errorf("trust verifier platform and builder are required")
	}
	return &TrustVerifier{
		Policy: policy, Keys: trusted, Revocations: append([]artifacttrust.Revocation(nil), revocations...),
		Local: local, Platform: platform, Builder: builder, keyed: true,
	}, nil
}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type BuildKey struct {
	SpecificationDigest  string `json:"specificationDigest"`
	Platform             string `json:"platform"`
	BuilderCompatibility string `json:"builderCompatibility"`
	ResolutionPolicy     string `json:"resolutionPolicy,omitempty"`
	TrustPolicy          string `json:"trustPolicy,omitempty"`
	ArtifactSchema       string `json:"artifactSchema,omitempty"`
}

func (k BuildKey) ID() string {
	b, _ := json.Marshal(k)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (k BuildKey) validate() error {
	if k.SpecificationDigest == "" || k.Platform == "" || k.BuilderCompatibility == "" {
		return fmt.Errorf("build key requires specification digest, platform, and builder compatibility")
	}
	return nil
}

type Artifact struct {
	Digest                string                    `json:"digest"`
	Verified              bool                      `json:"verified"`
	Source                string                    `json:"source,omitempty"`
	Nondeterministic      bool                      `json:"nondeterministic,omitempty"`
	ClosureDigest         string                    `json:"closureDigest,omitempty"`
	Provider              string                    `json:"provider,omitempty"`
	ProviderAuthorization string                    `json:"providerAuthorization,omitempty"`
	Signatures            []artifacttrust.Signature `json:"signatures,omitempty"`
	NondeterminismPolicy  string                    `json:"nondeterminismPolicy,omitempty"`
	Completion            *CompletionReceipt        `json:"completion,omitempty"`
	Execution             *ExecutionReceipt         `json:"execution,omitempty"`
}

// CompletionReceipt is the authoritative provider/lifecycle handoff. A
// coordinator may only treat an artifact as a generic fallback result after
// the provider has committed its manifest and verified every referenced
// object.
type CompletionReceipt struct {
	ArtifactDigest    string `json:"artifactDigest"`
	Provider          string `json:"provider"`
	ManifestCommitted bool   `json:"manifestCommitted"`
	ObjectsVerified   bool   `json:"objectsVerified"`
	Lifecycle         string `json:"lifecycle"`
}

type ExecutionReceipt struct {
	StagingRoot          string        `json:"stagingRoot"`
	PolicyDigest         string        `json:"policyDigest"`
	ProcessID            int           `json:"processId"`
	ConfinementPID       int           `json:"confinementPid"`
	MountNamespace       uint64        `json:"mountNamespace"`
	ParentMountNamespace uint64        `json:"parentMountNamespace"`
	CPULimit             int           `json:"cpuLimit,omitempty"`
	MemoryBytes          int64         `json:"memoryBytes,omitempty"`
	Timeout              time.Duration `json:"timeout,omitempty"`
	NetworkIsolated      bool          `json:"networkIsolated"`
	CredentialsExcluded  bool          `json:"credentialsExcluded"`
	FilesystemRestricted bool          `json:"filesystemRestricted"`
}

// ArtifactTrustDigest is the signature subject for a complete published
// artifact. It binds the immutable artifact digest to its closure and provider
// authorization, so changing either cannot pass a digest-only signature.
func ArtifactTrustDigest(artifact Artifact) string {
	content, _ := json.Marshal(struct {
		Digest                string `json:"digest"`
		ClosureDigest         string `json:"closureDigest"`
		Provider              string `json:"provider"`
		ProviderAuthorization string `json:"providerAuthorization"`
	}{artifact.Digest, artifact.ClosureDigest, artifact.Provider, artifact.ProviderAuthorization})
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type Event struct {
	Time              time.Time `json:"time"`
	Key               string    `json:"key"`
	Owner             string    `json:"owner,omitempty"`
	Epoch             uint64    `json:"epoch,omitempty"`
	Event             string    `json:"event"`
	Detail            string    `json:"detail,omitempty"`
	PolicyOutcome     string    `json:"policyOutcome,omitempty"`
	ExistingArtifact  *Artifact `json:"existingArtifact,omitempty"`
	CandidateArtifact *Artifact `json:"candidateArtifact,omitempty"`
}

type NondeterministicRecord struct {
	Time          time.Time `json:"time"`
	Key           string    `json:"key"`
	Existing      Artifact  `json:"existing"`
	Candidate     Artifact  `json:"candidate"`
	Policy        string    `json:"policy,omitempty"`
	PolicyOutcome string    `json:"policyOutcome"`
}

type commitState string

const (
	commitStatePrepared     commitState = "prepared"
	commitStateCommitted    commitState = "committed"
	commitStateAcknowledged commitState = "acknowledged"
)

type commitRecord struct {
	Key       BuildKey    `json:"key"`
	Claim     Claim       `json:"claim"`
	Artifact  Artifact    `json:"artifact"`
	State     commitState `json:"state"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

type BuildRequest struct {
	Root           string
	DiskBytes      int64
	Network        bool
	Credentials    bool
	QuarantineRoot string
	CPULimit       int
	MemoryBytes    int64
	Timeout        time.Duration
}

var ErrUnenforcedBuildPolicy = errors.New("build executor does not enforce the requested resource or network policy")

// ExecutionPolicy is the typed boundary passed to a prewarm executor. An
// executor must enforce these values at the process boundary before invoking
// the environment builder; metadata-only staging is not sufficient.
type ExecutionPolicy struct {
	Root           string
	DiskBytes      int64
	Network        bool
	Credentials    bool
	CPULimit       int
	MemoryBytes    int64
	Timeout        time.Duration
	QuarantineRoot string
}

func (p ExecutionPolicy) RequiresBoundary() bool {
	return p.Network || p.Credentials || p.CPULimit != 0 || p.MemoryBytes != 0 || p.Timeout != 0
}

func (p ExecutionPolicy) Digest() string {
	content, _ := json.Marshal(p)
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ValidateAuthoritativeCompletion(artifact Artifact) error {
	if artifact.Provider == "" || artifact.Completion == nil || artifact.Completion.ArtifactDigest != artifact.Digest || artifact.Completion.Provider == "" || !artifact.Completion.ManifestCommitted || !artifact.Completion.ObjectsVerified || artifact.Completion.Lifecycle == "" {
		return ErrUnverifiedArtifact
	}
	if artifact.Provider != "" && artifact.Completion.Provider != artifact.Provider {
		return ErrUnverifiedArtifact
	}
	return nil
}

func ValidateExecutionReceipt(artifact Artifact, policy ExecutionPolicy) error {
	if !policy.RequiresBoundary() {
		return nil
	}
	if artifact.Execution == nil || artifact.Execution.StagingRoot == "" || mustRealPath(artifact.Execution.StagingRoot) != mustRealPath(policy.Root) || artifact.Execution.PolicyDigest != policy.Digest() || artifact.Execution.CPULimit != policy.CPULimit || artifact.Execution.MemoryBytes != policy.MemoryBytes || artifact.Execution.Timeout != policy.Timeout || (!policy.Network && !artifact.Execution.NetworkIsolated) || !artifact.Execution.CredentialsExcluded || !artifact.Execution.FilesystemRestricted || artifact.Execution.ConfinementPID <= 0 || artifact.Execution.MountNamespace == 0 || artifact.Execution.ParentMountNamespace == 0 || artifact.Execution.MountNamespace == artifact.Execution.ParentMountNamespace {
		return ErrUnenforcedBuildPolicy
	}
	return nil
}

// ProcessBoundary is the optional concrete process/container enforcement
// seam. A typed builder may implement equivalent enforcement internally; a
// lifecycle bridge can instead delegate child creation here.
type ProcessBoundary interface {
	Run(context.Context, ExecutionPolicy, func(context.Context) (Artifact, error)) (Artifact, error)
}

type ProcessBoundaryFunc func(context.Context, ExecutionPolicy, func(context.Context) (Artifact, error)) (Artifact, error)

func (f ProcessBoundaryFunc) Run(ctx context.Context, policy ExecutionPolicy, build func(context.Context) (Artifact, error)) (Artifact, error) {
	return f(ctx, policy, build)
}

func (r BuildRequest) ExecutionPolicy(root string) (ExecutionPolicy, error) {
	if r.DiskBytes < 0 || r.CPULimit < 0 || r.MemoryBytes < 0 || r.Timeout < 0 {
		return ExecutionPolicy{}, fmt.Errorf("resource limits cannot be negative")
	}
	if r.Credentials {
		return ExecutionPolicy{}, fmt.Errorf("production credentials are not permitted in build staging")
	}
	return ExecutionPolicy{Root: root, DiskBytes: r.DiskBytes, Network: r.Network, Credentials: false, CPULimit: r.CPULimit, MemoryBytes: r.MemoryBytes, Timeout: r.Timeout, QuarantineRoot: r.QuarantineRoot}, nil
}

// ValidateExecutionStaging proves that a builder is attached to the exact
// owner-private staging directory allocated for this claim.
func ValidateExecutionStaging(claim Claim, policy ExecutionPolicy) error {
	if claim.Staging == "" || policy.Root == "" {
		return ErrStagingBoundary
	}
	claimRoot, err := filepath.EvalSymlinks(claim.Staging)
	if err != nil {
		return errors.Join(ErrStagingBoundary, fmt.Errorf("resolve claim staging: %w", err))
	}
	policyRoot, err := filepath.EvalSymlinks(policy.Root)
	if err != nil {
		return errors.Join(ErrStagingBoundary, fmt.Errorf("resolve execution staging: %w", err))
	}
	if claimRoot != policyRoot {
		return ErrStagingBoundary
	}
	info, err := os.Lstat(claim.Staging)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrStagingBoundary
	}
	content, err := os.ReadFile(filepath.Join(claim.Staging, "policy.json"))
	if err != nil {
		return fmt.Errorf("read staging policy: %w", err)
	}
	var persisted struct {
		Root        string `json:"root"`
		Network     bool   `json:"network"`
		Credentials bool   `json:"credentials"`
		DiskBytes   int64  `json:"diskBytes"`
		CPULimit    int    `json:"cpuLimit"`
		MemoryBytes int64  `json:"memoryBytes"`
		Timeout     string `json:"timeout"`
	}
	if err := json.Unmarshal(content, &persisted); err != nil {
		return fmt.Errorf("decode staging policy: %w", err)
	}
	if persisted.Root != "" {
		persistedRoot, rootErr := filepath.EvalSymlinks(persisted.Root)
		if rootErr != nil || persistedRoot != policyRoot {
			return ErrStagingBoundary
		}
	}
	persistedTimeout, err := time.ParseDuration(persisted.Timeout)
	if err != nil {
		return fmt.Errorf("decode staging timeout: %w", err)
	}
	if persisted.Network != policy.Network || persisted.Credentials != policy.Credentials || persisted.DiskBytes != policy.DiskBytes || persisted.CPULimit != policy.CPULimit || persisted.MemoryBytes != policy.MemoryBytes || persistedTimeout != policy.Timeout {
		return ErrStagingBoundary
	}
	return nil
}

// EnforcedBuilder is the safe prewarm extension point. Implementations own
// the actual child process/container boundary and must apply policy before
// returning a verified Artifact.
type EnforcedBuilder interface {
	Build(context.Context, Claim, ExecutionPolicy) (Artifact, error)
}

// AuthoritativeCompletionVerifier is required for coordinator-loss fallback.
// A caller-supplied ready bit or receipt is not authority by itself; the
// verifier must read back the committed provider/lifecycle state.
type AuthoritativeCompletionVerifier interface {
	VerifyCompletion(context.Context, Artifact) error
}

type EnforcedBuilderFunc func(context.Context, Claim, ExecutionPolicy) (Artifact, error)

func (f EnforcedBuilderFunc) Build(ctx context.Context, claim Claim, policy ExecutionPolicy) (Artifact, error) {
	return f(ctx, claim, policy)
}

// QuarantineStaging makes failed staging non-authoritative while preserving
// evidence for bounded diagnostics. The resulting path is never a claim or
// artifact location.
func QuarantineStaging(path, root, reason string) (string, error) {
	if path == "" || root == "" {
		return "", fmt.Errorf("staging path and quarantine root are required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	name := filepath.Join(root, fmt.Sprintf("quarantine-%d-%s", time.Now().UnixNano(), strings.ReplaceAll(reason, string(filepath.Separator), "_")))
	if err := os.Rename(path, name); err != nil {
		return "", err
	}
	return name, nil
}

// PrepareStaging creates an owner-private staging directory. Network access is
// deliberately optional: a caller can use an offline/local cache fallback.
func PrepareStaging(req BuildRequest, owner string) (string, func() error, error) {
	if req.Root == "" || owner == "" {
		return "", nil, fmt.Errorf("staging root and owner are required")
	}
	if req.DiskBytes < 0 {
		return "", nil, fmt.Errorf("disk reservation cannot be negative")
	}
	if req.Credentials {
		return "", nil, fmt.Errorf("production credentials are not permitted in build staging")
	}
	d, err := os.MkdirTemp(req.Root, "rcc-build-"+owner+"-")
	if err != nil {
		return "", nil, err
	}
	reservation, err := ReserveDisk(d, req.DiskBytes)
	if err != nil {
		return "", nil, errors.Join(err, os.RemoveAll(d))
	}
	if req.CPULimit < 0 || req.MemoryBytes < 0 || req.Timeout < 0 {
		return "", nil, errors.Join(fmt.Errorf("resource limits cannot be negative"), reservation.Release(), os.RemoveAll(d))
	}
	policy := map[string]any{"root": d, "network": req.Network, "credentials": false, "diskBytes": req.DiskBytes, "cpuLimit": req.CPULimit, "memoryBytes": req.MemoryBytes, "timeout": req.Timeout.String()}
	pb, _ := json.Marshal(policy)
	if e := os.WriteFile(filepath.Join(d, "policy.json"), pb, 0600); e != nil {
		return "", nil, errors.Join(e, reservation.Release(), os.RemoveAll(d))
	}
	return d, func() error {
		releaseErr := reservation.Release()
		removeErr := os.RemoveAll(d)
		return errors.Join(releaseErr, removeErr)
	}, nil
}

type Claim struct {
	Key       BuildKey  `json:"key"`
	Owner     string    `json:"owner"`
	Epoch     uint64    `json:"epoch"`
	ExpiresAt time.Time `json:"expiresAt"`
	Artifact  Artifact  `json:"artifact,omitempty"`
	Staging   string    `json:"-"`
}
type Outcome string

const (
	Claimed          Outcome = "claimed"
	ExistingArtifact Outcome = "existing-artifact"
	Waiting          Outcome = "waiting"
)

type Filesystem struct {
	Root                 string
	Clock                Clock
	lockWait             time.Duration
	requireArtifactProof bool
	verifier             ArtifactVerifier
	NondeterminismPolicy string
}

func newFilesystem(root string, clock Clock) *Filesystem {
	if clock == nil {
		clock = RealClock{}
	}
	return &Filesystem{Root: root, Clock: clock, lockWait: 2 * time.Second}
}

// NewFilesystem constructs a coordinator with mandatory keyed artifact trust.
// Tests in this package use newFilesystem so production callers cannot create
// an unverifying coordinator accidentally.
func NewFilesystem(root string, clock Clock, verifier *TrustVerifier) (*Filesystem, error) {
	if strings.TrimSpace(root) == "" || filepath.Clean(root) == string(filepath.Separator) {
		return nil, fmt.Errorf("coordinator root must be a non-root directory")
	}
	if verifier == nil || !verifier.keyed {
		return nil, fmt.Errorf("keyed artifact trust verifier is required")
	}
	trusted := *verifier
	trusted.Policy.AcceptedKeys = append([]string(nil), verifier.Policy.AcceptedKeys...)
	trusted.Policy.AcceptedBuilders = append([]string(nil), verifier.Policy.AcceptedBuilders...)
	trusted.Policy.AcceptedPlatforms = append([]string(nil), verifier.Policy.AcceptedPlatforms...)
	trusted.Keys = make(map[string]ed25519.PublicKey, len(verifier.Keys))
	for id, key := range verifier.Keys {
		trusted.Keys[id] = append(ed25519.PublicKey(nil), key...)
	}
	trusted.Revocations = append([]artifacttrust.Revocation(nil), verifier.Revocations...)
	c := newFilesystem(root, clock)
	c.requireArtifactProof = true
	c.verifier = &trusted
	return c, nil
}

// NewFilesystemWithVerifier is retained as an explicit alias for callers that
// used the earlier name; it accepts only the keyed verifier type.
func NewFilesystemWithVerifier(root string, clock Clock, verifier any) (*Filesystem, error) {
	trust, ok := verifier.(*TrustVerifier)
	if !ok {
		return nil, fmt.Errorf("keyed artifact trust verifier is required")
	}
	return NewFilesystem(root, clock, trust)
}

func (c *Filesystem) Claim(key BuildKey, owner string, ttl time.Duration) (Claim, Outcome, error) {
	return c.ClaimContext(context.Background(), key, owner, ttl)
}
func (c *Filesystem) ClaimContext(ctx context.Context, key BuildKey, owner string, ttl time.Duration) (claim Claim, outcome Outcome, err error) {
	if err := key.validate(); err != nil || owner == "" || ttl <= 0 {
		if err != nil {
			return Claim{}, "", err
		}
		return Claim{}, "", fmt.Errorf("owner and positive TTL are required")
	}
	release, err := c.lock(ctx, key)
	if err != nil {
		return Claim{}, "", err
	}
	defer func() {
		if closeErr := release(); err == nil {
			err = closeErr
		}
	}()
	if artifact, ok, err := c.readArtifact(key); err != nil {
		return Claim{}, "", err
	} else if ok {
		if err := c.verifyArtifact(artifact); err != nil {
			return Claim{}, "", errors.Join(ErrUnsafeState, err)
		}
		if err := c.recoverCommittedLocked(key, artifact); err != nil {
			return Claim{}, "", err
		}
		return Claim{Key: key, Artifact: artifact}, ExistingArtifact, nil
	}
	previous, ok, err := c.readClaim(key)
	if err != nil {
		return Claim{}, "", err
	}
	if ok && previous.ExpiresAt.After(c.Clock.Now()) {
		return previous, Waiting, ErrClaimBusy
	}
	epoch := uint64(1)
	if ok {
		epoch = previous.Epoch + 1
	}
	claim = Claim{Key: key, Owner: owner, Epoch: epoch, ExpiresAt: c.Clock.Now().Add(ttl)}
	if err := c.writeJSON(c.claimPath(key), claim); err != nil {
		return Claim{}, "", err
	}
	if eventErr := c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Owner: owner, Epoch: epoch, Event: "claimed"}); eventErr != nil {
		return Claim{}, "", fmt.Errorf("record claim event: %w", eventErr)
	}
	return claim, Claimed, nil
}

func (c *Filesystem) Heartbeat(claim Claim, ttl time.Duration) (err error) {
	if ttl <= 0 {
		return fmt.Errorf("heartbeat TTL must be positive")
	}
	release, err := c.lock(context.Background(), claim.Key)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := release(); err == nil {
			err = closeErr
		}
	}()
	current, ok, err := c.readClaim(claim.Key)
	if err != nil {
		return err
	}
	if !ok || current.Owner != claim.Owner || current.Epoch != claim.Epoch || !current.ExpiresAt.After(c.Clock.Now()) {
		return ErrStaleClaim
	}
	current.ExpiresAt = c.Clock.Now().Add(ttl)
	if err := c.writeJSON(c.claimPath(claim.Key), current); err != nil {
		return err
	}
	if eventErr := c.event(Event{Time: c.Clock.Now(), Key: claim.Key.ID(), Owner: claim.Owner, Epoch: claim.Epoch, Event: "heartbeat"}); eventErr != nil {
		return fmt.Errorf("record heartbeat event: %w", eventErr)
	}
	return nil
}

// HeartbeatContext renews a lease until the context is cancelled. The
// interval is half the requested TTL, leaving time for one retry/takeover.
func (c *Filesystem) HeartbeatContext(ctx context.Context, claim Claim, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("heartbeat TTL must be positive")
	}
	interval := ttl / 2
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := c.Heartbeat(claim, ttl); err != nil {
				return err
			}
		}
	}
}

// Release relinquishes a live claim. It is idempotent after takeover.
func (c *Filesystem) Release(claim Claim) error { return c.abandon(claim) }

// Wait observes a committed artifact with bounded polling. It never treats a
// claim or notification as sufficient evidence of readiness.
func (c *Filesystem) Wait(ctx context.Context, key BuildKey, interval time.Duration) (Artifact, Outcome, error) {
	if err := key.validate(); err != nil {
		return Artifact{}, "", err
	}
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	for {
		artifact, ok, err := c.readArtifact(key)
		if err != nil {
			return Artifact{}, "", err
		}
		if ok {
			if err := c.verifyArtifact(artifact); err != nil {
				return Artifact{}, "", errors.Join(ErrUnsafeState, err)
			}
			if err := c.Recover(key); err != nil {
				return Artifact{}, "", err
			}
			return artifact, ExistingArtifact, nil
		}
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return Artifact{}, Waiting, errors.Join(ErrWaitTimeout, ctx.Err())
		case <-t.C:
		}
	}
}

func (c *Filesystem) Publish(claim Claim, artifact Artifact) (err error) {
	if err := c.verifyArtifact(artifact); err != nil {
		return ErrUnverifiedArtifact
	}
	release, err := c.lock(context.Background(), claim.Key)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := release(); err == nil {
			err = closeErr
		}
	}()
	if current, ok, err := c.readArtifact(claim.Key); err != nil {
		return err
	} else if ok {
		if err := c.verifyArtifact(current); err != nil {
			return errors.Join(ErrUnsafeState, err)
		}
		if err := c.recoverCommittedLocked(claim.Key, current); err != nil {
			return err
		}
		if current.Digest == artifact.Digest {
			return nil
		}
		eventErr := c.recordNondeterminism(claim.Key, current, artifact)
		return errors.Join(ErrDivergentArtifact, eventErr)
	}
	current, ok, err := c.readClaim(claim.Key)
	if err != nil {
		return err
	}
	if !ok || current.Owner != claim.Owner || current.Epoch != claim.Epoch || !current.ExpiresAt.After(c.Clock.Now()) {
		return ErrStaleClaim
	}
	record := commitRecord{Key: claim.Key, Claim: claim, Artifact: artifact, State: commitStatePrepared, UpdatedAt: c.Clock.Now()}
	if err := c.writeJSON(c.commitPath(claim.Key), record); err != nil {
		return err
	}
	if err := c.writeJSON(c.artifactPath(claim.Key), artifact); err != nil {
		return err
	}
	record.State = commitStateCommitted
	record.UpdatedAt = c.Clock.Now()
	if err := c.writeJSON(c.commitPath(claim.Key), record); err != nil {
		return err
	}
	if eventErr := c.event(Event{Time: c.Clock.Now(), Key: claim.Key.ID(), Owner: claim.Owner, Epoch: claim.Epoch, Event: "published", Detail: artifact.Digest}); eventErr != nil {
		return fmt.Errorf("record publication event: %w", eventErr)
	}
	record.State = commitStateAcknowledged
	record.UpdatedAt = c.Clock.Now()
	if err := c.writeJSON(c.commitPath(claim.Key), record); err != nil {
		return err
	}
	if eventErr := c.event(Event{Time: c.Clock.Now(), Key: claim.Key.ID(), Owner: claim.Owner, Epoch: claim.Epoch, Event: "published-ack", Detail: artifact.Digest}); eventErr != nil {
		return fmt.Errorf("record publication acknowledgment: %w", eventErr)
	}
	return os.Remove(c.claimPath(claim.Key))
}

// PublishIndependent records a valid builder result without taking a lease.
// It is used only when policy explicitly permits an independent fallback.
func (c *Filesystem) PublishIndependent(key BuildKey, artifact Artifact) (err error) {
	if err := key.validate(); err != nil {
		return err
	}
	if err := c.verifyArtifact(artifact); err != nil {
		return ErrUnverifiedArtifact
	}
	release, err := c.lock(context.Background(), key)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	current, ok, err := c.readArtifact(key)
	if err != nil {
		return err
	}
	if ok {
		if err := c.verifyArtifact(current); err != nil {
			return errors.Join(ErrUnsafeState, err)
		}
		if current.Digest == artifact.Digest {
			return nil
		}
		eventErr := c.recordNondeterminism(key, current, artifact)
		return errors.Join(ErrDivergentArtifact, eventErr)
	}
	record := commitRecord{Key: key, Claim: Claim{Key: key, Owner: "independent", Epoch: 1}, Artifact: artifact, State: commitStatePrepared, UpdatedAt: c.Clock.Now()}
	if err := c.writeJSON(c.commitPath(key), record); err != nil {
		return err
	}
	if err := c.writeJSON(c.artifactPath(key), artifact); err != nil {
		return err
	}
	record.State = commitStateCommitted
	record.UpdatedAt = c.Clock.Now()
	if err := c.writeJSON(c.commitPath(key), record); err != nil {
		return err
	}
	if err := c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Owner: "independent", Epoch: 1, Event: "published-independent", Detail: artifact.Digest}); err != nil {
		return err
	}
	record.State = commitStateAcknowledged
	record.UpdatedAt = c.Clock.Now()
	if err := c.writeJSON(c.commitPath(key), record); err != nil {
		return err
	}
	return c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Owner: "independent", Epoch: 1, Event: "published-ack", Detail: artifact.Digest})
}

// VerifyArtifactProof validates complete closure metadata. Trust is established
// by TrustVerifier's keyed artifacttrust policy, never by a caller hash.
func VerifyArtifactProof(artifact Artifact) error {
	if !isSHA256Digest(artifact.Digest) || !isSHA256Digest(artifact.ClosureDigest) || artifact.Provider == "" || artifact.ProviderAuthorization == "" {
		return ErrUnverifiedArtifact
	}
	return nil
}

func isSHA256Digest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (c *Filesystem) verifyArtifact(artifact Artifact) error {
	if !artifact.Verified || artifact.Digest == "" {
		return ErrUnverifiedArtifact
	}
	if !c.requireArtifactProof {
		return nil
	}
	if c.verifier == nil {
		return ErrUnverifiedArtifact
	}
	return c.verifier.VerifyArtifact(artifact)
}

func (c *Filesystem) Committed(key BuildKey) (Artifact, bool, error) {
	artifact, ok, err := c.readArtifact(key)
	if err != nil || !ok {
		return artifact, ok, err
	}
	if err := c.verifyArtifact(artifact); err != nil {
		return Artifact{}, false, errors.Join(ErrUnsafeState, err)
	}
	if err := c.Recover(key); err != nil {
		return Artifact{}, false, err
	}
	return artifact, true, nil
}
func (c *Filesystem) Events(key BuildKey) ([]Event, error) {
	b, err := os.ReadFile(filepath.Join(c.dir(key), "events.jsonl"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Recover completes an interrupted artifact transaction. It is safe to call
// after a process restart and is idempotent once the commit acknowledgment is
// durable.
func (c *Filesystem) Recover(key BuildKey) error {
	if err := key.validate(); err != nil {
		return err
	}
	release, err := c.lock(context.Background(), key)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	artifact, ok, err := c.readArtifact(key)
	if err != nil || !ok {
		return err
	}
	if err := c.verifyArtifact(artifact); err != nil {
		return err
	}
	return c.recoverCommittedLocked(key, artifact)
}

func (c *Filesystem) recoverCommittedLocked(key BuildKey, artifact Artifact) error {
	var record commitRecord
	recordOK, err := c.readJSON(c.commitPath(key), &record)
	if err != nil {
		return err
	}
	if recordOK {
		if record.Key != key || record.Artifact.Digest != artifact.Digest || record.Artifact.ClosureDigest != artifact.ClosureDigest || record.Artifact.Provider != artifact.Provider {
			return ErrUnsafeState
		}
		if record.State == commitStatePrepared {
			record.State = commitStateCommitted
		}
	}
	claim, claimOK, err := c.readClaim(key)
	if err != nil {
		return err
	}
	if claimOK {
		if recordOK && (claim.Owner != record.Claim.Owner || claim.Epoch != record.Claim.Epoch) {
			// A foreign live claim must not be removed merely because an old
			// artifact transaction exists. The artifact still wins on reads.
			return nil
		}
		if !recordOK {
			record = commitRecord{Key: key, Claim: claim, Artifact: artifact, State: commitStateCommitted, UpdatedAt: c.Clock.Now()}
			recordOK = true
		}
		if err := os.Remove(c.claimPath(key)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if !recordOK {
		return nil
	}
	record.Artifact = artifact
	record.State = commitStateAcknowledged
	record.UpdatedAt = c.Clock.Now()
	if err := c.writeJSON(c.commitPath(key), record); err != nil {
		return err
	}
	events, err := c.Events(key)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Event == "published-ack" && event.Epoch == record.Claim.Epoch && event.Detail == artifact.Digest {
			return nil
		}
	}
	return c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Owner: record.Claim.Owner, Epoch: record.Claim.Epoch, Event: "published-ack", Detail: artifact.Digest})
}

func (c *Filesystem) recordNondeterminism(key BuildKey, existing, candidate Artifact) error {
	policy := candidate.NondeterminismPolicy
	if policy == "" {
		policy = c.NondeterminismPolicy
	}
	if policy == "" {
		policy = "reject"
	}
	record := NondeterministicRecord{Time: c.Clock.Now(), Key: key.ID(), Existing: existing, Candidate: candidate, Policy: policy, PolicyOutcome: "rejected-conflicting-artifact"}
	if err := c.appendJSONLine(c.nondeterministicPath(key), record); err != nil {
		return err
	}
	existingCopy, candidateCopy := existing, candidate
	return c.event(Event{Time: record.Time, Key: key.ID(), Event: "nondeterministic", Detail: existing.Digest + " != " + candidate.Digest, PolicyOutcome: record.PolicyOutcome, ExistingArtifact: &existingCopy, CandidateArtifact: &candidateCopy})
}

func (c *Filesystem) Nondeterminism(key BuildKey) ([]NondeterministicRecord, error) {
	content, err := os.ReadFile(c.nondeterministicPath(key))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []NondeterministicRecord
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if line == "" {
			continue
		}
		var record NondeterministicRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (c *Filesystem) event(e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	dir := filepath.Join(c.Root, e.Key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// Persist the directory entry as well, so an acknowledged event survives a
	// crash even when the filesystem reorders metadata updates.
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err = d.Sync(); err == nil {
		err = d.Close()
	} else {
		_ = d.Close()
	}
	return err
}

func (c *Filesystem) appendJSONLine(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

type PrewarmRequest struct {
	Keys                 []BuildKey
	Capacity             int
	Priority             int
	Wait                 bool
	IndependentBuild     bool
	DiskReservationBytes int64
	Backoff              time.Duration
	LeaseTTL             time.Duration
	Owner                string
	Build                BuildRequest
	NondeterminismPolicy string
	Priorities           map[string]int
	LocalFallback        bool
	Generation           string
	PreviousGeneration   string
	PreviousReady        bool
}
type PrewarmItem struct {
	Key        BuildKey
	Status     PrewarmStatus
	Reason     string `json:"reason,omitempty"`
	Generation string `json:"generation,omitempty"`
}
type PrewarmStatus string

const (
	PrewarmNeeded          PrewarmStatus = "needed"
	PrewarmReady           PrewarmStatus = "ready"
	PrewarmCapacityLimited PrewarmStatus = "capacity-limited"
	PrewarmFailed          PrewarmStatus = "failed"
	PrewarmDegraded        PrewarmStatus = "degraded"
)

func (c *Filesystem) Prewarm(ctx context.Context, request PrewarmRequest, build func(context.Context, Claim) (Artifact, error)) ([]PrewarmItem, error) {
	if build == nil {
		return nil, fmt.Errorf("prewarm builder is required")
	}
	if request.Build.Network || request.Build.Credentials || request.Build.CPULimit != 0 || request.Build.MemoryBytes != 0 || request.Build.Timeout != 0 {
		return nil, ErrUnenforcedBuildPolicy
	}
	return c.prewarm(ctx, request, EnforcedBuilderFunc(func(ctx context.Context, claim Claim, _ ExecutionPolicy) (Artifact, error) {
		return build(ctx, claim)
	}))
}

// PrewarmWithExecutor runs prewarm through a typed policy-enforcing executor.
// This is the production path for resource, network, timeout, or credential
// policy; the executor receives the exact staging boundary and policy.
func (c *Filesystem) PrewarmWithExecutor(ctx context.Context, request PrewarmRequest, build EnforcedBuilder) ([]PrewarmItem, error) {
	if build == nil {
		return nil, fmt.Errorf("prewarm builder is required")
	}
	return c.prewarm(ctx, request, build)
}

func (c *Filesystem) prewarm(ctx context.Context, request PrewarmRequest, build EnforcedBuilder) ([]PrewarmItem, error) {
	if request.Capacity < 0 {
		return nil, fmt.Errorf("capacity cannot be negative")
	}
	if request.DiskReservationBytes < 0 {
		return nil, fmt.Errorf("disk reservation cannot be negative")
	}
	if request.Capacity == 0 {
		if len(request.Keys) == 0 {
			return []PrewarmItem{}, nil
		}
		items := make([]PrewarmItem, len(request.Keys))
		for i, key := range request.Keys {
			items[i] = PrewarmItem{Key: key, Status: c.capacityStatus(request), Reason: "capacity is zero", Generation: request.Generation}
		}
		return items, nil
	}
	if request.Backoff <= 0 {
		request.Backoff = 25 * time.Millisecond
	}
	if request.LeaseTTL <= 0 {
		request.LeaseTTL = time.Minute
	}
	if request.Owner == "" {
		request.Owner = "prewarm"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items := make([]PrewarmItem, len(request.Keys))
	for i, key := range request.Keys {
		items[i] = PrewarmItem{Key: key, Status: c.capacityStatus(request), Generation: request.Generation}
	}
	limit := min(len(request.Keys), request.Capacity)
	order := make([]int, len(request.Keys))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return request.priorityFor(request.Keys[order[i]]) > request.priorityFor(request.Keys[order[j]])
	})
	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	worker := func() {
		defer wg.Done()
		for i := range jobs {
			itemErr := c.prewarmOne(ctx, request, request.Keys[i], build)
			if itemErr == nil {
				items[i].Status = PrewarmReady
			} else if errors.Is(itemErr, ErrWaitTimeout) || errors.Is(itemErr, context.Canceled) {
				items[i].Status = PrewarmNeeded
			}
			if itemErr != nil {
				items[i].Reason = itemErr.Error()
				if request.PreviousReady && request.PreviousGeneration != "" {
					items[i].Status = PrewarmDegraded
				} else if !errors.Is(itemErr, ErrWaitTimeout) && !errors.Is(itemErr, context.Canceled) {
					items[i].Status = PrewarmFailed
				}
				errMu.Lock()
				firstErr = errors.Join(firstErr, itemErr)
				errMu.Unlock()
			}
		}
	}
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go worker()
	}
feed:
	for i := 0; i < limit; i++ {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- order[i]:
		}
	}
	close(jobs)
	wg.Wait()
	return items, firstErr
}

func (c *Filesystem) capacityStatus(request PrewarmRequest) PrewarmStatus {
	if request.PreviousReady && request.PreviousGeneration != "" {
		return PrewarmDegraded
	}
	return PrewarmCapacityLimited
}

func (r PrewarmRequest) priorityFor(key BuildKey) int {
	if r.Priorities != nil {
		if priority, ok := r.Priorities[key.ID()]; ok {
			return priority
		}
	}
	return r.Priority
}

func (c *Filesystem) runFallback(ctx context.Context, request PrewarmRequest, key BuildKey, build EnforcedBuilder, coordinatorErr error) (artifact Artifact, err error) {
	root := request.Build.Root
	if root == "" {
		root = c.Root
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fallbackRoot, mkdirErr := os.MkdirTemp("", "rcc-prewarm-fallback-")
		if mkdirErr != nil {
			return Artifact{}, mkdirErr
		}
		root = fallbackRoot
	}
	buildRequest := request.Build
	buildRequest.Root = root
	staging, cleanup, err := PrepareStaging(buildRequest, "fallback")
	if err != nil {
		return Artifact{}, err
	}
	defer func() { err = errors.Join(err, cleanup()) }()
	policy, err := buildRequest.ExecutionPolicy(staging)
	if err != nil {
		return Artifact{}, err
	}
	claim := Claim{Key: key, Owner: "local-fallback", Epoch: 1, Staging: staging}
	artifact, err = build.Build(ctx, claim, policy)
	if err != nil {
		return Artifact{}, fmt.Errorf("local fallback after coordinator failure (%v): %w", coordinatorErr, err)
	}
	if err := ValidateExecutionStaging(claim, policy); err != nil {
		return Artifact{}, err
	}
	if err := ValidateExecutionReceipt(artifact, policy); err != nil {
		return Artifact{}, err
	}
	if err := ValidateAuthoritativeCompletion(artifact); err != nil {
		return Artifact{}, fmt.Errorf("local fallback lacks authoritative lifecycle completion: %w", err)
	}
	verifier, ok := build.(AuthoritativeCompletionVerifier)
	if !ok {
		return Artifact{}, fmt.Errorf("local fallback executor cannot verify authoritative lifecycle completion: %w", ErrUnverifiedArtifact)
	}
	if err := verifier.VerifyCompletion(ctx, artifact); err != nil {
		return Artifact{}, fmt.Errorf("verify local fallback lifecycle completion: %w", err)
	}
	if artifact.Source == "" {
		artifact.Source = "local-fallback"
	}
	return artifact, nil
}

func (c *Filesystem) prewarmOne(ctx context.Context, request PrewarmRequest, key BuildKey, build EnforcedBuilder) error {
	if err := c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Event: "prewarm-requested", Detail: fmt.Sprintf("priority=%d generation=%s", request.priorityFor(key), request.Generation)}); err != nil {
		if request.LocalFallback {
			_, fallbackErr := c.runFallback(ctx, request, key, build, err)
			if fallbackErr == nil {
				return nil
			}
			return errors.Join(err, fallbackErr)
		}
		return err
	}
	claim, outcome, err := c.ClaimContext(ctx, key, request.Owner, request.LeaseTTL)
	if err != nil && !errors.Is(err, ErrClaimBusy) {
		if request.LocalFallback {
			_, fallbackErr := c.runFallback(ctx, request, key, build, err)
			if fallbackErr == nil {
				// A local fallback has already completed the real lifecycle
				// publication. It must remain functional even when the optional
				// coordinator's state root is unavailable.
				return nil
			}
			return errors.Join(err, fallbackErr)
		}
		return err
	}
	if outcome == ExistingArtifact {
		return nil
	}
	if outcome == Waiting {
		if request.IndependentBuild {
			policy, policyErr := request.Build.ExecutionPolicy(c.Root)
			if policyErr != nil {
				return policyErr
			}
			independent, buildErr := build.Build(ctx, Claim{Key: key, Owner: "independent"}, policy)
			if buildErr == nil {
				buildErr = c.PublishIndependent(key, independent)
			}
			if buildErr != nil {
				return buildErr
			}
			return nil
		}
		if !request.Wait {
			return ErrWaitTimeout
		}
		for {
			artifact, ok, readErr := c.readArtifact(key)
			if readErr != nil {
				return readErr
			}
			if ok && c.verifyArtifact(artifact) == nil {
				return nil
			}
			retry, retryOutcome, claimErr := c.ClaimContext(ctx, key, request.Owner, request.LeaseTTL)
			if claimErr != nil && !errors.Is(claimErr, ErrClaimBusy) {
				return claimErr
			}
			if retryOutcome == Claimed {
				claim = retry
				outcome = Claimed
				break
			}
			timer := time.NewTimer(request.Backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		if outcome != Claimed {
			return ErrWaitTimeout
		}
	}
	buildRequest := request.Build
	if buildRequest.Root == "" {
		buildRequest.Root = c.Root
	}
	if buildRequest.DiskBytes == 0 {
		buildRequest.DiskBytes = request.DiskReservationBytes
	}
	policy, policyErr := buildRequest.ExecutionPolicy(buildRequest.Root)
	if policyErr != nil {
		return errors.Join(policyErr, c.abandon(claim))
	}
	var buildCtx context.Context
	var stopHeartbeat context.CancelFunc
	if buildRequest.Timeout > 0 {
		buildCtx, stopHeartbeat = context.WithTimeout(ctx, buildRequest.Timeout)
	} else {
		buildCtx, stopHeartbeat = context.WithCancel(ctx)
	}
	staging, cleanup, stagingErr := PrepareStaging(buildRequest, request.Owner)
	if stagingErr != nil {
		stopHeartbeat()
		return errors.Join(stagingErr, c.abandon(claim))
	}
	claim.Staging = staging
	heartbeatErr := make(chan error, 1)
	go func() { heartbeatErr <- c.HeartbeatContext(buildCtx, claim, request.LeaseTTL) }()
	policy.Root = staging
	artifact, err := build.Build(buildCtx, claim, policy)
	stopHeartbeat()
	if err == nil {
		err = ValidateExecutionStaging(claim, policy)
	}
	if err == nil {
		err = ValidateExecutionReceipt(artifact, policy)
	}
	cleanupErr := error(nil)
	if err != nil && buildRequest.QuarantineRoot != "" {
		_, cleanupErr = QuarantineStaging(staging, buildRequest.QuarantineRoot, err.Error())
		if cleanupErr == nil {
			cleanupErr = nil
		}
	} else {
		cleanupErr = cleanup()
	}
	if err == nil && cleanupErr != nil {
		err = cleanupErr
	}
	hbErr := <-heartbeatErr
	if err == nil && !errors.Is(hbErr, context.Canceled) {
		err = hbErr
	}
	if err == nil {
		if artifact.NondeterminismPolicy == "" {
			artifact.NondeterminismPolicy = request.NondeterminismPolicy
		}
		if current, ok, readErr := c.readClaim(claim.Key); readErr != nil {
			err = readErr
		} else if !ok || current.Owner != claim.Owner || current.Epoch != claim.Epoch || !current.ExpiresAt.After(c.Clock.Now()) {
			err = ErrStaleClaim
		}
	}
	if err == nil {
		err = c.Publish(claim, artifact)
	}
	if err != nil {
		return errors.Join(err, c.abandon(claim))
	}
	return nil
}

func (c *Filesystem) abandon(claim Claim) (err error) {
	release, err := c.lock(context.Background(), claim.Key)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := release(); err == nil {
			err = closeErr
		}
	}()
	current, ok, err := c.readClaim(claim.Key)
	if err != nil || !ok {
		return err
	}
	if current.Owner != claim.Owner || current.Epoch != claim.Epoch {
		return ErrStaleClaim
	}
	return os.Remove(c.claimPath(claim.Key))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func (c *Filesystem) dir(key BuildKey) string        { return filepath.Join(c.Root, key.ID()) }
func (c *Filesystem) claimPath(k BuildKey) string    { return filepath.Join(c.dir(k), "claim.json") }
func (c *Filesystem) artifactPath(k BuildKey) string { return filepath.Join(c.dir(k), "artifact.json") }
func (c *Filesystem) commitPath(k BuildKey) string   { return filepath.Join(c.dir(k), "commit.json") }
func (c *Filesystem) nondeterministicPath(k BuildKey) string {
	return filepath.Join(c.dir(k), "nondeterministic.jsonl")
}
func (c *Filesystem) lockPath(k BuildKey) string { return filepath.Join(c.dir(k), "lock") }

type lockRecord struct {
	Owner      string    `json:"owner"`
	Token      string    `json:"token"`
	AcquiredAt time.Time `json:"acquiredAt"`
}

func (c *Filesystem) lock(ctx context.Context, key BuildKey) (func() error, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.dir(key), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(c.lockWait)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		f, err := os.OpenFile(c.lockPath(key), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			tokenBytes := make([]byte, 16)
			if _, randomErr := rand.Read(tokenBytes); randomErr != nil {
				_ = f.Close()
				_ = os.Remove(c.lockPath(key))
				return nil, randomErr
			}
			held := lockRecord{Owner: fmt.Sprint(os.Getpid()), Token: hex.EncodeToString(tokenBytes), AcquiredAt: c.Clock.Now()}
			record, marshalErr := json.Marshal(held)
			if marshalErr == nil {
				_, marshalErr = f.Write(record)
			}
			closeErr := f.Close()
			if marshalErr != nil {
				_ = os.Remove(c.lockPath(key))
				return nil, marshalErr
			}
			if closeErr != nil {
				_ = os.Remove(c.lockPath(key))
				return nil, closeErr
			}
			return func() error {
				current, readErr := c.readLock(key)
				if readErr != nil || current.Token != held.Token {
					return ErrStaleClaim
				}
				return os.Remove(c.lockPath(key))
			}, nil
		}
		if os.IsExist(err) {
			if info, statErr := os.Lstat(c.lockPath(key)); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, ErrUnsafeState
			}
			if record, readErr := c.readLock(key); readErr == nil {
				if record.AcquiredAt.Add(c.lockWait).Before(c.Clock.Now()) {
					if current, confirmErr := c.readLock(key); confirmErr == nil && current == record {
						_ = os.Remove(c.lockPath(key))
					}
					continue
				}
			}
		}
		if !os.IsExist(err) || time.Now().After(deadline) {
			return nil, fmt.Errorf("acquire build key lock: %w", err)
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Filesystem) readClaim(key BuildKey) (Claim, bool, error) {
	var v Claim
	ok, err := c.readJSON(c.claimPath(key), &v)
	if err == nil && ok && (v.Key != key || v.Owner == "" || v.Epoch == 0 || v.ExpiresAt.IsZero()) {
		return Claim{}, false, ErrUnsafeState
	}
	return v, ok, err
}
func (c *Filesystem) readArtifact(key BuildKey) (Artifact, bool, error) {
	var v Artifact
	ok, err := c.readJSON(c.artifactPath(key), &v)
	if err == nil && ok && (!v.Verified || v.Digest == "") {
		return Artifact{}, false, ErrUnsafeState
	}
	return v, ok, err
}
func (c *Filesystem) readLock(key BuildKey) (lockRecord, error) {
	var record lockRecord
	b, err := os.ReadFile(c.lockPath(key))
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(b, &record); err != nil || record.Owner == "" || record.AcquiredAt.IsZero() {
		return lockRecord{}, ErrUnsafeState
	}
	return record, nil
}
func (c *Filesystem) readJSON(path string, value any) (bool, error) {
	info, statErr := os.Lstat(path)
	if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return false, ErrUnsafeState
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return false, statErr
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(b, value)
}
func (c *Filesystem) writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := path + ".tmp-" + strings.ReplaceAll(fmt.Sprint(time.Now().UnixNano()), "-", "n")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}
