package buildcoord

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
)

type fakeClock struct{ now time.Time }

type authoritativeFallbackBuilder struct{ artifact Artifact }

func (b authoritativeFallbackBuilder) Build(context.Context, Claim, ExecutionPolicy) (Artifact, error) {
	return b.artifact, nil
}

func (b authoritativeFallbackBuilder) VerifyCompletion(context.Context, Artifact) error {
	return ValidateAuthoritativeCompletion(b.artifact)
}

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func testKey() BuildKey {
	return BuildKey{SpecificationDigest: "sha256:spec", Platform: "linux_amd64", BuilderCompatibility: "v12-gzip-sha256"}
}

func TestBuildKeyIsDeterministicAndIncludesCompatibility(t *testing.T) {
	key := testKey()
	if key.ID() != (BuildKey{SpecificationDigest: "sha256:spec", Platform: "linux_amd64", BuilderCompatibility: "v12-gzip-sha256"}).ID() {
		t.Fatal("equivalent build keys differ")
	}
	changed := key
	changed.BuilderCompatibility = "other"
	if changed.ID() == key.ID() {
		t.Fatal("builder compatibility does not fence build key")
	}
}

func TestVerifierConstructorRequiresTrustBoundary(t *testing.T) {
	if _, err := NewFilesystemWithVerifier(t.TempDir(), nil, nil); err == nil {
		t.Fatal("nil verifier accepted")
	}
}

func TestVerifierConstructorRejectsUnkeyedVerifier(t *testing.T) {
	if _, err := NewFilesystemWithVerifier(t.TempDir(), nil, artifactVerifierFunc(func(Artifact) error { return nil })); err == nil {
		t.Fatal("unkeyed verifier accepted")
	}
}

