package environmentlifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

func TestExecutionHandleRejectsStaleLeaseOwnerIdentity(t *testing.T) {
	materialization := acquiredMaterialization(t)
	materializer := NewLocalMaterializer()
	lease, err := materializer.Lease(context.Background(), materialization)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = materializer.Release(context.Background(), lease) }()

	previous := processIdentityLookup
	processIdentityLookup = func(int) (string, error) {
		return lease.OwnerStart + "-stale", nil
	}
	t.Cleanup(func() { processIdentityLookup = previous })

	if _, err := materializer.ExecutionHandle(context.Background(), lease, []string{"python", "-V"}); err == nil {
		t.Fatal("execution unexpectedly accepted a lease owned by a different process identity")
	} else if !strings.Contains(err.Error(), "lease owner") {
		t.Fatalf("stale lease error = %v", err)
	}
}

func TestExecutionCallerDeathBeforeSpawn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux parent-death supervision contract")
	}
	prepared := filepath.Join(t.TempDir(), "prepared")
	started := filepath.Join(t.TempDir(), "started")
	command := executionProcessHelperCommand(t, "before-spawn", prepared, started)
	if err := command.Run(); err != nil {
		t.Fatalf("pre-spawn helper failed: %v", err)
	}
	if content, err := os.ReadFile(prepared); err != nil || string(content) != "prepared" {
		t.Fatalf("pre-spawn preparation proof = %q, %v", content, err)
	}
	if _, err := os.Stat(started); !os.IsNotExist(err) {
		t.Fatalf("pre-spawn child marker = %v", err)
	}
}

func TestExecutionCallerDeathAfterSpawn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux parent-death supervision contract")
	}
	ready := filepath.Join(t.TempDir(), "ready")
	survived := filepath.Join(t.TempDir(), "survived")
	command := executionProcessHelperCommand(t, "after-spawn", ready, survived)
	if err := command.Run(); err != nil {
		t.Fatalf("post-spawn helper failed: %v", err)
	}
	if err := waitForExecutionFileAbsent(survived, 3*time.Second); err != nil {
		killExecutionTestChild(t, ready)
		t.Fatal(err)
	}
}

func TestExecutionCallerDeathAfterChildExit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux parent-death supervision contract")
	}
	ready := filepath.Join(t.TempDir(), "ready")
	completed := filepath.Join(t.TempDir(), "completed")
	command := executionProcessHelperCommand(t, "after-child-exit", ready, completed)
	if err := command.Run(); err != nil {
		t.Fatalf("post-exit helper failed: %v", err)
	}
	if content, err := os.ReadFile(completed); err != nil || string(content) != "completed" {
		t.Fatalf("post-exit completion proof = %q, %v", content, err)
	}
	if content, err := os.ReadFile(ready); err != nil || string(content) != "child-exited" {
		t.Fatalf("post-exit child proof = %q, %v", content, err)
	}
}

func TestExecutionProcessHelper(t *testing.T) {
	if os.Getenv("RCC_EXECUTION_PROCESS_HELPER") != "1" {
		return
	}
	arguments := executionProcessArguments()
	if len(arguments) != 3 {
		t.Fatalf("helper arguments = %v", arguments)
	}
	switch arguments[0] {
	case "before-spawn":
		supervisor, err := newExecutionSupervisor()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = supervisor.close() }()
		command := exec.Command(os.Args[0], "-test.run=^TestExecutionProcessChildHelper$", "--", "never-started", arguments[2], arguments[2])
		attrs := &syscall.SysProcAttr{}
		setExecutionBoolField(attrs, "Setpgid", true)
		command.SysProcAttr = attrs
		preparer, ok := any(supervisor).(interface{ prepare(*exec.Cmd) error })
		if !ok {
			t.Fatal("execution supervisor has no command preparation seam")
		}
		if err := preparer.prepare(command); err != nil {
			t.Fatal(err)
		}
		if err := assertExecutionPdeathsig(command); err != nil {
			t.Fatal(err)
		}
		if !executionBoolField(command.SysProcAttr, "Setpgid") {
			t.Fatal("command preparation discarded an existing SysProcAttr field")
		}
		if err := os.WriteFile(arguments[1], []byte("prepared"), 0o600); err != nil {
			t.Fatal(err)
		}
	case "after-spawn":
		executionProcessHelperRunsChild(t, arguments[0], arguments[1], arguments[2])
	case "after-child-exit":
		materializer := executionProcessTestMaterializer{}
		command := []string{os.Args[0], "-test.run=^TestExecutionProcessChildHelper$", "--", "short", arguments[1], arguments[2]}
		_, child, err := Execute(context.Background(), materializer, Materialization{}, command)
		if err != nil || child.ExitCode != 0 {
			t.Fatalf("short execution = child %+v, err %v", child, err)
		}
		if err := os.WriteFile(arguments[2], []byte("completed"), 0o600); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown helper mode %q", arguments[0])
	}
}

