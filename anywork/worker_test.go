package anywork_test

import (
	"context"
	"errors"
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
