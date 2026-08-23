package artifactprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/environmentartifact"
)

type policyFlipProvider struct {
	*Journal
	policy      *Policy
	failurePath string
}

func (p *policyFlipProvider) PutObject(ctx context.Context, b Blob) error {
	err := p.Journal.PutObject(ctx, b)
	p.policy.statePath = p.failurePath
	return err
}

func TestJournalDurableRestartContract(t *testing.T) {
	path := t.TempDir() + "/provider.log"
	content := []byte("journal durable object")
	blob := testBlob(content)
	j, err := NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	reader, err := j.GetObject(context.Background(), blob.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(raw, content) {
		t.Fatalf("read=%q err=%v", raw, err)
	}
	j, err = NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := j.MissingObjects(context.Background(), []environmentartifact.Descriptor{blob.Descriptor})
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing=%v err=%v", missing, err)
	}
}

func TestJournalRejectsManifestWithMissingObjects(t *testing.T) {
	j, err := NewJournal(t.TempDir() + "/provider.log")
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	if err := j.CommitManifest(context.Background(), fixture.manifestBytes); err == nil {
		t.Fatal("journal committed manifest without its referenced objects")
	}
}

func TestJournalRecoversTornFinalRecord(t *testing.T) {
	path := t.TempDir() + "/provider.log"
	if err := os.WriteFile(path, []byte("{\"Kind\":\"object\"}\n{\"Kind\":\"object\""), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJournal(path); err != nil {
		t.Fatalf("torn final record should be discarded: %v", err)
	}
}

func TestJournalStreamsObjectsToSidecar(t *testing.T) {
	path := t.TempDir() + "/provider.log"
	content := bytes.Repeat([]byte("x"), 64*1024)
	j, err := NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.PutObject(context.Background(), testBlob(content)); err != nil {
		t.Fatal(err)
	}
	logBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logBytes, []byte(base64.StdEncoding.EncodeToString(content))) {
		t.Fatal("journal embedded object payload")
	}
	if _, err := os.Stat(path + ".objects/" + testBlob(content).Descriptor.Digest.Hex()); err != nil {
		t.Fatal(err)
	}
}

func TestJournalRestoreIsRestartDurableWithoutDuplicateHistory(t *testing.T) {
	source, err := NewJournal(t.TempDir() + "/source.log")
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		raw, _ := io.ReadAll(blob.Reader)
		if err := source.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(raw)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.CommitManifest(context.Background(), fixture.manifestBytes); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := source.Backup(context.Background(), &archive); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/target.log"
	target, err := NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Restore(context.Background(), bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Restore(context.Background(), bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("idempotent restore appended duplicate history")
	}
	restarted, err := NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err != nil {
		t.Fatalf("restart lost restored manifest: %v", err)
	}
}

func TestJournalRestoreFailureLeavesRecoverableUncommittedTransaction(t *testing.T) {
	source, err := NewJournal(t.TempDir() + "/source.log")
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		raw, _ := io.ReadAll(blob.Reader)
		if err := source.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(raw)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.CommitManifest(context.Background(), fixture.manifestBytes); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := source.Backup(context.Background(), &archive); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/target.log"
	target, err := NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	journalAppendHook = func(record journalRecord) error {
		if record.Kind == "manifest" {
			return errors.New("injected journal publication failure")
		}
		return nil
	}
	defer func() { journalAppendHook = nil }()
	if err := target.Restore(context.Background(), bytes.NewReader(archive.Bytes())); err == nil {
		t.Fatal("injected restore failure was accepted")
	}
	restarted, err := NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := restarted.ListObjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 0 {
		t.Fatalf("uncommitted restore objects became live: %d", len(objects))
	}
}

func TestPolicyTypedQuotaAndRateErrors(t *testing.T) {
	j, _ := NewJournal(t.TempDir() + "/q.log")
	p := NewPolicy(j, Limits{MaxBytes: 2, RequestsPerSecond: 1})
	blob := testBlob([]byte("three"))
	if err := p.PutObject(context.Background(), blob); err == nil || !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("quota err=%v", err)
	}
}

func TestPolicyDoesNotChargeDuplicateUploads(t *testing.T) {
	j, err := NewJournal(t.TempDir() + "/policy.log")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPolicy(j, Limits{MaxObjects: 1, MaxBytes: 4, MaxUploads: 2})
	blob := testBlob([]byte("body"))
	if err := p.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	if err := p.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader([]byte("body"))}); err != nil {
		t.Fatalf("idempotent duplicate rejected: %v", err)
	}
	other := testBlob([]byte("more"))
	if err := p.PutObject(context.Background(), other); err == nil || !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("next distinct upload should be quota-limited: %v", err)
	}
}

func TestPolicyRateLimitIsCallerVisibleAndTyped(t *testing.T) {
	j, err := NewJournal(t.TempDir() + "/rate.log")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPolicy(j, Limits{RequestsPerSecond: 1})
	if _, err := p.MissingObjects(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := p.MissingObjects(context.Background(), nil); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate-limit outcome=%v", err)
	}
}

func TestPolicyPersistenceFailureReportsCommittedMutation(t *testing.T) {
	j, err := NewJournal(t.TempDir() + "/outcome.log")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPolicy(j, Limits{})
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.json")
	p.statePath = statePath
	p.Provider = &policyFlipProvider{Journal: j, policy: p, failurePath: stateDir}
	blob := testBlob([]byte("mutation outcome"))
	err = p.PutObject(context.Background(), blob)
	var mutation *MutationError
	if !errors.As(err, &mutation) || !mutation.Committed {
		t.Fatalf("outcome=%v", err)
	}
	p.statePath = statePath
	if err := p.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader([]byte("mutation outcome"))}); err != nil {
		t.Fatalf("committed retry failed: %v", err)
	}
}

func TestPolicyQuotaSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/policy.log"
	j, err := NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPolicy(j, Limits{MaxObjects: 1})
	if err := p.PutObject(context.Background(), testBlob([]byte("body"))); err != nil {
		t.Fatal(err)
	}
	j, err = NewJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p = NewPolicy(j, Limits{MaxObjects: 1})
	if err := p.PutObject(context.Background(), testBlob([]byte("more"))); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("restart quota err=%v", err)
	}
}
