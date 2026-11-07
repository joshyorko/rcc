package environmentlifecycle

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/internal/trustfixture"
)

func TestMetadataRevocationDeniesNewLeaseWithoutTerminatingRunningProcess(t *testing.T) {
	m := acquiredMaterialization(t)
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := artifacttrust.Sign(m.ArtifactDigest.String(), "artifact", private)
	if err != nil {
		t.Fatal(err)
	}
	metadataPublic, metadataPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	p := artifacttrust.MetadataPayload{SchemaVersion: 2, Scope: "lease-test", ArtifactDigest: m.ArtifactDigest.String(), Sequence: 1, KeyID: "authority", IssuedAt: at.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339)}
	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	e, err := artifacttrust.SignMetadata(p, metadataPrivate)
	if err != nil {
		t.Fatal(err)
	}
	q := artifacttrust.VerifyRequest{ArtifactDigest: m.ArtifactDigest.String(), Keys: map[string]ed25519.PublicKey{"artifact": public}, Signatures: []artifacttrust.Signature{signature}, Metadata: &e,
		MetadataRoots: &artifacttrust.MetadataRoots{SchemaVersion: 2, Scope: p.Scope, MinimumSequence: 1, Keys: map[string]artifacttrust.MetadataKey{p.KeyID: {PublicKey: base64.RawStdEncoding.EncodeToString(metadataPublic), NotBefore: at.Add(-time.Hour).Format(time.RFC3339), NotAfter: at.Add(2 * time.Hour).Format(time.RFC3339)}}}, MetadataState: &artifacttrust.MetadataState{Root: t.TempDir()}}
	trustfixture.Put(t, carrier, q)
	m.TrustPolicy = &artifacttrust.Policy{Mode: artifacttrust.StrictRemote, FailClosedRevocations: true}
	m.TrustRequest, m.TrustCarrier = &q, carrier
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	stdin, input := io.Pipe()
	defer input.Close()
	defer stdin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	type result struct {
		handle ExecutionHandle
		child  ChildResult
		err    error
	}
	done := make(chan result, 1)
	go func() {
		handle, child, err := ExecuteWithStreams(ctx, NewLocalMaterializer(), m, []string{executable, "-test.run=^TestMetadataLeaseChild$", "--", ready}, stdin, &stdout, &stderr)
		done <- result{handle, child, err}
	}()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case r := <-done:
			t.Fatalf("child ended before readiness: %+v %s %s", r, stdout.String(), stderr.String())
		case <-ctx.Done():
			t.Fatal("child readiness timed out")
		case <-time.After(10 * time.Millisecond):
		}
	}
	history, err := artifacttrust.NewReceiptStore(filepath.Join(common.Product.Home(), "artifacts", "v1", "verification")).History(m.ArtifactDigest.String())
	if err != nil || len(history) == 0 {
		t.Fatal(history, err)
	}
	before, err := history[len(history)-1].JSON()
	if err != nil {
		t.Fatal(err)
	}
	p.Sequence = 2
	p.Revocations = []artifacttrust.Revocation{{KeyIDs: []string{"artifact"}, UpdatedAt: at.Format(time.RFC3339)}}
	revoked, err := artifacttrust.SignMetadata(p, metadataPrivate)
	if err != nil {
		t.Fatal(err)
	}
	revokedRequest := q
	revokedRequest.Metadata = &revoked
	trustfixture.Put(t, carrier, revokedRequest)
	if _, err := NewLocalMaterializer().Lease(ctx, m); err == nil || !strings.Contains(err.Error(), artifacttrust.CodeRevoked) {
		t.Fatalf("new lease after revocation: %v", err)
	}
	trustfixture.Put(t, carrier, q)
	if _, err := NewLocalMaterializer().Lease(ctx, m); err == nil || !strings.Contains(err.Error(), artifacttrust.CodeMetadataRollback) {
		t.Fatalf("lease rollback after revocation: %v", err)
	}
	select {
	case r := <-done:
		t.Fatalf("running child was interrupted by metadata refresh: %+v", r)
	default:
	}
	if _, err := input.Write([]byte("finish\n")); err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	r := <-done
	if r.err != nil || r.child.ExitCode != 0 || !strings.Contains(stdout.String(), "running-child-completed") {
		t.Fatalf("running child lost result: %+v %s %s", r, stdout.String(), stderr.String())
	}
	if r.handle.Verification.MetadataSequence != 1 || r.handle.Verification.ArtifactDigest != m.ArtifactDigest.String() {
		t.Fatal("running decision was replaced", r.handle.Verification)
	}
	if _, err := readLease(m.ArtifactDigest, r.handle.LeaseID); !os.IsNotExist(err) {
		t.Fatalf("lease not released after reap: %v", err)
	}
	history, err = artifacttrust.NewReceiptStore(filepath.Join(common.Product.Home(), "artifacts", "v1", "verification")).History(m.ArtifactDigest.String())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, receipt := range history {
		data, err := receipt.JSON()
		if err != nil {
			t.Fatal(err)
		}
		found = found || bytes.Equal(data, before)
	}
	if !found {
		t.Fatal("original decision receipt was changed or lost")
	}
}

func TestMetadataLeaseChild(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	var token string
	if _, err := fmt.Fscan(os.Stdin, &token); err != nil || token != "finish" {
		t.Fatalf("child request: %q %v", token, err)
	}
	fmt.Println("running-child-completed")
}

func TestMetadataArchiveTransportPreservesEnvelopeAndArtifactIdentity(t *testing.T) {
	_, provider, digest := publishedFixture(t)
	carrier := artifacttrust.NewFilesystemCarrier(t.TempDir())
	q := trustfixture.Authenticate(t, artifacttrust.VerifyRequest{ArtifactDigest: digest.String()})
	trustfixture.Put(t, carrier, q)
	archive := filepath.Join(t.TempDir(), "authenticated.rcca")
	exported, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: digest, Provider: provider, TrustCarrier: carrier, OutputPath: archive})
	if err != nil {
		t.Fatal(err)
	}
	common.Product.ForceHome(t.TempDir())
	target := artifacttrust.NewFilesystemCarrier(t.TempDir())
	imported, err := ImportArchive(context.Background(), ImportArchiveRequest{Path: archive, TrustCarrier: target})
	if err != nil {
		t.Fatal(err)
	}
	if imported.ArtifactDigest != digest || exported.ArtifactDigest != digest {
		t.Fatal("metadata changed immutable Artifact identity")
	}
	got, err := artifacttrust.ReadMetadata(target, digest.String())
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, _ := json.Marshal(q.Metadata)
	gotBytes, _ := json.Marshal(got)
	if !bytes.Equal(wantBytes, gotBytes) {
		t.Fatal("archive changed or omitted authenticated metadata")
	}
	if _, err := artifacttrust.VerifyMetadata(*got, *q.MetadataRoots, digest.String(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}
