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
}

func (v TrustVerifier) VerifyArtifact(a Artifact) error {
	if err := VerifyArtifactProof(a); err != nil {
		return err
	}
	return v.Policy.Evaluate(v.Local, a.Digest, v.Platform, v.Builder, a.Signatures, v.Revocations, v.Keys)
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
}
type Event struct {
	Time   time.Time `json:"time"`
	Key    string    `json:"key"`
	Owner  string    `json:"owner,omitempty"`
	Epoch  uint64    `json:"epoch,omitempty"`
	Event  string    `json:"event"`
	Detail string    `json:"detail,omitempty"`
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
		os.RemoveAll(d)
		return "", nil, err
	}
	if req.CPULimit < 0 || req.MemoryBytes < 0 || req.Timeout < 0 {
		_ = reservation.Release()
		os.RemoveAll(d)
		return "", nil, fmt.Errorf("resource limits cannot be negative")
	}
	policy := map[string]any{"network": req.Network, "credentials": false, "diskBytes": req.DiskBytes, "cpuLimit": req.CPULimit, "memoryBytes": req.MemoryBytes, "timeout": req.Timeout.String()}
	pb, _ := json.Marshal(policy)
	if e := os.WriteFile(filepath.Join(d, "policy.json"), pb, 0600); e != nil {
		_ = reservation.Release()
		os.RemoveAll(d)
		return "", nil, e
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
	RequireArtifactProof bool
	Verifier             ArtifactVerifier
	NondeterminismPolicy string
}

func NewFilesystem(root string, clock Clock) *Filesystem {
	if clock == nil {
		clock = RealClock{}
	}
	return &Filesystem{Root: root, Clock: clock, lockWait: 2 * time.Second}
}

// NewFilesystemWithVerifier constructs a coordinator with mandatory
// trust-backed publication verification.
func NewFilesystemWithVerifier(root string, clock Clock, verifier ArtifactVerifier) (*Filesystem, error) {
	if verifier == nil {
		return nil, fmt.Errorf("artifact verifier is required")
	}
	c := NewFilesystem(root, clock)
	c.RequireArtifactProof = true
	c.Verifier = verifier
	return c, nil
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
	if !artifact.Verified || artifact.Digest == "" || (c.RequireArtifactProof && (c.Verifier == nil || c.Verifier.VerifyArtifact(artifact) != nil)) {
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
		if current.Digest == artifact.Digest {
			if persisted, claimOK, claimErr := c.readClaim(claim.Key); claimErr == nil && claimOK && persisted.Owner == claim.Owner && persisted.Epoch == claim.Epoch {
				if eventErr := c.event(Event{Time: c.Clock.Now(), Key: claim.Key.ID(), Owner: claim.Owner, Epoch: claim.Epoch, Event: "published-ack", Detail: artifact.Digest}); eventErr != nil {
					return fmt.Errorf("record publication acknowledgment: %w", eventErr)
				}
				if removeErr := os.Remove(c.claimPath(claim.Key)); removeErr != nil && !os.IsNotExist(removeErr) {
					return removeErr
				}
			}
			return nil
		}
		eventErr := c.event(Event{Time: c.Clock.Now(), Key: claim.Key.ID(), Owner: claim.Owner, Epoch: claim.Epoch, Event: "nondeterministic", Detail: current.Digest + " != " + artifact.Digest + " policy=" + artifact.NondeterminismPolicy})
		return errors.Join(ErrDivergentArtifact, eventErr)
	}
	current, ok, err := c.readClaim(claim.Key)
	if err != nil {
		return err
	}
	if !ok || current.Owner != claim.Owner || current.Epoch != claim.Epoch || !current.ExpiresAt.After(c.Clock.Now()) {
		return ErrStaleClaim
	}
	if err := c.writeJSON(c.artifactPath(claim.Key), artifact); err != nil {
		return err
	}
	if eventErr := c.event(Event{Time: c.Clock.Now(), Key: claim.Key.ID(), Owner: claim.Owner, Epoch: claim.Epoch, Event: "published", Detail: artifact.Digest}); eventErr != nil {
		return fmt.Errorf("record publication event: %w", eventErr)
	}
	return os.Remove(c.claimPath(claim.Key))
}

