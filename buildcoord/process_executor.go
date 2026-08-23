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
	Command     []string
	Environment []string
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
	runner, networkIsolated, err := constrainedCommand(it.Command, policy)
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
	command.Stdout = &output
	command.Stderr = &output
	processID := 0
	if err := command.Start(); err != nil {
		return Artifact{}, fmt.Errorf("start staged build: %w", err)
	}
	processID = command.Process.Pid
	err = command.Wait()
	if runCtx.Err() != nil {
		return Artifact{}, runCtx.Err()
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("staged build failed: %w: %s", err, output.String())
	}
	proof, err := os.ReadFile(filepath.Join(policy.Root, ".rcc-process-root"))
	if err != nil || strings.TrimSpace(string(proof)) != mustRealPath(policy.Root) {
		return Artifact{}, ErrStagingBoundary
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
		CPULimit: policy.CPULimit, MemoryBytes: policy.MemoryBytes, Timeout: policy.Timeout,
		NetworkIsolated: networkIsolated, CredentialsExcluded: true,
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

func constrainedCommand(command []string, policy ExecutionPolicy) ([]string, bool, error) {
	runner := append([]string(nil), command...)
	script := `set -eu
root=$(pwd -P)
test "$root" = "$RCC_BUILD_STAGING_ROOT"
printf '%s' "$root" > "$RCC_BUILD_STAGING_ROOT/.rcc-process-root"
exec "$@"
`
	runner = append([]string{"/bin/sh", "-c", script, "rcc-staged-build"}, runner...)
	if policy.CPULimit > 0 || policy.MemoryBytes > 0 {
		prlimit, err := exec.LookPath("prlimit")
		if err != nil {
			return nil, false, fmt.Errorf("%w: prlimit is unavailable", ErrUnenforcedBuildPolicy)
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
	if !policy.Network {
		bwrap, err := exec.LookPath("bwrap")
		if err != nil {
			return nil, false, fmt.Errorf("%w: network namespace boundary is unavailable", ErrUnenforcedBuildPolicy)
		}
		runner = append([]string{bwrap, "--die-with-parent", "--unshare-net", "--bind", string(filepath.Separator), string(filepath.Separator), "--chdir", policy.Root, "--"}, runner...)
	}
	return runner, !policy.Network, nil
}

func mustRealPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}
