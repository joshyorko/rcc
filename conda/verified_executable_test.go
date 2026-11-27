package conda

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestPublishExecutableVerifiedAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helper")
	body := []byte("verified helper")
	h := sha256.Sum256(body)
	digest := hex.EncodeToString(h[:])
	if err := publishExecutable(path, digest, 100, strings.NewReader(string(body))); err != nil {
		t.Fatal(err)
	}
	if !verifiedExecutable(path, digest, 100) {
		t.Fatal("published executable failed verification")
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Symlink(path, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := publishExecutable(filepath.Join(dir, "alias"), digest, 100, strings.NewReader("bad")); err == nil {
		t.Fatal("invalid source accepted")
	}
	if !verifiedExecutable(path, digest, 100) {
		t.Fatal("symlink publication changed target")
	}
}

type interruptedReader struct{}

func (interruptedReader) Read([]byte) (int, error) { return 0, os.ErrClosed }

func TestPublishExecutableRetriesAfterInterruptedWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helper")
	body := "retry helper"
	h := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(h[:])
	if err := publishExecutable(path, digest, 100, interruptedReader{}); err == nil {
		t.Fatal("interrupted source accepted")
	}
	if err := publishExecutable(path, digest, 100, strings.NewReader(body)); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if !verifiedExecutable(path, digest, 100) {
		t.Fatal("retry publication failed verification")
	}
}

func TestPublishExecutableRejectsBadSourceAndPreservesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helper")
	if err := os.WriteFile(path, []byte("old"), 0700); err != nil {
		t.Fatal(err)
	}
	err := publishExecutable(path, strings.Repeat("0", 64), 10, strings.NewReader("corrupt"))
	if err == nil {
		t.Fatal("corrupt source accepted")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "old" {
		t.Fatalf("destination changed: %q, %v", body, err)
	}
}

func TestPublishExecutableConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helper")
	body := "concurrent helper"
	h := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(h[:])
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- publishExecutable(path, digest, 100, strings.NewReader(body)) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !verifiedExecutable(path, digest, 100) {
		t.Fatal("concurrent publication is invalid")
	}
}
