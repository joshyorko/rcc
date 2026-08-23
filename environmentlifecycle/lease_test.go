package environmentlifecycle

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
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

func TestNewLeaseAndExecutionHandleRetainTrustDecision(t *testing.T) {
	materialization := acquiredMaterialization(t)
	materialization.Verification.DecisionID = "decision-1"
	materialization.Verification.RevocationSnapshot = "sha256:revocations"
	materializer := NewLocalMaterializer()
	lease, err := materializer.Lease(context.Background(), materialization)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = materializer.Release(context.Background(), lease) }()
	if lease.Verification.DecisionID != "decision-1" || lease.Verification.RevocationSnapshot != "sha256:revocations" || lease.Verification.LeaseID != lease.ID {
		t.Fatalf("lease verification=%+v", lease.Verification)
	}
	handle, err := materializer.ExecutionHandle(context.Background(), lease, []string{"python", "-V"})
	if err != nil {
		t.Fatal(err)
	}
	if handle.Verification.DecisionID != lease.Verification.DecisionID || handle.Verification.LeaseID != lease.ID {
		t.Fatalf("handle verification=%+v lease=%+v", handle.Verification, lease.Verification)
	}
}

func TestNewLeaseRejectsInvalidTrustDecision(t *testing.T) {
	materialization := acquiredMaterialization(t)
	materialization.Verification = artifacttrust.VerificationReceipt{
		ArtifactDigest: materialization.ArtifactDigest.String(), Code: artifacttrust.CodeRevoked,
	}
	if _, err := NewLocalMaterializer().Lease(context.Background(), materialization); err == nil {
		t.Fatal("invalid trust decision acquired a lease")
	}
}

func TestNewLeaseRefreshesRevocationsWithoutChangingRunningLease(t *testing.T) {
	_, remote, artifactDigest := publishedFixture(t)
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	now := time.Now().UTC()
	_, emptyRevocations, err := artifacttrust.NewRevocationBundleAt(artifactDigest.String(), nil, now, "offline-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := artifacttrust.PutAttachment(carrier, artifactDigest.String(), "revocations", emptyRevocations); err != nil {
		t.Fatal(err)
	}
	policy := &artifacttrust.Policy{
		Mode: artifacttrust.PermissiveLocal, AllowUnsignedLocal: true,
		FailClosedRevocations: true, RevocationMaxAge: 24 * time.Hour,
	}
	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: remote, TrustPolicy: policy, TrustCarrier: carrier,
	})
	if err != nil {
		t.Fatal(err)
	}
	materialization := Materialization{
		ArtifactDigest: result.ArtifactDigest, ID: result.MaterializationID, Path: result.Path,
		CacheHit: result.CacheHit, Verification: result.Verification,
		TrustPolicy: result.TrustPolicy, TrustCarrier: result.TrustCarrier,
	}
	materializer := NewLocalMaterializer()
	running, err := materializer.Lease(context.Background(), materialization)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = materializer.Release(context.Background(), running) }()

	updated := now.Add(time.Second)
	_, revokedBytes, err := artifacttrust.NewRevocationBundleAt(artifactDigest.String(), []artifacttrust.Revocation{{ArtifactDigests: []string{artifactDigest.String()}, UpdatedAt: artifacttrust.FreshTimestamp(updated)}}, updated, "offline-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := artifacttrust.PutAttachment(carrier, artifactDigest.String(), "revocations", revokedBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := materializer.Lease(context.Background(), materialization); err == nil {
		t.Fatal("new lease ignored refreshed revocation")
	}
	if _, err := materializer.ExecutionHandle(context.Background(), running, []string{"python", "-V"}); err != nil {
		t.Fatalf("already-running lease was re-evaluated: %v", err)
	}
}