func TestTrustVerifierBindsClosureAndProviderToKeyedSignature(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	artifact := Artifact{Digest: "sha256:" + strings.Repeat("a", 64), Verified: true, ClosureDigest: "sha256:" + strings.Repeat("b", 64), Provider: "provider-a", ProviderAuthorization: "authorization-a"}
	signature, err := artifacttrust.Sign(ArtifactTrustDigest(artifact), "build-key", private)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Signatures = []artifacttrust.Signature{signature}
	verifier, err := NewTrustVerifier(artifacttrust.Policy{RequireSignature: true, AcceptedKeys: []string{"build-key"}, AcceptedPlatforms: []string{"linux_amd64"}, AcceptedBuilders: []string{"builder-v1"}}, map[string]ed25519.PublicKey{"build-key": public}, nil, false, "linux_amd64", "builder-v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyArtifact(artifact); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
	artifact.Provider = "provider-b"
	if err := verifier.VerifyArtifact(artifact); err == nil {
		t.Fatal("provider mutation accepted by digest-only signature")
	}
}

func TestFilesystemCopiesKeyedTrustConfiguration(t *testing.T) {
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewTrustVerifier(artifacttrust.Policy{RequireSignature: true, AcceptedKeys: []string{"build-key"}}, map[string]ed25519.PublicKey{"build-key": public}, nil, false, "linux_amd64", "builder-v1")
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewFilesystem(t.TempDir(), nil, verifier)
	if err != nil {
		t.Fatal(err)
	}
	verifier.Keys["build-key"][0] ^= 0xff
	if coordinator.verifier.(*TrustVerifier).Keys["build-key"][0] == verifier.Keys["build-key"][0] {
		t.Fatal("coordinator trust keys alias caller configuration")
	}
}

func TestBuildKeyIncludesPolicyAndArtifactSchema(t *testing.T) {
	a := testKey()
	b := a
	b.ResolutionPolicy = "offline-v2"
	b.TrustPolicy = "strict"
	b.ArtifactSchema = "v1"
	if a.ID() == b.ID() {
		t.Fatal("policy/schema changes must claim independently")
	}
}

func TestReleaseAndWaitAreStableLeaseOperations(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	key := testKey()
	claim, outcome, err := c.Claim(key, "owner", time.Minute)
	if err != nil || outcome != Claimed {
		t.Fatalf("claim: %v %v", outcome, err)
	}
	if err := c.Release(claim); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, _, err := c.Wait(ctx, key, 5*time.Millisecond); err == nil {
		t.Fatal("wait unexpectedly succeeded without artifact")
	}
}

func TestArtifactProofIsRequiredWhenConfigured(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	c.requireArtifactProof = true
	c.verifier = artifactVerifierFunc(func(a Artifact) error {
		if a.ProviderAuthorization == "" {
			return errors.New("missing authorization")
		}
		return nil
	})
	claim, _, err := c.Claim(testKey(), "owner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(claim, Artifact{Digest: "sha256:one", Verified: true}); !errors.Is(err, ErrUnverifiedArtifact) {
		t.Fatalf("proof bypass: %v", err)
	}
	closure := "sha256:" + strings.Repeat("a", 64)
	proof := Artifact{Digest: "sha256:one", Verified: true, ClosureDigest: closure, Provider: "local", ProviderAuthorization: "opaque-provider-proof"}
	if err := c.Publish(claim, proof); err != nil {
		t.Fatalf("proof publish: %v", err)
	}
}

type artifactVerifierFunc func(Artifact) error

func (f artifactVerifierFunc) VerifyArtifact(a Artifact) error { return f(a) }

func TestPrepareStagingRejectsCredentialsAndCleansUp(t *testing.T) {
	if _, _, err := PrepareStaging(BuildRequest{Root: t.TempDir(), Credentials: true}, "owner"); err == nil {
		t.Fatal("credentials accepted")
	}
	d, cleanup, err := PrepareStaging(BuildRequest{Root: t.TempDir(), Network: true}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	_ = d
}

func TestReserveDiskChecksAndReleasesCapacity(t *testing.T) {
	r := t.TempDir()
	reservation, err := ReserveDisk(r, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Release(); err != nil {
		t.Fatal(err)
	}
	if err := reservation.Release(); err != nil {
		t.Fatal("release must be idempotent: ", err)
	}
}

func TestClaimFailureMatrixCoreBoundaries(t *testing.T) {
	c := newFilesystem(t.TempDir(), RealClock{})
	k := testKey()
	if _, _, err := c.Claim(BuildKey{}, "x", time.Minute); err == nil {
		t.Fatal("empty key accepted")
	}
	cl, _, err := c.Claim(k, "x", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(cl, Artifact{Digest: "", Verified: true}); !errors.Is(err, ErrUnverifiedArtifact) {
		t.Fatalf("unverified=%v", err)
	}
	if _, _, err := c.Claim(k, "y", time.Minute); !errors.Is(err, ErrClaimBusy) {
		t.Fatalf("busy=%v", err)
	}
	if err := c.Publish(cl, Artifact{Digest: "sha256:one", Verified: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(cl, Artifact{Digest: "sha256:two", Verified: true}); !errors.Is(err, ErrDivergentArtifact) {
		t.Fatalf("divergent=%v", err)
	}
	events, err := c.Events(k)
	if err != nil || len(events) < 2 {
		t.Fatalf("events=%v err=%v", events, err)
	}
}

func TestBuildKeyAndHeartbeatValidateInputs(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	if _, _, err := c.Claim(BuildKey{}, "owner", time.Minute); err == nil {
		t.Fatal("empty build key accepted")
	}
	claim, _, err := c.Claim(testKey(), "owner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Heartbeat(claim, 0); err == nil {
		t.Fatal("non-positive heartbeat TTL accepted")
	}
}

func TestFilesystemCoordinatorRecoversOnlyExpiredLock(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	c := newFilesystem(t.TempDir(), clock)
	key := testKey()
	if err := os.MkdirAll(c.dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.lockPath(key), []byte(`{"owner":"dead","acquiredAt":"1970-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, outcome, err := c.Claim(key, "new", time.Minute); err != nil || outcome != Claimed {
		t.Fatalf("expired lock: %v %v", outcome, err)
	}
	if err := os.Remove(c.claimPath(key)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.lockPath(key), []byte(`{"owner":"live","acquiredAt":"2100-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Claim(key, "blocked", time.Minute); err == nil {
		t.Fatal("live lock stolen")
	}
}

func TestFilesystemCoordinatorFencesTakeoverAndRejectsStalePublish(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	c := newFilesystem(t.TempDir(), clock)
	key := testKey()
	first, outcome, err := c.Claim(key, "first", time.Minute)
	if err != nil || outcome != Claimed || first.Epoch != 1 {
		t.Fatalf("first claim: %#v %v %v", first, outcome, err)
	}
	clock.Advance(2 * time.Minute)
	second, outcome, err := c.Claim(key, "second", time.Minute)
	if err != nil || outcome != Claimed || second.Epoch != 2 {
		t.Fatalf("takeover: %#v %v %v", second, outcome, err)
	}
	if err := c.Publish(first, Artifact{Digest: "sha256:first", Verified: true}); !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("stale publish: %v", err)
	}
	if err := c.Publish(second, Artifact{Digest: "sha256:second", Verified: true}); err != nil {
		t.Fatal(err)
	}
	artifact, ok, err := c.Committed(key)
	if err != nil || !ok || artifact.Digest != "sha256:second" {
		t.Fatalf("committed: %#v %v %v", artifact, ok, err)
	}
}

func TestCommittedArtifactWinsAndDivergenceIsVisible(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	key := testKey()
	claim, _, err := c.Claim(key, "builder", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(claim, Artifact{Digest: "sha256:one", Verified: true}); err != nil {
		t.Fatal(err)
	}
	next, outcome, err := c.Claim(key, "other", time.Minute)
	if err != nil || outcome != ExistingArtifact || next.Artifact.Digest != "sha256:one" {
		t.Fatalf("existing artifact: %#v %v %v", next, outcome, err)
	}
	claim2 := Claim{Key: key, Owner: "other", Epoch: claim.Epoch + 1}
	if err := c.Publish(claim2, Artifact{Digest: "sha256:two", Verified: true}); !errors.Is(err, ErrDivergentArtifact) {
		t.Fatalf("divergence: %v", err)
	}
	events, err := c.Events(key)
	if err != nil || events[len(events)-1].Event != "nondeterministic" || !strings.Contains(events[len(events)-1].Detail, "sha256:one") || !strings.Contains(events[len(events)-1].Detail, "sha256:two") {
		t.Fatalf("nondeterminism receipt: %#v %v", events, err)
	}
}

func TestCommittedArtifactRecoveryAcknowledgesClaimTransaction(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	c := newFilesystem(t.TempDir(), clock)
	key := testKey()
	claim, _, err := c.Claim(key, "builder", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	artifact := Artifact{Digest: "sha256:one", Verified: true}
	if err := c.writeJSON(c.artifactPath(key), artifact); err != nil {
		t.Fatal(err)
	}
	if err := c.writeJSON(c.commitPath(key), commitRecord{Key: key, Claim: claim, Artifact: artifact, State: commitStateCommitted}); err != nil {
		t.Fatal(err)
	}
	if _, outcome, err := c.Claim(key, "recovery", time.Minute); err != nil || outcome != ExistingArtifact {
		t.Fatalf("recovery claim: %v %v", outcome, err)
	}
	if _, err := os.Stat(c.claimPath(key)); !os.IsNotExist(err) {
		t.Fatalf("claim was not acknowledged: %v", err)
	}
	events, err := c.Events(key)
	if err != nil {
		t.Fatal(err)
	}
	foundAck := false
	for _, event := range events {
		if event.Event == "published-ack" {
			foundAck = true
		}
	}
	if !foundAck {
		t.Fatalf("recovery acknowledgment missing: %#v", events)
	}
}

func TestNondeterminismPersistsBothArtifactsAndPolicyOutcome(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	key := testKey()
	claim, _, err := c.Claim(key, "builder", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(claim, Artifact{Digest: "sha256:first", Verified: true, NondeterminismPolicy: "reject"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(Claim{Key: key, Owner: "other", Epoch: claim.Epoch + 1}, Artifact{Digest: "sha256:second", Verified: true, NondeterminismPolicy: "reject"}); !errors.Is(err, ErrDivergentArtifact) {
		t.Fatalf("divergent publish: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(c.dir(key), "nondeterministic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var record NondeterministicRecord
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(string(content)), "\n")[0]), &record); err != nil {
		t.Fatal(err)
	}
	if record.Existing.Digest != "sha256:first" || record.Candidate.Digest != "sha256:second" || record.PolicyOutcome == "" {
		t.Fatalf("incomplete nondeterminism record: %#v", record)
	}
}

func TestPrewarmRejectsUnenforcedResourcePolicy(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	_, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1, Build: BuildRequest{CPULimit: 1, MemoryBytes: 1024, Network: true}}, func(context.Context, Claim) (Artifact, error) {
		return Artifact{Digest: "sha256:unsafe", Verified: true}, nil
	})
	if !errors.Is(err, ErrUnenforcedBuildPolicy) {
		t.Fatalf("unenforced resource policy accepted: %v", err)
	}
}

func TestPrewarmWithExecutorReceivesEnforcedPolicyAtStagingBoundary(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	var received ExecutionPolicy
	items, err := c.PrewarmWithExecutor(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1, Build: BuildRequest{CPULimit: 2, MemoryBytes: 4096, Network: true, Timeout: time.Second}}, EnforcedBuilderFunc(func(_ context.Context, _ Claim, policy ExecutionPolicy) (Artifact, error) {
		received = policy
		return Artifact{Digest: "sha256:executor", Verified: true, Execution: &ExecutionReceipt{StagingRoot: policy.Root, PolicyDigest: policy.Digest(), CPULimit: policy.CPULimit, MemoryBytes: policy.MemoryBytes, Timeout: policy.Timeout, NetworkIsolated: false, CredentialsExcluded: true}}, nil
	}))
	if err != nil || len(items) != 1 || items[0].Status != PrewarmReady {
		t.Fatalf("typed prewarm: %#v %v", items, err)
	}
	if received.Root == "" || received.CPULimit != 2 || received.MemoryBytes != 4096 || !received.Network || received.Timeout != time.Second {
		t.Fatalf("executor policy: %#v", received)
	}
	if _, err := os.Stat(filepath.Join(received.Root, "policy.json")); !os.IsNotExist(err) {
		t.Fatalf("staging was not cleaned after build: %v", err)
	}
}

func TestPrewarmUsesLocalFallbackWhenCoordinatorUnavailable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "coordinator-file")
	if err := os.WriteFile(root, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newFilesystem(root, &fakeClock{now: time.Unix(100, 0)})
	items, err := c.PrewarmWithExecutor(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1, LocalFallback: true}, authoritativeFallbackBuilder{artifact: Artifact{Digest: "sha256:fallback", Verified: true, Provider: "local", Completion: &CompletionReceipt{ArtifactDigest: "sha256:fallback", Provider: "local", ManifestCommitted: true, ObjectsVerified: true, Lifecycle: "fixture"}}})
	if err != nil || len(items) != 1 || items[0].Status != PrewarmReady {
		t.Fatalf("fallback prewarm: %#v %v", items, err)
	}
}

func TestPrewarmRetainsReadyPreviousGenerationWhenNextLacksCapacity(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	items, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 0, Generation: "n-plus-one", PreviousGeneration: "n", PreviousReady: true}, func(context.Context, Claim) (Artifact, error) {
		t.Fatal("builder called while capacity is zero")
		return Artifact{}, nil
	})
	if err != nil || len(items) != 1 || items[0].Status != PrewarmDegraded || items[0].Generation != "n-plus-one" {
		t.Fatalf("rolling generation result: %#v %v", items, err)
	}
}

func TestPrewarmHonorsPerKeyPriorityOrder(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	low := testKey()
	high := low
	high.SpecificationDigest = "sha256:high"
	var first string
	items, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{low, high}, Capacity: 1, Priorities: map[string]int{high.ID(): 10, low.ID(): 1}}, func(_ context.Context, claim Claim) (Artifact, error) {
		if first == "" {
			first = claim.Key.SpecificationDigest
		}
		return Artifact{Digest: "sha256:priority", Verified: true}, nil
	})
	if err != nil || len(items) != 2 {
		t.Fatalf("priority prewarm: %#v %v", items, err)
	}
	if first != high.SpecificationDigest {
		t.Fatalf("first prewarm key = %q, want %q", first, high.SpecificationDigest)
	}
}

func TestCommandExecutorEnforcesRuntimePolicyAndProvesStagingUse(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process boundary is Linux-backed")
	}
	root := t.TempDir()
	request := BuildRequest{Root: root, Network: false, CPULimit: 2, MemoryBytes: 64 << 20, Timeout: time.Second}
	staging, cleanup, err := PrepareStaging(request, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	policy, err := request.ExecutionPolicy(staging)
	if err != nil {
		t.Fatal(err)
	}
	claim := Claim{Key: testKey(), Owner: "owner", Epoch: 1, Staging: staging}
	content := `{"digest":"sha256:` + strings.Repeat("a", 64) + `","verified":true,"closureDigest":"sha256:` + strings.Repeat("b", 64) + `","provider":"fixture","providerAuthorization":"fixture-auth","completion":{"artifactDigest":"sha256:` + strings.Repeat("a", 64) + `","provider":"fixture","manifestCommitted":true,"objectsVerified":true,"lifecycle":"fixture"}}`
	executor, err := NewCommandExecutor([]string{"/bin/sh", "-c", fmt.Sprintf("printf '%%s' %s", shellQuote(content))})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := executor.Build(context.Background(), claim, policy)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Execution == nil || artifact.Execution.StagingRoot != staging || artifact.Execution.CPULimit != 2 || artifact.Execution.MemoryBytes != 64<<20 || !artifact.Execution.NetworkIsolated || !artifact.Execution.CredentialsExcluded {
		t.Fatalf("execution receipt: %#v", artifact.Execution)
	}
}

func TestCommandExecutorRejectsStagingMismatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process boundary is Linux-backed")
	}
	root := t.TempDir()
	policy, err := (BuildRequest{Root: root}).ExecutionPolicy(root)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewCommandExecutor([]string{"/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Build(context.Background(), Claim{Key: testKey(), Owner: "owner", Epoch: 1, Staging: filepath.Join(root, "other")}, policy); !errors.Is(err, ErrStagingBoundary) {
		t.Fatalf("staging mismatch: %v", err)
	}
}

func TestCommandExecutorHonorsTimeout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process boundary is Linux-backed")
	}
	root := t.TempDir()
	request := BuildRequest{Root: root, Timeout: 20 * time.Millisecond}
	staging, cleanup, err := PrepareStaging(request, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	policy, err := request.ExecutionPolicy(staging)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewCommandExecutor([]string{"/bin/sh", "-c", "sleep 1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Build(context.Background(), Claim{Key: testKey(), Owner: "owner", Epoch: 1, Staging: staging}, policy)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}

func TestCommandExecutorRejectsCredentialEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime process boundary is Linux-backed")
	}
	root := t.TempDir()
	staging, cleanup, err := PrepareStaging(BuildRequest{Root: root}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	policy, err := (BuildRequest{Root: root}).ExecutionPolicy(staging)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewCommandExecutor([]string{"/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	executor.Environment = []string{"AWS_SECRET_ACCESS_KEY=leak"}
	if _, err := executor.Build(context.Background(), Claim{Key: testKey(), Owner: "owner", Epoch: 1, Staging: staging}, policy); err == nil {
		t.Fatal("credential-bearing environment accepted")
	}
}

func TestLocalFallbackRejectsNonAuthoritativeArtifact(t *testing.T) {
	root := filepath.Join(t.TempDir(), "coordinator-file")
	if err := os.WriteFile(root, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newFilesystem(root, &fakeClock{now: time.Unix(100, 0)})
	_, err := c.PrewarmWithExecutor(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1, LocalFallback: true}, EnforcedBuilderFunc(func(_ context.Context, _ Claim, _ ExecutionPolicy) (Artifact, error) {
		return Artifact{Digest: "sha256:" + strings.Repeat("a", 64), Verified: true}, nil
	}))
	if !errors.Is(err, ErrUnverifiedArtifact) {
		t.Fatalf("non-authoritative fallback accepted: %v", err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\"+`"'"`+"'") + "'"
}

func TestPrewarmIsBoundedAndActionsNeutral(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	items, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey(), {SpecificationDigest: "sha256:other", Platform: "linux_amd64", BuilderCompatibility: "v12"}}, Capacity: 1}, func(context.Context, Claim) (Artifact, error) {
		return Artifact{Digest: "sha256:prewarm", Verified: true}, nil
	})
	if err != nil || len(items) != 2 || items[1].Status != PrewarmCapacityLimited {
		t.Fatalf("prewarm: %#v %v", items, err)
	}
}

func TestPrewarmZeroCapacityReturnsWithoutBuilding(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	items, err := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 0}, func(context.Context, Claim) (Artifact, error) {
		t.Fatal("builder called with zero capacity")
		return Artifact{}, nil
	})
	if err != nil || len(items) != 1 || items[0].Status != PrewarmCapacityLimited {
		t.Fatalf("zero-capacity prewarm: %#v %v", items, err)
	}
}

func TestObsoleteLockReleaseDoesNotRemoveSuccessorLock(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	firstRelease, err := c.lock(context.Background(), testKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(c.lockPath(testKey())); err != nil {
		t.Fatal(err)
	}
	secondRelease, err := c.lock(context.Background(), testKey())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondRelease() }()
	if err := firstRelease(); !errors.Is(err, ErrStaleClaim) {
		t.Fatalf("obsolete release: %v", err)
	}
	if _, err := os.Lstat(c.lockPath(testKey())); err != nil {
		t.Fatalf("successor lock was removed: %v", err)
	}
}

func TestPersistedCoordinatorStateFailsClosed(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	key := testKey()
	if err := os.MkdirAll(c.dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.claimPath(key), []byte(`{"key":{"specificationDigest":"sha256:other","platform":"linux_amd64","builderCompatibility":"v12"},"owner":"x","epoch":1,"expiresAt":"2100-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Claim(key, "owner", time.Minute); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("mismatched persisted claim: %v", err)
	}
}

func TestPrewarmBuildsOnceAndWaitersReuseCommittedArtifact(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	var builds atomic.Int32
	build := func(ctx context.Context, claim Claim) (Artifact, error) {
		builds.Add(1)
		time.Sleep(20 * time.Millisecond)
		return Artifact{Digest: "sha256:built", Verified: true}, nil
	}
	ctx := context.Background()
	results := make(chan []PrewarmItem, 2)
	for i := 0; i < 2; i++ {
		go func() {
			items, _ := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 2}, build)
			results <- items
		}()
	}
	for i := 0; i < 2; i++ {
		<-results
	}
	if builds.Load() != 1 {
		t.Fatalf("expected one build, got %d", builds.Load())
	}
}

func TestPrewarmHonorsCancellation(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1}, func(context.Context, Claim) (Artifact, error) { t.Fatal("builder called"); return Artifact{}, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestFailedPrewarmBuilderRelinquishesClaimForRetry(t *testing.T) {
	c := newFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	buildFailure := errors.New("build failed")
	_, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1}, func(context.Context, Claim) (Artifact, error) {
		return Artifact{}, buildFailure
	})
	if !errors.Is(err, buildFailure) {
		t.Fatalf("first build: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	items, err := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1}, func(context.Context, Claim) (Artifact, error) {
		return Artifact{Digest: "sha256:retry", Verified: true}, nil
	})
	if err != nil || len(items) != 1 || items[0].Status != PrewarmReady {
		t.Fatalf("retry build: %#v %v", items, err)
	}
}

func TestWaitingPrewarmTakesOverAfterBuilderFailure(t *testing.T) {
	c := newFilesystem(t.TempDir(), RealClock{})
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var attempts atomic.Int32
	build := func(context.Context, Claim) (Artifact, error) {
		if attempts.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			return Artifact{}, errors.New("first builder failed")
		}
		return Artifact{Digest: "sha256:takeover", Verified: true}, nil
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1, Wait: true}, build)
		firstDone <- err
	}()
	<-firstStarted
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	secondDone := make(chan error, 1)
	go func() {
		_, err := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1, Wait: true}, build)
		secondDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(releaseFirst)
	if err := <-firstDone; err == nil {
		t.Fatal("first builder unexpectedly succeeded")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("waiting builder did not take over: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("build attempts = %d", attempts.Load())
	}
}

func TestHelperProcessClaimRace(t *testing.T) {
	if os.Getenv("BUILDCOORD_HELPER") == "1" {
		c := newFilesystem(os.Getenv("BUILDCOORD_ROOT"), RealClock{})
		_, outcome, err := c.Claim(testKey(), os.Getenv("BUILDCOORD_OWNER"), time.Minute)
		if err != nil {
			os.Exit(2)
		}
		if outcome != Claimed {
			os.Exit(3)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	cmds := make([]*exec.Cmd, 2)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run", "^TestHelperProcessClaimRace$")
		cmds[i].Env = append(os.Environ(), "BUILDCOORD_HELPER=1", "BUILDCOORD_ROOT="+filepath.Clean(root), "BUILDCOORD_OWNER="+string(rune('a'+i)))
	}
	for _, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	passed := 0
	for _, cmd := range cmds {
		if err := cmd.Wait(); err == nil {
			passed++
		}
	}
	if passed != 1 {
		t.Fatalf("expected one race winner, got %d", passed)
	}
}