func TestExecutionProcessChildHelper(t *testing.T) {
	if os.Getenv("RCC_EXECUTION_PROCESS_HELPER") != "1" {
		return
	}
	arguments := executionProcessArguments()
	if len(arguments) != 3 {
		t.Fatalf("child arguments = %v", arguments)
	}
	ready := map[string]string{
		"short":         "child-exited",
		"never-started": "started",
	}[arguments[0]]
	if arguments[0] == "long" {
		ready = fmt.Sprintf("%d", os.Getpid())
	}
	if err := os.WriteFile(arguments[1], []byte(ready), 0o600); err != nil {
		t.Fatal(err)
	}
	if arguments[0] == "short" {
		return
	}
	if arguments[0] != "long" {
		t.Fatalf("unknown child mode %q", arguments[0])
	}
	time.Sleep(1500 * time.Millisecond)
	if err := os.WriteFile(arguments[2], []byte("survived"), 0o600); err != nil {
		t.Fatal(err)
	}
}

type executionProcessTestMaterializer struct{}

func (executionProcessTestMaterializer) Materialize(context.Context, environmentartifact.Manifest) (Materialization, error) {
	return Materialization{}, errors.New("unused in execution process test")
}

func (executionProcessTestMaterializer) Lease(context.Context, Materialization) (Lease, error) {
	return Lease{ID: "execution-process-test-lease", MaterializationID: "execution-process-test-materialization"}, nil
}

func (executionProcessTestMaterializer) ExecutionHandle(_ context.Context, lease Lease, command []string) (ExecutionHandle, error) {
	if len(command) == 0 {
		return ExecutionHandle{}, errors.New("empty test command")
	}
	return ExecutionHandle{
		MaterializationID: lease.MaterializationID,
		LeaseID:           lease.ID,
		CWD:               mustExecutionTestWorkingDirectory(),
		Executable:        command[0],
		Environment:       os.Environ(),
	}, nil
}

func (executionProcessTestMaterializer) Release(context.Context, Lease) error { return nil }

func executionProcessHelperCommand(t *testing.T, mode, first, second string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestExecutionProcessHelper$", "--", mode, first, second)
	command.Env = append(os.Environ(), "RCC_EXECUTION_PROCESS_HELPER=1")
	return command
}

func executionProcessHelperRunsChild(t *testing.T, mode, ready, proof string) {
	t.Helper()
	command := []string{os.Args[0], "-test.run=^TestExecutionProcessChildHelper$", "--", "long", ready, proof}
	done := make(chan error, 1)
	go func() {
		_, _, err := Execute(context.Background(), executionProcessTestMaterializer{}, Materialization{}, command)
		done <- err
	}()
	waitForExecutionFile(t, ready, 3*time.Second)
	os.Exit(0)
}

func executionProcessArguments() []string {
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) {
			return os.Args[index+1:]
		}
	}
	return nil
}

func waitForExecutionFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForExecutionFileAbsent(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("caller-death child survived: %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func killExecutionTestChild(t *testing.T, ready string) {
	t.Helper()
	content, err := os.ReadFile(ready)
	if err != nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(string(content), "%d", &pid); err != nil || pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}

func mustExecutionTestWorkingDirectory() string {
	workingDirectory, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return workingDirectory
}

func assertExecutionPdeathsig(command *exec.Cmd) error {
	if command.SysProcAttr == nil {
		return errors.New("execution command has no SysProcAttr")
	}
	value := reflect.ValueOf(command.SysProcAttr)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return errors.New("execution command SysProcAttr is not a pointer")
	}
	field := value.Elem().FieldByName("Pdeathsig")
	if !field.IsValid() || field.Kind() < reflect.Int || field.Kind() > reflect.Int64 {
		return errors.New("execution command has no Pdeathsig")
	}
	if field.Int() != 9 {
		return fmt.Errorf("execution Pdeathsig = %d, want 9", field.Int())
	}
	return nil
}

func setExecutionBoolField(attrs *syscall.SysProcAttr, name string, value bool) {
	field := reflect.ValueOf(attrs).Elem().FieldByName(name)
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.Bool {
		field.SetBool(value)
	}
}

func executionBoolField(attrs *syscall.SysProcAttr, name string) bool {
	if attrs == nil {
		return false
	}
	field := reflect.ValueOf(attrs).Elem().FieldByName(name)
	return field.IsValid() && field.Kind() == reflect.Bool && field.Bool()
}
