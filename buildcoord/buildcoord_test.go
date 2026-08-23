package buildcoord

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

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

func TestBuildKeyIncludesPolicyAndArtifactSchema(t *testing.T) {
	a:=testKey(); b:=a; b.ResolutionPolicy="offline-v2"; b.TrustPolicy="strict"; b.ArtifactSchema="v1"
	if a.ID()==b.ID(){t.Fatal("policy/schema changes must claim independently")}
}

func TestPrepareStagingRejectsCredentialsAndCleansUp(t *testing.T) {
	if _,_,err:=PrepareStaging(BuildRequest{Root:t.TempDir(),Credentials:true},"owner");err==nil{t.Fatal("credentials accepted")}
	d,cleanup,err:=PrepareStaging(BuildRequest{Root:t.TempDir(),Network:true},"owner");if err!=nil{t.Fatal(err)};if err:=cleanup();err!=nil{t.Fatal(err)}; _ = d
}

func TestClaimFailureMatrixCoreBoundaries(t *testing.T) {
	c:=NewFilesystem(t.TempDir(),RealClock{}); k:=testKey()
	if _,_,err:=c.Claim(BuildKey{},"x",time.Minute);err==nil{t.Fatal("empty key accepted")}
	cl,_,err:=c.Claim(k,"x",time.Minute);if err!=nil{t.Fatal(err)}
	if err:=c.Publish(cl,Artifact{Digest:"",Verified:true});!errors.Is(err,ErrUnverifiedArtifact){t.Fatalf("unverified=%v",err)}
	if _,_,err:=c.Claim(k,"y",time.Minute);!errors.Is(err,ErrClaimBusy){t.Fatalf("busy=%v",err)}
	if err:=c.Publish(cl,Artifact{Digest:"sha256:one",Verified:true});err!=nil{t.Fatal(err)}
	if err:=c.Publish(cl,Artifact{Digest:"sha256:two",Verified:true});!errors.Is(err,ErrDivergentArtifact){t.Fatalf("divergent=%v",err)}
	events,err:=c.Events(k);if err!=nil||len(events)<2{t.Fatalf("events=%v err=%v",events,err)}
}

func TestBuildKeyAndHeartbeatValidateInputs(t *testing.T) {
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
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
	c := NewFilesystem(t.TempDir(), clock)
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
	c := NewFilesystem(t.TempDir(), clock)
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
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
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
}

func TestPrewarmIsBoundedAndActionsNeutral(t *testing.T) {
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	items, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey(), {SpecificationDigest: "sha256:other", Platform: "linux_amd64", BuilderCompatibility: "v12"}}, Capacity: 1}, func(context.Context, Claim) (Artifact, error) {
		return Artifact{Digest: "sha256:prewarm", Verified: true}, nil
	})
	if err != nil || len(items) != 2 || items[1].Status != PrewarmCapacityLimited {
		t.Fatalf("prewarm: %#v %v", items, err)
	}
}

func TestPrewarmZeroCapacityReturnsWithoutBuilding(t *testing.T) {
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
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
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
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
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
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
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
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
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1}, func(context.Context, Claim) (Artifact, error) { t.Fatal("builder called"); return Artifact{}, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestFailedPrewarmBuilderRelinquishesClaimForRetry(t *testing.T) {
	c := NewFilesystem(t.TempDir(), &fakeClock{now: time.Unix(100, 0)})
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
	c := NewFilesystem(t.TempDir(), RealClock{})
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
		_, err := c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1}, build)
		firstDone <- err
	}()
	<-firstStarted
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	secondDone := make(chan error, 1)
	go func() {
		_, err := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{testKey()}, Capacity: 1}, build)
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
		c := NewFilesystem(os.Getenv("BUILDCOORD_ROOT"), RealClock{})
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
