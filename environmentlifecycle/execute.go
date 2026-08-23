package environmentlifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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
	if err != nil || stored != lease {
		return ExecutionHandle{}, fmt.Errorf("lease is absent or changed")
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
	return executeWithSignals(ctx, materializer, materialization, command, signals)
}

func executeWithSignals(ctx context.Context, materializer Materializer, materialization Materialization, command []string, signals <-chan os.Signal) (handle ExecutionHandle, child ChildResult, err error) {
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
	process := exec.CommandContext(ctx, handle.Executable, command[1:]...)
	process.Dir = handle.CWD
	process.Env = handle.Environment
	if err = process.Start(); err != nil {
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
					_ = process.Process.Kill()
					<-waited
					return handle, child, fmt.Errorf("forward child signal: %w", signalErr)
				}
			}
		case <-ctx.Done():
			_ = process.Process.Kill()
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
