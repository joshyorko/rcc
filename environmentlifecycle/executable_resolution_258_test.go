package environmentlifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/conda"
)

func TestExecutableResolution258BareNameUsesOnlyMaterializedPath(t *testing.T) {
	root := t.TempDir()
	bin := conda.CondaPaths(root)[0]
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rcc258-unique-probe"
	artifactExecutable := writeResolutionExecutable258(t, filepath.Join(bin, name))
	hostDirectory := t.TempDir()
	writeResolutionExecutable258(t, filepath.Join(hostDirectory, name))
	t.Setenv("PATH", hostDirectory)

	resolved, err := resolveMaterializedExecutable(root, name)
	if err != nil {
		t.Fatalf("resolve artifact executable: %v", err)
	}
	if resolved != artifactExecutable {
		t.Fatalf("resolved executable = %q, want artifact executable %q", resolved, artifactExecutable)
	}
	if _, err := resolveMaterializedExecutable(root, "rcc258-missing-probe"); err == nil || !strings.Contains(err.Error(), "rcc258-missing-probe") {
		t.Fatalf("missing executable error = %v", err)
	}
}

func TestExecutableResolution258AbsoluteAndRelativePaths(t *testing.T) {
	root := t.TempDir()
	bin := conda.CondaPaths(root)[0]
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	relative := writeResolutionExecutable258(t, filepath.Join(bin, "relative-probe"))
	relativeCommand, err := filepath.Rel(root, relative)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveMaterializedExecutable(root, relativeCommand)
	if err != nil || resolved != relative {
		t.Fatalf("relative executable = %q, %v; want %q", resolved, err, relative)
	}

	host := writeResolutionExecutable258(t, filepath.Join(t.TempDir(), "host-probe"))
	resolved, err = resolveMaterializedExecutable(root, host)
	if err != nil || resolved != host {
		t.Fatalf("absolute host executable = %q, %v; want %q", resolved, err, host)
	}
}

func TestExecutableResolution258RejectsTraversalAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := writeResolutionExecutable258(t, filepath.Join(filepath.Dir(root), "outside-probe"))
	if _, err := resolveMaterializedExecutable(root, filepath.Join("..", filepath.Base(outside))); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("traversal path error = %v", err)
	}
	bin := conda.CondaPaths(root)[0]
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "escaping-probe"+filepath.Ext(outside))
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolveMaterializedExecutable(root, "escaping-probe"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("escaping symlink error = %v", err)
	}
}

func TestExecutableResolution258PythonAliasesAndWindowsPATHEXT(t *testing.T) {
	for _, command := range []string{"python", "python3", "python.exe"} {
		if !isMaterializedPythonCommand(command) {
			t.Errorf("bare Python command %q was not mapped", command)
		}
	}
	for _, command := range []string{"./python", filepath.Join("bin", "python3")} {
		if isMaterializedPythonCommand(command) {
			t.Errorf("path-qualified Python command %q was mapped", command)
		}
	}
	got := windowsExecutableExtensions(".EXE; cmd ;.exe;; .BAT")
	want := []string{".exe", ".cmd", ".bat"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("PATHEXT normalization = %v, want %v", got, want)
	}
	defaults := windowsExecutableExtensions("")
	if strings.Join(defaults, ",") != ".com,.exe,.bat,.cmd" {
		t.Fatalf("default PATHEXT = %v", defaults)
	}
}

func writeResolutionExecutable258(t *testing.T, path string) string {
	t.Helper()
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		path += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecutableResolution258AllowsSymlinkedParentOfEnvironment(t *testing.T) {
	physical := t.TempDir()
	root := filepath.Join(physical, "environment")
	executable := writeResolutionExecutable258(t, filepath.Join(conda.CondaPaths(root)[0], "probe"))
	alias := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resolved, err := resolveMaterializedExecutable(filepath.Join(alias, "environment"), "probe")
	if err != nil || resolved != executable {
		t.Fatalf("symlinked environment parent: got %q, %v; want %q", resolved, err, executable)
	}
}
