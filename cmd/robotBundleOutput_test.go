package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareBundleOutputWritersCaptureSeparateStreams(t *testing.T) {
	workarea := t.TempDir()
	var stdout, stderr bytes.Buffer
	writers, err := prepareBundleOutputWriters(workarea, "reports", true, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writers.stdout.Write([]byte("out\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := writers.stderr.Write([]byte("err\n")); err != nil {
		t.Fatal(err)
	}
	if err := writers.close(); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out\n" || stderr.String() != "err\n" {
		t.Fatalf("streams crossed or disappeared: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	for name, want := range map[string]string{"stdout.log": "out\n", "stderr.log": "err\n"} {
		got, err := os.ReadFile(filepath.Join(workarea, "reports", name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestPrepareBundleOutputWritersRejectUnsafeArtifactDirectories(t *testing.T) {
	workarea := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workarea, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, directory := range []string{"../escape", filepath.Join(outside, "absolute"), "linked/output", "."} {
		if _, err := prepareBundleOutputWriters(workarea, directory, true, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Errorf("unsafe artifact directory %q was accepted", directory)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsafe artifact path wrote outside workarea: entries=%v err=%v", entries, err)
	}
}

func TestFinalizeBundleArtifactsRejectsSymlinkEscapes(t *testing.T) {
	workarea := t.TempDir()
	project := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(workarea, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "owned.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workarea, "reports", "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := finalizeBundleArtifactsTo(workarea, filepath.Join(workarea, "reports"), project); err == nil {
		t.Fatal("expected artifact symlink escape to be rejected")
	}
	if _, err := os.Stat(filepath.Join(project, "reports", "linked", "owned.txt")); !os.IsNotExist(err) {
		t.Fatalf("artifact copy created an escaped project path: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(outside, "owned.txt"))
	if err != nil || string(contents) != "keep" {
		t.Fatalf("outside file changed: %q, %v", contents, err)
	}
}
