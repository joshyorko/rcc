package anywork_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshyorko/rcc/anywork"
)

func TestAutoScaleHonorsWorkerCountAfterPoolPreinitialization(t *testing.T) {
	const subprocessKey = "RCC_ANYWORK_WORKER_LIMIT_SUBPROCESS"
	if os.Getenv(subprocessKey) == "1" {
		anywork.WorkerCount = 2
		anywork.AutoScale()
		if got := anywork.Scale(); got != 2 {
			t.Fatalf("worker count = %d, want 2", got)
		}

		var completed atomic.Int32
		anywork.Backlog(func() { completed.Add(1) })
		if err := anywork.Sync(); err != nil {
			t.Fatalf("Sync returned error: %v", err)
		}
		if got := completed.Load(); got != 1 {
			t.Fatalf("completed work count = %d, want 1", got)
		}
		t.Log("worker limit and task dispatch passed")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.v", "-test.run=^TestAutoScaleHonorsWorkerCountAfterPoolPreinitialization$")
	cmd.Env = append(os.Environ(), subprocessKey+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worker limit subprocess failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "worker limit and task dispatch passed") {
		t.Fatalf("worker limit subprocess did not run the verification: %s", output)
	}
}

func cleanupAnywork(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := anywork.Sync(); err != nil {
			t.Errorf("cleanup Sync returned unexpected error: %v", err)
		}
	})
}

func waitForWorkerSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func TestAutoScaleGrowsToConfiguredWorkerCount(t *testing.T) {
	cleanupAnywork(t)
	previous := anywork.WorkerCount
	anywork.WorkerCount = 3
	anywork.AutoScale()
	anywork.WorkerCount = previous
	if got := anywork.Scale(); got != 3 {
		t.Fatalf("Scale() after AutoScale with WorkerCount=3 = %d, want 3", got)
	}
}

