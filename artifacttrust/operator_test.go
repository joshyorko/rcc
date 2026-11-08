package artifacttrust

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMetadataOperatorRenewalPreservesCompleteSnapshotAndDenials(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	o := MetadataOperator{Roots: roots, State: MetadataState{Root: t.TempDir()}}
	c := MetadataChange{Operation: "issue", ArtifactDigest: p.ArtifactDigest, Sequence: p.Sequence, KeyID: p.KeyID, IssuedAt: p.IssuedAt, ExpiresAt: p.ExpiresAt, Provenance: p.Provenance, SBOM: p.SBOM, Revocations: []Revocation{}}
	first, receipt, err := o.Change(c, nil, key, at)
	if err != nil || !receipt.Valid || !receipt.Retained {
		t.Fatal(receipt, err)
	}
	c.Operation = "revoke"
	c.Sequence++
	c.Provenance = nil
	c.SBOM = nil
	c.Revocations = nil
	c.RevokeKeyID = "artifact-signer"
	c.Reason = "operator review"
	revoked, receipt, err := o.Change(c, &first, key, at)
	if err != nil || receipt.PreviousDigest == "" {
		t.Fatal(receipt, err)
	}
	// The predecessor has expired; renewal authenticates its historical signature,
	// then requires a currently authorized, fresh replacement.
	later := at.Add(90 * time.Minute)
	c.Operation = "renew"
	c.Sequence++
	c.RevokeKeyID = ""
	c.Reason = ""
	c.IssuedAt = later.Format(time.RFC3339)
	c.ExpiresAt = later.Add(15 * time.Minute).Format(time.RFC3339)
	renewed, receipt, err := o.Change(c, &revoked, key, later)
	if err != nil {
		t.Fatal(receipt, err)
	}
	got, err := VerifyMetadata(renewed, roots, p.ArtifactDigest, later)
	if err != nil || len(got.Revocations) != 1 || got.Revocations[0].KeyIDs[0] != "artifact-signer" || got.Revocations[0].UpdatedAt != c.IssuedAt {
		t.Fatal(got, err)
	}
	before, _ := json.Marshal([]any{p.Provenance, p.SBOM})
	after, _ := json.Marshal([]any{got.Provenance, got.SBOM})
	if !bytes.Equal(before, after) || got.ArtifactDigest != p.ArtifactDigest {
		t.Fatal("renewal changed policy inputs or artifact")
	}
	if _, _, err = o.Change(c, &first, key, later); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatalf("old predecessor accepted: %v", err)
	}
}

func TestMetadataOperatorRejectsUnauthorizedFreshnessAndReplay(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	first, _ := SignMetadata(p, key)
	for _, name := range []string{"unauthorized", "wrong-key", "future", "expired", "same-sequence", "missing-prior", "future-prior", "wrong-artifact"} {
		t.Run(name, func(t *testing.T) {
			o := MetadataOperator{Roots: roots, State: MetadataState{Root: t.TempDir()}}
			c := MetadataChange{Operation: "renew", ArtifactDigest: p.ArtifactDigest, Sequence: 2, KeyID: p.KeyID, IssuedAt: at.Format(time.RFC3339), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339)}
			prior := &first
			signing := key
			want := CodeMetadataInvalid
			switch name {
			case "unauthorized":
				c.KeyID = "not-authorized"
				want = CodeMetadataAuthority
			case "wrong-key":
				_, signing, _ = ed25519.GenerateKey(nil)
			case "future":
				c.IssuedAt = at.Add(time.Minute).Format(time.RFC3339)
				want = CodeMetadataExpired
			case "expired":
				c.IssuedAt = at.Add(-time.Hour).Format(time.RFC3339)
				c.ExpiresAt = at.Add(-time.Minute).Format(time.RFC3339)
				want = CodeMetadataExpired
			case "same-sequence":
				c.Sequence = p.Sequence
				want = CodeMetadataRollback
			case "missing-prior":
				prior = nil
			case "future-prior":
				q := p
				q.IssuedAt = at.Add(time.Minute).Format(time.RFC3339)
				e, _ := SignMetadata(q, key)
				prior = &e
				want = CodeMetadataExpired
			case "wrong-artifact":
				c.ArtifactDigest = Digest([]byte("other"))
				want = CodeMetadataAuthority
			}
			_, r, err := o.Change(c, prior, signing, at)
			if !metadataCodeIs(err, want) || r.Valid || r.Retained {
				t.Fatalf("%s: %+v %v", want, r, err)
			}
		})
	}
}

