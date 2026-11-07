package buildcoord

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise real OS contention, not a simulated claim or a timeout increase.
func TestPrewarmWaitingForKeyLockHonorsContext(t *testing.T) {
	c := newFilesystem(t.TempDir(), RealClock{})
	c.lockWait = 5 * time.Millisecond
	key := testKey()
	release, err := c.lock(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	var builds atomic.Int32
	_, err = c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{key}, Capacity: 1, Wait: true, Backoff: time.Millisecond}, func(context.Context, Claim) (Artifact, error) {
		builds.Add(1)
		return Artifact{Digest: "sha256:must-not-build", Verified: true}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting lock contender returned %v instead of its context deadline", err)
	}
	if builds.Load() != 0 {
		t.Fatal("built without owning a claim")
	}
}

func TestPrewarmRetriesReleasedKeyLockWithoutDuplicateBuild(t *testing.T) {
	c := newFilesystem(t.TempDir(), RealClock{})
	c.lockWait = 5 * time.Millisecond
	key := testKey()
	release, err := c.lock(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			_ = release()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var builds atomic.Int32
	result := make(chan error, 1)
	go func() {
		_, e := c.Prewarm(ctx, PrewarmRequest{Keys: []BuildKey{key}, Capacity: 1, Wait: true, Backoff: time.Millisecond}, func(context.Context, Claim) (Artifact, error) {
			builds.Add(1)
			return Artifact{Digest: "sha256:one-build", Verified: true}, nil
		})
		result <- e
	}()
	select {
	case err := <-result:
		t.Fatalf("returned before key lock was released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	held = false
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 1 {
		t.Fatalf("build count %d, want one", builds.Load())
	}
}

func TestPrewarmNonWaitingKeyLockRemainsBounded(t *testing.T) {
	c := newFilesystem(t.TempDir(), RealClock{})
	c.lockWait = 5 * time.Millisecond
	key := testKey()
	release, err := c.lock(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	_, err = c.Prewarm(context.Background(), PrewarmRequest{Keys: []BuildKey{key}, Capacity: 1}, func(context.Context, Claim) (Artifact, error) {
		t.Error("built without owning a claim")
		return Artifact{}, nil
	})
	if err == nil {
		t.Fatal("non-waiting lock contention must fail closed")
	}
}
