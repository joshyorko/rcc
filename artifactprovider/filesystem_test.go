package artifactprovider

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/joshyorko/rcc/environmentartifact"
)

func testBlob(content []byte) Blob {
	return Blob{
		Descriptor: environmentartifact.Descriptor{
			MediaType: "application/vnd.rcc.test",
			Digest:    environmentartifact.DigestBytes(content),
			Size:      int64(len(content)),
		},
		Reader: bytes.NewReader(content),
	}
}

func TestCASRejectsSymlinkRoot(t *testing.T) {
	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "provider")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesystem(link); err == nil {
		t.Fatal("symlinked provider root accepted")
	}
}

func TestCASRejectsSymlinkParentComponent(t *testing.T) {
	root := t.TempDir()
	provider, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := testBlob([]byte("parent traversal sentinel"))
	outside := t.TempDir()
	fanout := filepath.Join(root, "objects", "sha256", blob.Descriptor.Digest.Hex()[:2])
	if err := os.Symlink(outside, fanout); err != nil {
		t.Fatal(err)
	}

	if err := provider.PutObject(context.Background(), blob); err == nil {
		t.Fatal("symlinked digest fanout accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("CAS wrote through symlinked parent: %v", entries)
	}
}

func TestCASRejectsWrongSizeAndDigestWithoutVisiblePartial(t *testing.T) {
	root := t.TempDir()
	provider, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := testBlob([]byte("correct"))
	blob.Reader = bytes.NewReader([]byte("wrong bytes"))
	if err := provider.PutObject(context.Background(), blob); err == nil {
		t.Fatal("wrong content accepted")
	}
	if _, err := os.Lstat(provider.objectPath(blob.Descriptor.Digest)); !os.IsNotExist(err) {
		t.Fatalf("failed upload became visible: %v", err)
	}
}

func TestCASRejectsExistingConflictingContent(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blob := testBlob([]byte("immutable"))
	path := provider.objectPath(blob.Descriptor.Digest)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("conflict!"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := provider.PutObject(context.Background(), blob); err == nil {
		t.Fatal("conflicting immutable destination accepted")
	}
}

func TestCASConcurrentIdenticalPublicationIsIdempotent(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("same immutable bytes")
	const workers = 12
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			errors <- provider.PutObject(context.Background(), testBlob(content))
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent publication: %v", err)
		}
	}
	got, err := os.ReadFile(provider.objectPath(environmentartifact.DigestBytes(content)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("stored content = %q", got)
	}
}

func TestCASPublicationSurvivesFilesystemProviderRestart(t *testing.T) {
	root := t.TempDir()
	content := []byte("restart durable")
	provider, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.PutObject(context.Background(), testBlob(content)); err != nil {
		t.Fatal(err)
	}
	provider, err = NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := provider.MissingObjects(context.Background(), []environmentartifact.Descriptor{testBlob(content).Descriptor})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("durable object reported missing: %v", missing)
	}
}