func TestMetadataOperatorRotationAndRecoveryKeepJournal(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	state := MetadataState{Root: t.TempDir()}
	first, _ := SignMetadata(p, key)
	if err := state.Admit(p, first); err != nil {
		t.Fatal(err)
	}
	pub, rotated, _ := ed25519.GenerateKey(nil)
	roots.Keys["rotated"] = MetadataKey{PublicKey: base64.RawStdEncoding.EncodeToString(pub), NotBefore: at.Add(-time.Hour).Format(time.RFC3339), NotAfter: at.Add(2 * time.Hour).Format(time.RFC3339)}
	o := MetadataOperator{Roots: roots, State: state}
	c := MetadataChange{Operation: "renew", ArtifactDigest: p.ArtifactDigest, Sequence: 2, KeyID: "rotated", IssuedAt: at.Format(time.RFC3339), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339)}
	second, _, err := o.Change(c, &first, rotated, at)
	if err != nil {
		t.Fatal(err)
	}
	delete(roots.Keys, p.KeyID)
	if _, _, err := o.Change(c, &first, rotated, at); !metadataCodeIs(err, CodeMetadataAuthority) {
		t.Fatal(err)
	}
	c.Operation = "issue"
	c.Sequence = 3
	c.Provenance = p.Provenance
	c.SBOM = p.SBOM
	c.Revocations = []Revocation{{ArtifactDigests: []string{p.ArtifactDigest}, Reason: "recovered reviewed deny set"}}
	_, r, err := o.Change(c, nil, rotated, at)
	if err != nil || !r.Retained {
		t.Fatal(r, err)
	}
	// Recovery did not reset the journal or change scope/Artifact namespace.
	if err := state.Admit(p, first); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatal(err)
	}
	if _, err := o.Distribute(second, p.ArtifactDigest, NewFilesystemCarrier(t.TempDir()), at); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatal(err)
	}
}

type unavailableOperatorCarrier struct{}

func (unavailableOperatorCarrier) Read(string) ([]byte, error) { return nil, errors.New("unavailable") }
func (unavailableOperatorCarrier) Write(string, []byte) error  { return errors.New("unavailable") }
func TestMetadataOperatorTransportFailureRetainsCheckpoint(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	e, _ := SignMetadata(p, key)
	o := MetadataOperator{Roots: roots, State: MetadataState{Root: t.TempDir()}}
	r, err := o.Distribute(e, p.ArtifactDigest, unavailableOperatorCarrier{}, at)
	if err == nil || r.Valid || !r.Retained || r.Distributed || r.Code != "metadata-distribution" {
		t.Fatal(r, err)
	}
	carrier := NewFilesystemCarrier(t.TempDir())
	r, err = o.Distribute(e, p.ArtifactDigest, carrier, at)
	if err != nil || !r.Valid || !r.Distributed {
		t.Fatal(r, err)
	}
	p.Revocations = []Revocation{{KeyIDs: []string{"key"}, UpdatedAt: at.Format(time.RFC3339)}}
	conflict, _ := SignMetadata(p, key)
	if _, err = o.Distribute(conflict, p.ArtifactDigest, carrier, at); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatal(err)
	}
	got, err := ReadMetadata(carrier, p.ArtifactDigest)
	if err != nil || *got != e {
		t.Fatal(got, err)
	}
}