// PublishIndependent records a valid builder result without taking a lease.
// It is used only when policy explicitly permits an independent fallback.
func (c *Filesystem) PublishIndependent(key BuildKey, artifact Artifact) error {
	if err := key.validate(); err != nil {
		return err
	}
	if !artifact.Verified || artifact.Digest == "" || (c.RequireArtifactProof && (c.Verifier == nil || c.Verifier.VerifyArtifact(artifact) != nil)) {
		return ErrUnverifiedArtifact
	}
	release, err := c.lock(context.Background(), key)
	if err != nil {
		return err
	}
	defer release()
	current, ok, err := c.readArtifact(key)
	if err != nil {
		return err
	}
	if ok {
		if current.Digest == artifact.Digest {
			return nil
		}
		eventErr := c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Event: "nondeterministic", Detail: current.Digest + " != " + artifact.Digest + " policy=" + artifact.NondeterminismPolicy})
		return errors.Join(ErrDivergentArtifact, eventErr)
	}
	if err := c.writeJSON(c.artifactPath(key), artifact); err != nil {
		return err
	}
	return c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Event: "published-independent", Detail: artifact.Digest})
}

// VerifyArtifactProof validates complete closure metadata. Trust is established
// by TrustVerifier's keyed artifacttrust policy, never by a caller hash.
func VerifyArtifactProof(artifact Artifact) error {
	if artifact.ClosureDigest == "" || artifact.Provider == "" {
		return ErrUnverifiedArtifact
	}
	return nil
}

func (c *Filesystem) Committed(key BuildKey) (Artifact, bool, error) { return c.readArtifact(key) }
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
}
type PrewarmItem struct {
	Key    BuildKey
	Status PrewarmStatus
	Reason string `json:"reason,omitempty"`
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
	if request.Capacity < 0 {
		return nil, fmt.Errorf("capacity cannot be negative")
	}
	if request.DiskReservationBytes < 0 {
		return nil, fmt.Errorf("disk reservation cannot be negative")
	}
	if build == nil {
		return nil, fmt.Errorf("prewarm builder is required")
	}
	if request.Capacity == 0 {
		if len(request.Keys) == 0 {
			return []PrewarmItem{}, nil
		}
		items := make([]PrewarmItem, len(request.Keys))
		for i, key := range request.Keys {
			items[i] = PrewarmItem{Key: key, Status: PrewarmCapacityLimited, Reason: "capacity is zero"}
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
		items[i] = PrewarmItem{Key: key, Status: PrewarmCapacityLimited}
	}
	limit := min(len(request.Keys), request.Capacity)
	order := make([]int, len(request.Keys))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return request.Priorities[request.Keys[order[i]].ID()] > request.Priorities[request.Keys[order[j]].ID()]
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
				if !errors.Is(itemErr, ErrWaitTimeout) && !errors.Is(itemErr, context.Canceled) {
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

func (c *Filesystem) prewarmOne(ctx context.Context, request PrewarmRequest, key BuildKey, build func(context.Context, Claim) (Artifact, error)) error {
	if err := c.event(Event{Time: c.Clock.Now(), Key: key.ID(), Event: "prewarm-requested", Detail: fmt.Sprintf("priority=%d", request.Priority)}); err != nil {
		return err
	}
	claim, outcome, err := c.ClaimContext(ctx, key, request.Owner, request.LeaseTTL)
	if err != nil && !errors.Is(err, ErrClaimBusy) {
		return err
	}
	if outcome == ExistingArtifact {
		return nil
	}
	if outcome == Waiting {
		if request.IndependentBuild {
			independent, buildErr := build(ctx, Claim{Key: key, Owner: "independent"})
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
			if ok && artifact.Verified {
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
	artifact, err := build(buildCtx, claim)
	stopHeartbeat()
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
func (c *Filesystem) lockPath(k BuildKey) string     { return filepath.Join(c.dir(k), "lock") }

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
	return nil
}