func TestArtifactLifecycleLocksDoNotSerializeUnrelatedArtifacts(t *testing.T) {
	first, err := environmentartifact.ParseDigest("sha256:" + strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	second, err := environmentartifact.ParseDigest("sha256:" + strings.Repeat("2", 64))
	if err != nil {
		t.Fatal(err)
	}
	firstLock := artifactLock(first)
	firstLock.Lock()
	defer firstLock.Unlock()

	acquired := make(chan struct{})
	go func() {
		lock := artifactLock(second)
		lock.Lock()
		close(acquired)
		lock.Unlock()
	}()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("unrelated artifact lifecycle remained serialized")
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

func TestReconcileTreatsPIDReuseAsStale(t *testing.T) {
	previous := processIdentityLookup
	processIdentityLookup = func(int) (string, error) { return "new-start-token", nil }
	t.Cleanup(func() { processIdentityLookup = previous })
	lease := Lease{OwnerPID: 42, OwnerStart: "old-start-token"}
	if got := classifyLease(lease); got != LeaseStale {
		t.Fatalf("PID reuse status = %q", got)
	}
}

func TestExecuteRunsPythonAndReleasesProcessScopedLease(t *testing.T) {
	materialization := acquiredMaterialization(t)
	hostPython, err := hostPythonPath()
	if err != nil {
		t.Skip("host Python is unavailable")
	}
	if runtime.GOOS == "windows" {
		installWindowsTestPython(t, materialization.Path, hostPython)
	} else {
		python := filepath.Join(materialization.Path, "python")
		wrapper := []byte(fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", hostPython))
		if err := os.WriteFile(python, wrapper, 0o750); err != nil {
			t.Fatal(err)
		}
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
	command := []string{"python"}
	if runtime.GOOS == "windows" {
		hostPython, err := hostPythonPath()
		if err != nil {
			t.Skip("host Python is unavailable")
		}
		installWindowsTestPython(t, materialization.Path, hostPython)
		command = append(command, "-c", "import os; os._exit(17)")
	} else {
		python := filepath.Join(materialization.Path, "python")
		if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 17\n"), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	handle, child, err := Execute(context.Background(), NewLocalMaterializer(), materialization, command)
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
	missing := filepath.Join(filepath.VolumeName(materialization.Path)+string(os.PathSeparator), "definitely", "not", "an", "executable")
	handle, _, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{missing})
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
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal forwarding is not a Windows contract")
	}
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

func TestExecuteWithStreamsPreservesRequestResponseAndReleasesAfterReap(t *testing.T) {
	materialization := acquiredMaterialization(t)
	python := filepath.Join(materialization.Path, "python")
	ready := filepath.Join(t.TempDir(), "ready")
	if err := os.WriteFile(python, []byte("#!/bin/sh\n: > \"$1\"\nIFS= read line\nprintf 'reply:%s\\n' \"$line\"\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	stdin, input := io.Pipe()
	var stdout, stderr bytes.Buffer
	type result struct {
		handle ExecutionHandle
		child  ChildResult
		err    error
	}
	done := make(chan result, 1)
	go func() {
		handle, child, err := ExecuteWithStreams(context.Background(), NewLocalMaterializer(), materialization, []string{"python", ready}, stdin, &stdout, &stderr)
		done <- result{handle: handle, child: child, err: err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stream child never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case result := <-done:
		t.Fatal("stream child exited before request", result.err)
	default:
	}
	leases, err := filepath.Glob(filepath.Join(common.Product.Home(), "artifacts", "v1", "materializations", materialization.ArtifactDigest.Hex(), "leases", "*.json"))
	if err != nil || len(leases) == 0 {
		t.Fatalf("stream lease was not observable during child lifetime: %v", err)
	}
	if _, err := os.Stat(leases[0]); err != nil {
		t.Fatalf("stream lease disappeared before child reap: %v", err)
	}
	_, _ = input.Write([]byte("request\n"))
	_ = input.Close()
	outcome := <-done
	handle := outcome.handle
	child := outcome.child
	err = outcome.err
	if err != nil || child.ExitCode != 0 {
		t.Fatalf("stream execution = handle %+v child %+v err %v", handle, child, err)
	}
	if stdout.String() != "reply:request\n" || stderr.Len() != 0 {
		t.Fatalf("stream output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if _, err := readLease(handle.ArtifactDigest, handle.LeaseID); !os.IsNotExist(err) {
		t.Fatalf("lease survived stream child reap: %v", err)
	}
}

func hostPythonPath() (string, error) {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func installWindowsTestPython(t *testing.T, root, source string) {
	t.Helper()
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "python.exe"), content, 0o750); err != nil {
		t.Fatal(err)
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
