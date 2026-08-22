package environmentlifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/joshyorko/rcc/common"
)

func TestLeaseAndExecutionHandleHaveExplicitTypedLifecycle(t *testing.T) {
	materialization := acquiredMaterialization(t)
	materializer := NewLocalMaterializer()

	lease, err := materializer.Lease(context.Background(), materialization)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ID == "" || lease.MaterializationID != materialization.ID || lease.ArtifactDigest != materialization.ArtifactDigest {
		t.Fatalf("lease = %+v", lease)
	}
	if lease.OwnerPID != os.Getpid() || lease.OwnerStart == "" || lease.CreatedAt.IsZero() {
		t.Fatalf("lease owner identity is incomplete: %+v", lease)
	}
	handle, err := materializer.ExecutionHandle(context.Background(), lease, []string{"python", "-V"})
	if err != nil {
		t.Fatal(err)
	}
	if handle.LeaseID != lease.ID || handle.MaterializationID != materialization.ID || handle.Executable == "" || handle.CWD != materialization.Path || len(handle.Environment) == 0 {
		t.Fatalf("execution handle = %+v", handle)
	}
	if err := materializer.Release(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if err := materializer.Release(context.Background(), lease); err != nil {
		t.Fatalf("idempotent release failed: %v", err)
	}
}

func TestLeaseFailsClosedWhenStrongOwnerIdentityIsUnavailable(t *testing.T) {
	materialization := acquiredMaterialization(t)
	previous := processIdentityLookup
	processIdentityLookup = func(int) (string, error) { return "", fmt.Errorf("identity unavailable") }
	t.Cleanup(func() { processIdentityLookup = previous })

	_, err := NewLocalMaterializer().Lease(context.Background(), materialization)
	if err == nil {
		t.Fatal("lease unexpectedly accepted missing strong owner identity")
	}
}

func TestLeaseFailsClosedWhenStrongOwnerIdentityIsAmbiguous(t *testing.T) {
	materialization := acquiredMaterialization(t)
	previous := processIdentityLookup
	processIdentityLookup = func(int) (string, error) { return "", nil }
	t.Cleanup(func() { processIdentityLookup = previous })

	_, err := NewLocalMaterializer().Lease(context.Background(), materialization)
	if err == nil {
		t.Fatal("lease unexpectedly accepted ambiguous strong owner identity")
	}
}

func TestExecuteRunsPythonAndReleasesProcessScopedLease(t *testing.T) {
	materialization := acquiredMaterialization(t)
	hostPython, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("host python3 is unavailable")
	}
	python := filepath.Join(materialization.Path, "python")
	wrapper := []byte(fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", hostPython))
	if err := os.WriteFile(python, wrapper, 0o750); err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(t.TempDir(), "python-ran")

	handle, child, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{
		"python", "-c", fmt.Sprintf("from pathlib import Path; Path(%q).write_text('ok')", proof),
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.ExitCode != 0 || handle.LeaseID == "" {
		t.Fatalf("execution result = handle %+v, child %+v", handle, child)
	}
	content, err := os.ReadFile(proof)
	if err != nil || string(content) != "ok" {
		t.Fatalf("Python proof = %q, %v", content, err)
	}
	if _, err := readLease(handle.ArtifactDigest, handle.LeaseID); !os.IsNotExist(err) {
		t.Fatalf("process-scoped lease survived execution: %v", err)
	}
}

func TestExecuteReturnsChildExitCodeAndStillReleasesLease(t *testing.T) {
	materialization := acquiredMaterialization(t)
	python := filepath.Join(materialization.Path, "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 17\n"), 0o750); err != nil {
		t.Fatal(err)
	}

	handle, child, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{"python"})
	if err != nil {
		t.Fatal(err)
	}
	if child.ExitCode != 17 {
		t.Fatalf("child exit code = %d", child.ExitCode)
	}
	if _, err := readLease(handle.ArtifactDigest, handle.LeaseID); !os.IsNotExist(err) {
		t.Fatalf("lease survived non-zero child exit: %v", err)
	}
}

func TestExecuteSpawnFailureStillReleasesLease(t *testing.T) {
	materialization := acquiredMaterialization(t)
	handle, _, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{"/definitely/not/an/executable"})
	if err == nil {
		t.Fatal("missing executable unexpectedly started")
	}
	if handle.LeaseID == "" {
		t.Fatal("spawn failure did not return the attempted execution handle")
	}
	if _, err := readLease(handle.ArtifactDigest, handle.LeaseID); !os.IsNotExist(err) {
		t.Fatalf("lease survived child spawn failure: %v", err)
	}
}

func TestExecuteForwardsTerminationSignalAndReleasesLease(t *testing.T) {
	materialization := acquiredMaterialization(t)
	python := filepath.Join(materialization.Path, "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\ntrap 'exit 23' TERM\n: > \"$1\"\nwhile :; do sleep 1; done\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "child-ready")
	signals := make(chan os.Signal, 1)
	type outcome struct {
		handle ExecutionHandle
		child  ChildResult
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		handle, child, err := executeWithSignals(context.Background(), NewLocalMaterializer(), materialization, []string{"python", ready}, signals)
		finished <- outcome{handle: handle, child: child, err: err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	signals <- syscall.SIGTERM
	result := <-finished
	if result.err != nil || result.child.ExitCode != 23 {
		t.Fatalf("forwarded signal result = handle %+v, child %+v, err %v", result.handle, result.child, result.err)
	}
	if _, err := readLease(result.handle.ArtifactDigest, result.handle.LeaseID); !os.IsNotExist(err) {
		t.Fatalf("lease survived forwarded termination: %v", err)
	}
}

func acquiredMaterialization(t *testing.T) Materialization {
	t.Helper()
	_, remote, artifactDigest := publishedFixture(t)
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})
	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: artifactDigest, Provider: remote})
	if err != nil {
		t.Fatal(err)
	}
	return Materialization{ArtifactDigest: result.ArtifactDigest, ID: result.MaterializationID, Path: result.Path, CacheHit: result.CacheHit}
}
