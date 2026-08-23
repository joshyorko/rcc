package buildcoord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const maxExecutorOutput = 4 << 20

// CommandExecutor is RCC's concrete child-process boundary for prewarming. It
// runs a command in the claimed staging root, applies Linux namespace and
// rlimit controls, strips inherited credentials, and accepts only a strict
// Artifact JSON result on stdout.
type CommandExecutor struct {
	Command        []string
	Environment    []string
	ReadOnlyInputs []string
}

func NewCommandExecutor(command []string) (*CommandExecutor, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, fmt.Errorf("build command is required")
	}
	return &CommandExecutor{Command: append([]string(nil), command...)}, nil
}

func (it *CommandExecutor) Build(ctx context.Context, claim Claim, policy ExecutionPolicy) (Artifact, error) {
	if it == nil || len(it.Command) == 0 {
		return Artifact{}, fmt.Errorf("build command is required")
	}
	if policy.Credentials {
		return Artifact{}, ErrUnenforcedBuildPolicy
	}
	if err := ValidateExecutionStaging(claim, policy); err != nil {
		return Artifact{}, err
	}
	if runtime.GOOS != "linux" && policy.RequiresBoundary() {
		return Artifact{}, fmt.Errorf("%w: concrete resource/network boundary is Linux-only", ErrUnenforcedBuildPolicy)
	}
	env, err := executorEnvironment(policy.Root, it.Environment)
	if err != nil {
		return Artifact{}, err
	}
	runner, networkIsolated, confined, err := constrainedCommand(it.Command, policy, it.ReadOnlyInputs)
	if err != nil {
		return Artifact{}, err
	}
	runCtx := ctx
	stop := func() {}
	if policy.Timeout > 0 {
		runCtx, stop = context.WithTimeout(ctx, policy.Timeout)
	}
	defer stop()
	command := exec.CommandContext(runCtx, runner[0], runner[1:]...)
	command.Dir = policy.Root
	command.Env = env
	var output boundedBuffer
	var diagnostics boundedBuffer
	command.Stdout = &output
	command.Stderr = &diagnostics
	var statusReader, statusWriter, blockReader, blockWriter *os.File
	var statusStart <-chan statusStartResult
	var statusDone <-chan error
	parentNamespace := uint64(0)
	confinementPID := 0
	mountNamespace := uint64(0)
	var validateErr error
	if confined {
		statusReader, statusWriter, err = os.Pipe()
		if err != nil {
			return Artifact{}, fmt.Errorf("create parent-owned confinement status pipe: %w", err)
		}
		blockReader, blockWriter, err = os.Pipe()
		if err != nil {
			_ = statusReader.Close()
			_ = statusWriter.Close()
			return Artifact{}, fmt.Errorf("create parent-owned sandbox gate: %w", err)
		}
		defer func() { _ = statusReader.Close() }()
		defer func() { _ = statusWriter.Close() }()
		defer func() { _ = blockReader.Close() }()
		defer func() { _ = blockWriter.Close() }()
		statusStart, statusDone = readBwrapStatus(statusReader)
		command.ExtraFiles = []*os.File{statusWriter, blockReader}
	}
	processID := 0
	if err := command.Start(); err != nil {
		return Artifact{}, fmt.Errorf("start staged build: %w", err)
	}
	processID = command.Process.Pid
	if confined {
		// The parent writer is closed immediately after bwrap has inherited its
		// descriptor. The payload only sees bwrap's closed status descriptor.
		_ = statusWriter.Close()
		select {
		case result := <-statusStart:
			if result.err != nil {
				_ = command.Process.Kill()
				_ = command.Wait()
				return Artifact{}, result.err
			}
			parentNamespace, validateErr = validateConfinementStart(result.status, processID)
			if validateErr != nil {
				_ = command.Process.Kill()
				_ = command.Wait()
				return Artifact{}, validateErr
			}
			confinementPID = result.status.ChildPID
			mountNamespace = result.status.MountNamespace
			// bwrap's --block-fd holds the sandbox payload until this parent
			// validation has completed.
			_ = blockWriter.Close()
		case <-runCtx.Done():
			_ = command.Process.Kill()
			_ = command.Wait()
			return Artifact{}, runCtx.Err()
		}
	}
	err = command.Wait()
	if runCtx.Err() != nil {
		return Artifact{}, runCtx.Err()
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("staged build failed: %w: %s", err, diagnostics.String())
	}
	if err := finishBwrapStatus(statusDone, confined); err != nil {
		return Artifact{}, err
	}
	var artifact Artifact
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, fmt.Errorf("decode staged artifact: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Artifact{}, fmt.Errorf("staged build returned multiple artifact records")
	}
	artifact.Execution = &ExecutionReceipt{
		StagingRoot: policy.Root, PolicyDigest: policy.Digest(), ProcessID: processID,
		ConfinementPID: confinementPID, MountNamespace: mountNamespace,
		ParentMountNamespace: parentNamespace,
		CPULimit:             policy.CPULimit, MemoryBytes: policy.MemoryBytes, Timeout: policy.Timeout,
		NetworkIsolated: networkIsolated, CredentialsExcluded: true, FilesystemRestricted: confined,
	}
	return artifact, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxExecutorOutput {
		return 0, io.ErrShortWrite
	}
	return b.Buffer.Write(p)
}

