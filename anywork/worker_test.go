package anywork_test

import (
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/joshyorko/rcc/anywork"
)

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
