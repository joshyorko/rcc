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
	var confinementInfo *os.File
	var confinementPath string
	if confined {
		confinementInfo, err = os.CreateTemp("", "rcc-process-confinement-")
		if err != nil {
			return Artifact{}, fmt.Errorf("create parent-owned confinement receipt: %w", err)
		}
		confinementPath = confinementInfo.Name()
		defer confinementInfo.Close()
		defer os.Remove(confinementPath)
		command.ExtraFiles = []*os.File{confinementInfo}
	}
	processID := 0
	if err := command.Start(); err != nil {
		return Artifact{}, fmt.Errorf("start staged build: %w", err)
	}
	processID = command.Process.Pid
	err = command.Wait()
	if confinementInfo != nil {
		_ = confinementInfo.Close()
	}
	if runCtx.Err() != nil {
		return Artifact{}, runCtx.Err()
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("staged build failed: %w: %s", err, diagnostics.String())
	}
	confinementPID, mountNamespace, err := readConfinementReceipt(confinementPath, confined)
	if err != nil {
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
		CPULimit: policy.CPULimit, MemoryBytes: policy.MemoryBytes, Timeout: policy.Timeout,
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
		"--info-fd", "3",
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

func readConfinementReceipt(path string, confined bool) (int, uint64, error) {
	if !confined {
		return 0, 0, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("read parent-owned confinement receipt: %w", err)
	}
	var receipt struct {
		ChildPID       int    `json:"child-pid"`
		MountNamespace uint64 `json:"mnt-namespace"`
	}
	if err := json.Unmarshal(content, &receipt); err != nil || receipt.ChildPID <= 0 || receipt.MountNamespace == 0 {
		return 0, 0, ErrStagingBoundary
	}
	return receipt.ChildPID, receipt.MountNamespace, nil
}

func mustRealPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}