func executorEnvironment(root string, requested []string) ([]string, error) {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + root, "TMPDIR=" + root, "RCC_BUILD_STAGING_ROOT=" + root}
	for _, raw := range requested {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || !safeEnvironmentName(parts[0]) {
			return nil, fmt.Errorf("invalid or credential-bearing build environment entry")
		}
		env = append(env, raw)
	}
	return env, nil
}

func safeEnvironmentName(name string) bool {
	if name == "" || strings.ContainsAny(name, "\r\n=") {
		return false
	}
	lower := strings.ToLower(name)
	for _, fragment := range []string{"secret", "token", "password", "credential", "private_key", "access_key"} {
		if strings.Contains(lower, fragment) {
			return false
		}
	}
	return true
}

func constrainedCommand(command []string, policy ExecutionPolicy, readOnlyInputs []string) ([]string, bool, bool, error) {
	runner := append([]string(nil), command...)
	script := `set -eu
exec "$@"
`
	runner = append([]string{"/bin/sh", "-c", script, "rcc-staged-build"}, runner...)
	if runtime.GOOS != "linux" {
		if policy.RequiresBoundary() || !policy.Network {
			return nil, false, false, fmt.Errorf("%w: restricted filesystem boundary is Linux-only", ErrUnenforcedBuildPolicy)
		}
		return runner, false, false, nil
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, false, false, fmt.Errorf("%w: bwrap is unavailable", ErrUnenforcedBuildPolicy)
	}
	if policy.CPULimit > 0 || policy.MemoryBytes > 0 {
		prlimit, err := exec.LookPath("prlimit")
		if err != nil {
			return nil, false, false, fmt.Errorf("%w: prlimit is unavailable", ErrUnenforcedBuildPolicy)
		}
		args := []string{}
		if policy.CPULimit > 0 {
			args = append(args, "--cpu="+strconv.Itoa(policy.CPULimit))
		}
		if policy.MemoryBytes > 0 {
			args = append(args, "--as="+strconv.FormatInt(policy.MemoryBytes, 10))
		}
		runner = append([]string{prlimit}, append(args, append([]string{"--"}, runner...)...)...)
	}
	mounts := []string{
		"--unshare-user-try",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", mustRealPath(policy.Root), mustRealPath(policy.Root),
		"--chdir", mustRealPath(policy.Root),
		"--json-status-fd", "3",
		"--block-fd", "4",
	}
	for _, systemPath := range []string{"/usr/bin", "/usr/lib", "/bin", "/lib", "/lib64"} {
		if _, statErr := os.Lstat(systemPath); statErr != nil {
			return nil, false, false, fmt.Errorf("%w: required runtime path %q is unavailable", ErrUnenforcedBuildPolicy, systemPath)
		}
		mounts = append(mounts, "--ro-bind", systemPath, systemPath)
	}
	for _, systemPath := range []string{"/usr/lib64", "/usr/libexec"} {
		if _, statErr := os.Lstat(systemPath); statErr == nil {
			mounts = append(mounts, "--ro-bind", systemPath, systemPath)
		}
	}
	for _, input := range readOnlyInputs {
		resolved, resolveErr := filepath.Abs(input)
		if resolveErr != nil {
			return nil, false, false, fmt.Errorf("resolve read-only input: %w", resolveErr)
		}
		resolved = mustRealPath(resolved)
		if resolved == string(filepath.Separator) || resolved == mustRealPath(policy.Root) || pathWithin(resolved, mustRealPath(policy.Root)) {
			return nil, false, false, fmt.Errorf("read-only input overlaps staging boundary")
		}
		if _, statErr := os.Stat(resolved); statErr != nil {
			return nil, false, false, fmt.Errorf("read-only input %q: %w", input, statErr)
		}
		mounts = append(mounts, "--ro-bind", resolved, resolved)
	}
	if !policy.Network {
		mounts = append(mounts, "--unshare-net")
	}
	runner = append([]string{bwrap, "--die-with-parent"}, append(mounts, append([]string{"--"}, runner...)...)...)
	return runner, !policy.Network, true, nil
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

type bwrapStatus struct {
	ChildPID       int    `json:"child-pid"`
	MountNamespace uint64 `json:"mnt-namespace"`
	ExitCode       *int   `json:"exit-code,omitempty"`
}

type statusStartResult struct {
	status bwrapStatus
	err    error
}

func readBwrapStatus(reader *os.File) (<-chan statusStartResult, <-chan error) {
	start := make(chan statusStartResult, 1)
	done := make(chan error, 1)
	go func() {
		defer close(start)
		defer close(done)
		decoder := json.NewDecoder(reader)
		var initial bwrapStatus
		if err := decoder.Decode(&initial); err != nil {
			start <- statusStartResult{err: fmt.Errorf("read bwrap start status: %w", err)}
			done <- err
			return
		}
		start <- statusStartResult{status: initial}
		var terminal bwrapStatus
		if err := decoder.Decode(&terminal); err != nil {
			done <- fmt.Errorf("read bwrap terminal status: %w", err)
			return
		}
		if terminal.ExitCode == nil || *terminal.ExitCode != 0 {
			done <- fmt.Errorf("bwrap exited with status %v", terminal.ExitCode)
			return
		}
		done <- nil
	}()
	return start, done
}

func validateConfinementStart(status bwrapStatus, bwrapPID int) (uint64, error) {
	if status.ChildPID <= 0 || status.MountNamespace == 0 {
		return 0, ErrStagingBoundary
	}
	parentPID, err := processParent(status.ChildPID)
	if err != nil || parentPID != bwrapPID {
		return 0, ErrStagingBoundary
	}
	parentNamespace, err := namespaceID("/proc/self/ns/mnt")
	if err != nil {
		return 0, err
	}
	childPath := fmt.Sprintf("/proc/%d/ns/mnt", status.ChildPID)
	childNamespace, err := namespaceID(childPath)
	if err != nil || childNamespace != status.MountNamespace || childNamespace == parentNamespace {
		return 0, ErrStagingBoundary
	}
	return parentNamespace, nil
}

func finishBwrapStatus(done <-chan error, confined bool) error {
	if !confined {
		return nil
	}
	if err := <-done; err != nil {
		return err
	}
	return nil
}

func processParent(pid int) (int, error) {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(line, "PPid:") {
			continue
		}
		var parent int
		if _, err := fmt.Sscanf(line, "PPid:\t%d", &parent); err != nil {
			return 0, err
		}
		return parent, nil
	}
	return 0, fmt.Errorf("process parent is missing")
}

func namespaceID(path string) (uint64, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return 0, err
	}
	start := strings.IndexByte(target, '[')
	end := strings.IndexByte(target, ']')
	if start < 0 || end <= start+1 {
		return 0, fmt.Errorf("invalid namespace link %q", target)
	}
	return strconv.ParseUint(target[start+1:end], 10, 64)
}

func mustRealPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}
