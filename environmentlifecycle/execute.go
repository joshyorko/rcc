package environmentlifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/environmentartifact"
)

type ExecutionHandle struct {
	ArtifactDigest    environmentartifact.Digest
	MaterializationID string
	LeaseID           string
	CWD               string
	Executable        string
	Environment       []string
	CacheHit          CacheProvenance
	Verification      artifacttrust.VerificationReceipt
}

type ChildResult struct {
	ExitCode int
}

func (it *LocalMaterializer) ExecutionHandle(ctx context.Context, lease Lease, command []string) (ExecutionHandle, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionHandle{}, err
	}
	if len(command) == 0 {
		return ExecutionHandle{}, fmt.Errorf("execution command is empty")
	}
	stored, err := readLease(lease.ArtifactDigest, lease.ID)
	if err != nil || !leasesEqual(stored, lease) {
		return ExecutionHandle{}, fmt.Errorf("lease is absent or changed")
	}
	if classifyLease(lease) != LeaseActive {
		return ExecutionHandle{}, fmt.Errorf("lease owner identity is no longer current")
	}
	record, err := readReadyRecord(lease.ArtifactDigest)
	if err != nil || record.MaterializationID != lease.MaterializationID {
		return ExecutionHandle{}, fmt.Errorf("lease materialization is not ready")
	}
	executable := command[0]
	base := strings.ToLower(filepath.Base(executable))
	if base == "python" || base == "python3" || base == "python.exe" {
		executable, err = materializedPython(record.Path)
		if err != nil {
			return ExecutionHandle{}, err
		}
	} else if !filepath.IsAbs(executable) {
		executable, err = exec.LookPath(executable)
		if err != nil {
			return ExecutionHandle{}, err
		}
	}
	return ExecutionHandle{
		ArtifactDigest: lease.ArtifactDigest, MaterializationID: lease.MaterializationID,
		LeaseID: lease.ID, CWD: record.Path, Executable: executable,
		Environment: conda.CondaExecutionEnvironment(record.Path, nil, true), CacheHit: CacheLocalMaterialization,
		Verification: lease.Verification,
	}, nil
}

func Execute(ctx context.Context, materializer Materializer, materialization Materialization, command []string) (handle ExecutionHandle, child ChildResult, err error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	return executeWithSignalsAndStreams(ctx, materializer, materialization, command, signals, nil, nil, nil)
}

// ExecuteInDirectory runs a source command with the materialized environment
// while keeping the process-scoped lease tied to that materialization.
func ExecuteInDirectory(ctx context.Context, materializer Materializer, materialization Materialization, command []string, workingDirectory string) (handle ExecutionHandle, child ChildResult, err error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	return executeWithSignalsAndStreamsInDirectory(ctx, materializer, materialization, command, signals, nil, nil, nil, workingDirectory)
}

func executeWithSignals(ctx context.Context, materializer Materializer, materialization Materialization, command []string, signals <-chan os.Signal) (handle ExecutionHandle, child ChildResult, err error) {
	return executeWithSignalsAndStreams(ctx, materializer, materialization, command, signals, nil, nil, nil)
}

// ExecuteWithStreams runs one child while preserving the supplied streams for its
// entire lifetime. The execution lease is released only after the child is reaped.
func ExecuteWithStreams(ctx context.Context, materializer Materializer, materialization Materialization, command []string, stdin io.Reader, stdout, stderr io.Writer) (handle ExecutionHandle, child ChildResult, err error) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	return executeWithSignalsAndStreams(ctx, materializer, materialization, command, signals, stdin, stdout, stderr)
}

func executeWithSignalsAndStreams(ctx context.Context, materializer Materializer, materialization Materialization, command []string, signals <-chan os.Signal, stdin io.Reader, stdout, stderr io.Writer) (handle ExecutionHandle, child ChildResult, err error) {
	return executeWithSignalsAndStreamsInDirectory(ctx, materializer, materialization, command, signals, stdin, stdout, stderr, "")
}

func executeWithSignalsAndStreamsInDirectory(ctx context.Context, materializer Materializer, materialization Materialization, command []string, signals <-chan os.Signal, stdin io.Reader, stdout, stderr io.Writer, workingDirectory string) (handle ExecutionHandle, child ChildResult, err error) {
	child.ExitCode = -1
	lease, err := materializer.Lease(ctx, materialization)
	if err != nil {
		return handle, child, err
	}
	defer func() {
		if releaseErr := materializer.Release(context.Background(), lease); err == nil && releaseErr != nil {
			err = fmt.Errorf("release execution lease: %w", releaseErr)
		}
	}()
	handle, err = materializer.ExecutionHandle(ctx, lease, command)
	if err != nil {
		return handle, child, err
	}
	if workingDirectory != "" {
		workingDirectory, err = filepath.Abs(workingDirectory)
		if err != nil {
			return handle, child, fmt.Errorf("resolve execution working directory: %w", err)
		}
		info, statErr := os.Lstat(workingDirectory)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return handle, child, fmt.Errorf("execution working directory is not a regular directory")
		}
		handle.CWD = workingDirectory
	}
	process := exec.CommandContext(ctx, handle.Executable, command[1:]...)
	process.Dir = handle.CWD
	process.Env = handle.Environment
	process.Stdin = stdin
	process.Stdout = stdout
	process.Stderr = stderr
	process.WaitDelay = 3 * time.Second
	supervisor, err := newExecutionSupervisor()
	if err != nil {
		return handle, child, err
	}
	defer func() {
		if closeErr := supervisor.close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close execution supervisor: %w", closeErr)
		}
	}()
	if preparer, ok := any(supervisor).(interface{ prepare(*exec.Cmd) error }); ok {
		if err = preparer.prepare(process); err != nil {
			return handle, child, fmt.Errorf("prepare execution supervisor: %w", err)
		}
	}
	if err = process.Start(); err != nil {
		return handle, child, err
	}
	if err = supervisor.attach(process.Process); err != nil {
		_ = process.Process.Kill()
		_ = process.Wait()
		return handle, child, err
	}
	waited := make(chan error, 1)
	go func() { waited <- process.Wait() }()
	for {
		select {
		case err = <-waited:
			return executionResult(handle, child, err, ctx.Err())
		case received := <-signals:
			if received != nil {
				if signalErr := process.Process.Signal(received); signalErr != nil && !errors.Is(signalErr, os.ErrProcessDone) {
					_ = supervisor.terminate(process.Process)
					<-waited
					return handle, child, fmt.Errorf("forward child signal: %w", signalErr)
				}
			}
		case <-ctx.Done():
			_ = supervisor.terminate(process.Process)
			<-waited
			return handle, child, ctx.Err()
		}
	}
}

func executionResult(handle ExecutionHandle, child ChildResult, err error, contextErr error) (ExecutionHandle, ChildResult, error) {
	if err == nil {
		child.ExitCode = 0
		return handle, child, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		child.ExitCode = exitError.ExitCode()
		if contextErr == nil {
			return handle, child, nil
		}
	}
	return handle, child, err
}
