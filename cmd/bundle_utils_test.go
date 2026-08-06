package cmd

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testZipReader(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()

	var payload bytes.Buffer
	zw := zip.NewWriter(&payload)
	for name, contents := range files {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(payload.Bytes()), int64(payload.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestExtractRobotTreeRejectsSymlinkComponent(t *testing.T) {
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "linked")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	err := extractRobotTree(testZipReader(t, map[string]string{
		"robot/linked/escaped.txt": "must stay contained",
	}), dest)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symbolic-link rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote through destination symlink: %v", err)
	}
}

func TestExtractRobotTreeCreatesDestination(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "workarea")

	err := extractRobotTree(testZipReader(t, map[string]string{
		"robot/task.py": "print('ok')",
	}), dest)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dest, "task.py"))
	if err != nil || string(contents) != "print('ok')" {
		t.Fatalf("extracted file missing: contents=%q err=%v", contents, err)
	}
}

func TestUnpackRobotTreeForceReplacesDestination(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "robot")
	outside := t.TempDir()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "linked")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	err := unpackRobotTree(testZipReader(t, map[string]string{
		"robot/current.txt":          "current",
		"robot/linked/contained.txt": "contained",
	}), dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("force unpack merged with the old destination: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(dest, "current.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "current" {
		t.Fatalf("unexpected extracted contents %q", contents)
	}
	if _, err := os.Stat(filepath.Join(outside, "contained.txt")); !os.IsNotExist(err) {
		t.Fatalf("force unpack followed the old destination symlink: %v", err)
	}
	contents, err = os.ReadFile(filepath.Join(dest, "linked", "contained.txt"))
	if err != nil || string(contents) != "contained" {
		t.Fatalf("staged file missing: contents=%q err=%v", contents, err)
	}
}

func TestUnpackRobotTreeFailurePreservesDestination(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "robot")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := unpackRobotTree(testZipReader(t, map[string]string{
		"robot/../../escaped.txt": "unsafe",
	}), dest, true)
	if err == nil {
		t.Fatal("expected unsafe path to fail")
	}
	contents, readErr := os.ReadFile(filepath.Join(dest, "keep.txt"))
	if readErr != nil || string(contents) != "keep" {
		t.Fatalf("failed unpack changed the old destination: contents=%q err=%v", contents, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(parent, "escaped.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe archive wrote outside the destination: %v", statErr)
	}
}