func TestScaleMatchesConcurrentWorkerCapacity(t *testing.T) {
	cleanupAnywork(t)
	workers := int(anywork.Scale())
	if workers < 2 {
		t.Fatalf("Scale() = %d, want at least the initialized two workers", workers)
	}
	started := make(chan struct{}, workers)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	var active, maximum atomic.Int32

	for range workers {
		anywork.Backlog(func() {
			current := active.Add(1)
			for observed := maximum.Load(); current > observed; observed = maximum.Load() {
				if maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			active.Add(-1)
		})
	}
	for range workers {
		waitForWorkerSignal(t, started, "each worker to start one held task")
	}
	if got := maximum.Load(); got != int32(workers) {
		t.Fatalf("simultaneously active workers = %d, Scale() reports %d", got, workers)
	}
	close(release)
	released = true
	if err := anywork.Sync(); err != nil {
		t.Fatalf("Sync returned error after releasing all workers: %v", err)
	}
}

func TestSyncDrainsTasksQueuedWhilePriorWorkIsHeld(t *testing.T) {
	cleanupAnywork(t)
	firstStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	firstReleased := false
	defer func() {
		if !firstReleased {
			close(releaseFirst)
		}
	}()
	anywork.Backlog(func() {
		firstStarted <- struct{}{}
		<-releaseFirst
	})
	waitForWorkerSignal(t, firstStarted, "initial work to start")

	syncStarted := make(chan struct{})
	syncDone := make(chan error, 1)
	go func() {
		// This signals goroutine launch, not registration inside Sync's wait.
		close(syncStarted)
		syncDone <- anywork.Sync()
	}()
	waitForWorkerSignal(t, syncStarted, "Sync caller to start")

	secondStarted := make(chan struct{}, 1)
	releaseSecond := make(chan struct{})
	secondReleased := false
	defer func() {
		if !secondReleased {
			close(releaseSecond)
		}
	}()
	anywork.Backlog(func() {
		secondStarted <- struct{}{}
		<-releaseSecond
	})
	close(releaseFirst)
	firstReleased = true
	waitForWorkerSignal(t, secondStarted, "work queued during Sync to start")
	select {
	case err := <-syncDone:
		t.Fatalf("Sync returned before work queued during its wait was released: %v", err)
	default:
	}
	close(releaseSecond)
	secondReleased = true
	select {
	case err := <-syncDone:
		if err != nil {
			t.Fatalf("Sync returned error after all queued work completed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Sync did not return after all queued work completed")
	}
}

func TestBacklogIgnoresNilAndSyncWaitsForQueuedWork(t *testing.T) {
	cleanupAnywork(t)
	var completed atomic.Int32
	anywork.Backlog(func() { completed.Add(1) })
	anywork.Backlog(nil)

	if err := anywork.Sync(); err != nil {
		t.Fatalf("Sync returned unexpected error: %v", err)
	}
	if got := completed.Load(); got != 1 {
		t.Fatalf("completed work count = %d, want 1", got)
	}
}

func TestSyncReportsPanickingWorkAsFailure(t *testing.T) {
	cleanupAnywork(t)
	anywork.Backlog(func() { panic("expected test failure") })

	err := anywork.Sync()
	if err == nil || !strings.Contains(err.Error(), "1 failures") {
		t.Fatalf("Sync error = %v, want one failure", err)
	}
}

func TestWorkerPoolSurvivesPanicsFromEveryWorker(t *testing.T) {
	const subprocessKey = "RCC_ANYWORK_ALL_WORKER_PANIC_SUBPROCESS"
	if os.Getenv(subprocessKey) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.v", "-test.run=^TestWorkerPoolSurvivesPanicsFromEveryWorker$")
		cmd.Env = append(os.Environ(), subprocessKey+"=1")
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("all-worker panic recovery subprocess exceeded its deadline: %v\n%s", ctx.Err(), output)
		}
		if err != nil {
			t.Fatalf("all-worker panic recovery subprocess failed: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "all-worker panic recovery passed") {
			t.Fatalf("subprocess did not run the all-worker recovery oracle: %s", output)
		}
		return
	}

	cleanupAnywork(t)
	workers := int(anywork.Scale())
	if workers < 2 {
		t.Fatalf("Scale() = %d, want at least two workers", workers)
	}
	panicsStarted := make(chan struct{}, workers)
	releasePanics := make(chan struct{})
	panicsReleased := false
	defer func() {
		if !panicsReleased {
			close(releasePanics)
		}
	}()
	for range workers {
		anywork.Backlog(func() {
			panicsStarted <- struct{}{}
			<-releasePanics
			panic("expected all-worker recovery")
		})
	}
	for range workers {
		waitForWorkerSignal(t, panicsStarted, "every worker to hold a panic task")
	}
	close(releasePanics)
	panicsReleased = true
	wantFailures := fmt.Sprintf("There has been %d failures. See messages above.", workers)
	if err := anywork.Sync(); err == nil || err.Error() != wantFailures {
		t.Fatalf("Sync error = %v, want %d recovered failures", err, workers)
	}

	goodStarted := make(chan struct{}, workers)
	releaseGood := make(chan struct{})
	goodReleased := false
	defer func() {
		if !goodReleased {
			close(releaseGood)
		}
	}()
	var completed atomic.Int32
	for range workers {
		anywork.Backlog(func() {
			goodStarted <- struct{}{}
			<-releaseGood
			completed.Add(1)
		})
	}
	for range workers {
		waitForWorkerSignal(t, goodStarted, "every worker to start a held recovery task")
	}
	close(releaseGood)
	goodReleased = true
	if err := anywork.Sync(); err != nil {
		t.Fatalf("Sync returned stale failure after all workers completed normal work: %v", err)
	}
	if got := completed.Load(); got != int32(workers) {
		t.Fatalf("normal recovery tasks completed = %d, want %d", got, workers)
	}
	t.Log("all-worker panic recovery passed")
}

type recordingCloser struct{ closed bool }

func (it *recordingCloser) Close() error {
	it.closed = true
	return nil
}

func TestOnErrPanicCloseAllClosesNonNilClosersAndPanics(t *testing.T) {
	cleanupAnywork(t)
	first, second := &recordingCloser{}, &recordingCloser{}
	defer func() {
		recovered, ok := recover().(error)
		if !ok || !errors.Is(recovered, io.ErrUnexpectedEOF) {
			t.Fatalf("panic = %v, want %v", recovered, io.ErrUnexpectedEOF)
		}
		if !first.closed || !second.closed {
			t.Fatalf("closers closed = (%v, %v), want both true", first.closed, second.closed)
		}
	}()

	anywork.OnErrPanicCloseAll(io.ErrUnexpectedEOF, first, nil, second)
}
