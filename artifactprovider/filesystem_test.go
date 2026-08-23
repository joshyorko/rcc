package artifactprovider

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
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

func TestCASRejectsSymlinkAncestorOfProviderRoot(t *testing.T) {
	outside := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "redirect")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesystem(filepath.Join(link, "provider")); err == nil {
		t.Fatal("symlinked provider-root ancestor accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("provider initialized through symlinked ancestor: %v", entries)
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

func TestFilesystemObjectReaderRejectsTamperedProviderBytes(t *testing.T) {
	p, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blob := testBlob([]byte("trusted bytes"))
	if err := p.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.objectPath(blob.Descriptor.Digest), []byte("tampered bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.GetObjectByDigest(context.Background(), blob.Descriptor.Digest); err == nil {
		t.Fatal("tampered object was served")
	}
}

func TestFilesystemIdempotentPublicationFailsClosedWhenAuditIsUnavailable(t *testing.T) {
	root := t.TempDir()
	p, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := testBlob([]byte("audit failure publication"))
	if err := p.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(root, filesystemAuditFile)
	if err := os.Remove(auditPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(auditPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := p.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader([]byte("audit failure publication"))}); err == nil {
		t.Fatal("idempotent publication ignored audit failure")
	}
}

func TestFilesystemRestoreRollbackMarkerRecoversAfterPublicationFailure(t *testing.T) {
	root := t.TempDir()
	provider, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := testBlob([]byte("restore rollback marker"))
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	name := "objects/sha256/" + blob.Descriptor.Digest.Hex()
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: blob.Descriptor.Size}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("restore rollback marker")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	filesystemRestorePublishHook = func(string) error { return errors.New("injected restore publication failure") }
	defer func() { filesystemRestorePublishHook = nil }()
	if err := provider.Restore(context.Background(), bytes.NewReader(archive.Bytes())); err == nil {
		t.Fatal("injected restore failure was accepted")
	}
	recovered, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	health, err := recovered.Health(context.Background())
	if err != nil || !health.Ready || health.Corrupt {
		t.Fatalf("recovery health=%+v err=%v", health, err)
	}
	if _, err := os.Stat(provider.objectPath(blob.Descriptor.Digest)); !os.IsNotExist(err) {
		t.Fatalf("rolled back object remains: %v", err)
	}
}

func TestFilesystemRestoreCrashMatrix(t *testing.T) {
	if os.Getenv("RCC_FILESYSTEM_CRASH_CHILD") == "1" {
		root := os.Getenv("RCC_FILESYSTEM_CRASH_ROOT")
		archive, _ := os.ReadFile(os.Getenv("RCC_FILESYSTEM_CRASH_ARCHIVE"))
		p, err := NewFilesystem(root)
		if err != nil {
			os.Exit(2)
		}
		_ = p.Restore(context.Background(), bytes.NewReader(archive))
		os.Exit(4)
	}
	source, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blob := testBlob([]byte("filesystem crash matrix"))
	if err := source.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := source.Backup(context.Background(), &archive); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "backup.tar")
	if err := os.WriteFile(archivePath, archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{"filesystem-marker", "filesystem-publish", "filesystem-complete"} {
		root := t.TempDir()
		cmd := exec.Command(os.Args[0], "-test.run=TestFilesystemRestoreCrashMatrix")
		cmd.Env = append(os.Environ(), "RCC_FILESYSTEM_CRASH_CHILD=1", "RCC_FILESYSTEM_CRASH_ROOT="+root, "RCC_FILESYSTEM_CRASH_ARCHIVE="+archivePath, "RCC_PROVIDER_RESTORE_CRASH="+boundary)
		if err := cmd.Run(); err == nil {
			t.Fatalf("boundary %s did not crash", boundary)
		}
		recovered, err := NewFilesystem(root)
		if err != nil {
			t.Fatalf("boundary %s restart=%v", boundary, err)
		}
		health, err := recovered.Health(context.Background())
		if err != nil || !health.Ready {
			t.Fatalf("boundary %s health=%+v err=%v", boundary, health, err)
		}
	}
}