func TestMetadataOperatorIndependentProcesses(t *testing.T) {
	type request struct {
		Operator MetadataOperator
		Envelope MetadataEnvelope
		Artifact string
		Carrier  string
		At       time.Time
	}
	if input := os.Getenv("RCC_OPERATOR_PROCESS_INPUT"); input != "" {
		data, err := os.ReadFile(input)
		if err != nil {
			t.Fatal(err)
		}
		var q request
		if err = json.Unmarshal(data, &q); err != nil {
			t.Fatal(err)
		}
		r, err := q.Operator.Distribute(q.Envelope, q.Artifact, NewFilesystemCarrier(q.Carrier), q.At)
		if err != nil && !metadataCodeIs(err, CodeMetadataRollback) {
			t.Fatal(err)
		}
		if err = json.NewEncoder(os.Stdout).Encode(r); err != nil {
			t.Fatal(err)
		}
		return
	}
	p, roots, key, at := metadataFixture(t)
	root := t.TempDir()
	o := MetadataOperator{Roots: roots, State: MetadataState{Root: filepath.Join(root, "journal")}}
	carrier := filepath.Join(root, "carrier")
	exe, _ := os.Executable()
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	var highest MetadataEnvelope
	for i := 0; i < 8; i++ {
		p.Sequence = uint64(i + 1)
		e, _ := SignMetadata(p, key)
		highest = e
		data, _ := json.Marshal(request{o, e, p.ArtifactDigest, carrier, at})
		path := filepath.Join(root, string(rune('a'+i))+".json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		c := exec.Command(exe, "-test.run=^TestMetadataOperatorIndependentProcesses$")
		c.Env = append(os.Environ(), "RCC_OPERATOR_PROCESS_INPUT="+path)
		output := &bytes.Buffer{}
		c.Stdout = output
		c.Stderr = output
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, c)
		outputs = append(outputs, output)
	}
	for i, c := range commands {
		if err := c.Wait(); err != nil {
			t.Fatalf("%v %s", err, outputs[i].String())
		}
	}
	got, err := ReadMetadata(NewFilesystemCarrier(carrier), p.ArtifactDigest)
	if err != nil || *got != highest {
		t.Fatalf("concurrent distribution rolled carrier back: %v", err)
	}
	p.Sequence = 1
	old, _ := SignMetadata(p, key)
	if _, err = o.Distribute(old, p.ArtifactDigest, NewFilesystemCarrier(carrier), at); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatal(err)
	}
}

func TestMetadataOperatorNeverRepairsCorruptJournal(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	first, _ := SignMetadata(p, key)
	o := MetadataOperator{Roots: roots, State: MetadataState{Root: t.TempDir()}}
	carrier := NewFilesystemCarrier(t.TempDir())
	if _, err := o.Distribute(first, p.ArtifactDigest, carrier, at); err != nil {
		t.Fatal(err)
	}
	journals, err := filepath.Glob(filepath.Join(o.State.Root, "*.jsonl"))
	if err != nil || len(journals) != 1 {
		t.Fatal(journals, err)
	}
	original, err := os.ReadFile(journals[0])
	if err != nil {
		t.Fatal(err)
	}
	corrupt := append(original, []byte("{partial")...)
	if err = os.WriteFile(journals[0], corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	p.Sequence++
	next, _ := SignMetadata(p, key)
	r, err := o.Distribute(next, p.ArtifactDigest, carrier, at)
	if !metadataCodeIs(err, CodeMetadataState) || r.Retained || r.Valid {
		t.Fatal(r, err)
	}
	after, err := os.ReadFile(journals[0])
	if err != nil || !bytes.Equal(after, corrupt) {
		t.Fatal("operator modified corrupt journal", err)
	}
	got, err := ReadMetadata(carrier, p.ArtifactDigest)
	if err != nil || *got != first {
		t.Fatal("operator distributed after state failure", err)
	}
}
