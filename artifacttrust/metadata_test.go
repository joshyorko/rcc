package artifacttrust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func metadataFixture(t *testing.T) (MetadataPayload, MetadataRoots, ed25519.PrivateKey, time.Time) {
	t.Helper()
	at := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	artifact := Digest([]byte("immutable Artifact"))
	p := MetadataPayload{SchemaVersion: 2, Scope: "deployment-A", ArtifactDigest: artifact, Sequence: 1, KeyID: "metadata-A", IssuedAt: at.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), Provenance: &Provenance{MediaType: ProvenanceMediaType, ArtifactDigest: artifact, SourceRepository: "allowed", DependencySources: []string{"allowed"}}, SBOM: &SBOM{MediaType: SBOMMediaType, ArtifactDigest: artifact, Components: []Component{{Name: "pkg", Version: "1"}}}, Revocations: []Revocation{}}
	roots := MetadataRoots{SchemaVersion: 2, Scope: p.Scope, MinimumSequence: 1, Keys: map[string]MetadataKey{p.KeyID: {PublicKey: base64.RawStdEncoding.EncodeToString(pub), NotBefore: at.Add(-time.Hour).Format(time.RFC3339), NotAfter: at.Add(2 * time.Hour).Format(time.RFC3339)}}}
	return p, roots, key, at
}

func TestMetadataEnvelopeAuthenticatesAllPolicyInputs(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	e, err := SignMetadata(p, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyMetadata(e, roots, p.ArtifactDigest, at); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*MetadataPayload){
		"source":       func(p *MetadataPayload) { p.Provenance.SourceRepository = "attacker" },
		"dependencies": func(p *MetadataPayload) { p.Provenance.DependencySources = []string{"attacker"} },
		"sbom":         func(p *MetadataPayload) { p.SBOM.Components[0].Version = "hidden" },
		"revocations": func(p *MetadataPayload) {
			p.Revocations = []Revocation{{KeyIDs: []string{"artifact-signer"}, UpdatedAt: p.IssuedAt}}
		},
		"sequence": func(p *MetadataPayload) { p.Sequence++ },
		"issued":   func(p *MetadataPayload) { p.IssuedAt = at.Format(time.RFC3339) },
		"expiry":   func(p *MetadataPayload) { p.ExpiresAt = at.Add(30 * time.Minute).Format(time.RFC3339) },
		"scope":    func(p *MetadataPayload) { p.Scope = "deployment-B" },
		"artifact": func(p *MetadataPayload) {
			p.ArtifactDigest = Digest([]byte("other"))
			p.Provenance.ArtifactDigest = p.ArtifactDigest
			p.SBOM.ArtifactDigest = p.ArtifactDigest
		},
	} {
		t.Run(name, func(t *testing.T) {
			data, _ := base64.RawStdEncoding.DecodeString(e.Payload)
			var modified MetadataPayload
			if err := json.Unmarshal(data, &modified); err != nil {
				t.Fatal(err)
			}
			mutate(&modified)
			data, _ = json.Marshal(modified)
			bad := e
			bad.Payload = base64.RawStdEncoding.EncodeToString(data)
			if _, err := VerifyMetadata(bad, roots, p.ArtifactDigest, at); err == nil {
				t.Fatal("unsigned mutation accepted")
			}
		})
	}
}

func TestLegacyInventoryMetadataV2IsVerifiedWithoutRewritingSignedBytes(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	p.Provenance = nil
	p.SBOM.MediaType = LegacySBOMMediaType
	envelope, err := SignMetadata(p, key)
	if err != nil {
		t.Fatal(err)
	}
	signedBytes, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	carrier := NewFilesystemCarrier(t.TempDir())
	if err := PutAttachment(carrier, p.ArtifactDigest, "metadata", signedBytes); err != nil {
		t.Fatal(err)
	}
	readBytes, err := GetAttachment(carrier, p.ArtifactDigest, "metadata")
	if err != nil || !bytes.Equal(readBytes, signedBytes) {
		t.Fatalf("signed metadata bytes changed: equal=%t err=%v", bytes.Equal(readBytes, signedBytes), err)
	}
	var readEnvelope MetadataEnvelope
	if err := json.Unmarshal(readBytes, &readEnvelope); err != nil {
		t.Fatal(err)
	}
	policy := Policy{Mode: PermissiveLocal, AllowUnsignedLocal: true}
	receipt := policy.Verify(VerifyRequest{
		Metadata: &readEnvelope, MetadataRoots: &roots, MetadataState: &MetadataState{Root: t.TempDir()},
		ArtifactDigest: p.ArtifactDigest, Platform: "linux/amd64", Builder: "builder", At: at,
	})
	if !receipt.Valid {
		t.Fatalf("legacy v2 metadata inventory rejected: %+v", receipt)
	}
}

func TestMetadataValidityAndIndependentAuthority(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	e, _ := SignMetadata(p, key)
	for name, when := range map[string]time.Time{"future": at.Add(-2 * time.Minute), "expired": at.Add(time.Hour)} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyMetadata(e, roots, p.ArtifactDigest, when); err == nil {
				t.Fatal("invalid time accepted")
			}
		})
	}
	delete(roots.Keys, p.KeyID)
	if _, err := VerifyMetadata(e, roots, p.ArtifactDigest, at); err == nil {
		t.Fatal("artifact signer or carrier must not supply metadata authority")
	}
}

func TestMetadataSignerRejectsPayloadTooLargeForVerifier(t *testing.T) {
	p, _, key, _ := metadataFixture(t)
	p.Provenance.SourceRepository = strings.Repeat("a", 2<<20)
	if _, err := SignMetadata(p, key); !metadataCodeIs(err, CodeMetadataInvalid) {
		t.Fatalf("signer emitted unverifiable oversized payload: %v", err)
	}
}

func TestMetadataRollbackSurvivesRestartAndKeyRotation(t *testing.T) {
	p, roots, key, at := metadataFixture(t)
	root := t.TempDir()
	first, _ := SignMetadata(p, key)
	if err := (MetadataState{Root: root}).Admit(p, first); err != nil {
		t.Fatal(err)
	}
	pub, newKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rotated := p
	rotated.Sequence = 2
	rotated.KeyID = "metadata-B"
	roots.Keys[rotated.KeyID] = MetadataKey{PublicKey: base64.RawStdEncoding.EncodeToString(pub), NotBefore: at.Add(-time.Hour).Format(time.RFC3339), NotAfter: at.Add(2 * time.Hour).Format(time.RFC3339)}
	second, _ := SignMetadata(rotated, newKey)
	verified, err := VerifyMetadata(second, roots, p.ArtifactDigest, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := (MetadataState{Root: root}).Admit(verified, second); err != nil {
		t.Fatal(err)
	}
	if err := (MetadataState{Root: root}).Admit(p, first); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatalf("old-key rollback: %v", err)
	}
	equivocal := rotated
	equivocal.Revocations = []Revocation{{KeyIDs: []string{"artifact-signer"}, UpdatedAt: rotated.IssuedAt}}
	other, _ := SignMetadata(equivocal, newKey)
	if err := (MetadataState{Root: root}).Admit(equivocal, other); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatalf("same-sequence equivocation: %v", err)
	}
	delete(roots.Keys, p.KeyID)
	if _, err := VerifyMetadata(first, roots, p.ArtifactDigest, at); !metadataCodeIs(err, CodeMetadataAuthority) {
		t.Fatalf("retired key: %v", err)
	}
	roots.MinimumSequence = 3
	if _, err := VerifyMetadata(second, roots, p.ArtifactDigest, at); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatalf("bootstrap floor: %v", err)
	}
}

func TestMetadataPartialJournalFailsClosed(t *testing.T) {
	p, _, key, _ := metadataFixture(t)
	e, _ := SignMetadata(p, key)
	root := t.TempDir()
	if err := (MetadataState{Root: root}).Admit(p, e); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	f, err := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"sequence\":2")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (MetadataState{Root: root}).Admit(p, e); !metadataCodeIs(err, CodeMetadataState) {
		t.Fatalf("partial journal accepted: %v", err)
	}
}

func metadataCodeIs(err error, code string) bool {
	var failure *MetadataError
	return errors.As(err, &failure) && failure.Code == code
}

func authenticatedTestRequest(t *testing.T, q VerifyRequest) VerifyRequest {
	t.Helper()
	if q.At.IsZero() {
		q.At = time.Now().UTC()
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := MetadataPayload{SchemaVersion: 2, Scope: "test-authority", ArtifactDigest: q.ArtifactDigest, Sequence: 1, KeyID: "metadata-key", IssuedAt: q.At.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: q.At.Add(time.Hour).Format(time.RFC3339), Provenance: q.Provenance, SBOM: q.SBOM, Revocations: q.Revocations}
	e, err := SignMetadata(p, key)
	if err != nil {
		t.Fatal(err)
	}
	q.Metadata = &e
	q.MetadataRoots = &MetadataRoots{SchemaVersion: 2, Scope: p.Scope, MinimumSequence: 1, Keys: map[string]MetadataKey{p.KeyID: {PublicKey: base64.RawStdEncoding.EncodeToString(pub), NotBefore: q.At.Add(-time.Hour).Format(time.RFC3339), NotAfter: q.At.Add(2 * time.Hour).Format(time.RFC3339)}}}
	q.MetadataState = &MetadataState{Root: t.TempDir()}
	return q
}

func TestStrictPolicyCommitsAuthenticatedRevocationBeforeDenying(t *testing.T) {
	p, roots, metadataKey, at := metadataFixture(t)
	p.Provenance.Platform, p.Provenance.Builder = "linux_amd64", "fixture"
	pub, artifactKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := SignAt(p.ArtifactDigest, "artifact-key", artifactKey, at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	first, err := SignMetadata(p, metadataKey)
	if err != nil {
		t.Fatal(err)
	}
	state := MetadataState{Root: t.TempDir()}
	q := VerifyRequest{ArtifactDigest: p.ArtifactDigest, Platform: p.Provenance.Platform, Builder: p.Provenance.Builder, At: at, Keys: map[string]ed25519.PublicKey{"artifact-key": pub}, Signatures: []Signature{sig}, Metadata: &first, MetadataRoots: &roots, MetadataState: &state}
	policy := Policy{Mode: StrictRemote, FailClosedRevocations: true, AcceptedSources: []string{"allowed"}}
	accepted := policy.Verify(q)
	if !accepted.Valid || accepted.MetadataVersion != 2 {
		t.Fatalf("signed metadata rejected: %+v", accepted)
	}
	p.Sequence = 2
	p.Revocations = []Revocation{{KeyIDs: []string{"artifact-key"}, UpdatedAt: at.Format(time.RFC3339)}}
	revoked, err := SignMetadata(p, metadataKey)
	if err != nil {
		t.Fatal(err)
	}
	q.Metadata = &revoked
	denied := policy.Verify(q)
	if denied.Valid || denied.Code != CodeRevoked {
		t.Fatalf("authenticated revocation: %+v", denied)
	}
	q.Metadata = &first
	rollback := policy.Verify(q)
	if rollback.Valid || rollback.Code != CodeMetadataRollback {
		t.Fatalf("revocation rollback: %+v", rollback)
	}
	q.Metadata = nil
	legacy := policy.Verify(q)
	if legacy.Valid || legacy.Code != CodeMetadataUnsigned {
		t.Fatalf("unsigned metadata downgrade: %+v", legacy)
	}
	if accepted.DecisionID == denied.DecisionID || denied.DecisionID == rollback.DecisionID {
		t.Fatal("metadata decisions lost receipt identity")
	}
}

func TestMetadataDecisionBindsDeploymentAuthorityAndRequiredProvenance(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := Digest([]byte("authority receipt fixture"))
	sig, err := Sign(digest, "artifact", key)
	if err != nil {
		t.Fatal(err)
	}
	q := authenticatedTestRequest(t, VerifyRequest{ArtifactDigest: digest, Keys: map[string]ed25519.PublicKey{"artifact": pub}, Signatures: []Signature{sig}})
	policy := Policy{Mode: StrictRemote}
	first := policy.Verify(q)
	if !first.Valid || first.MetadataAuthorityDigest == "" {
		t.Fatalf("authority receipt: %+v", first)
	}
	q.MetadataRoots.Keys["authorized-overlap"] = q.MetadataRoots.Keys["metadata-key"]
	second := policy.Verify(q)
	if !second.Valid || first.DecisionID == second.DecisionID || first.MetadataAuthorityDigest == second.MetadataAuthorityDigest {
		t.Fatal("authority configuration change was not bound to decision")
	}
	policy.AcceptedSources = []string{"required-source"}
	if receipt := policy.Verify(q); receipt.Valid || receipt.Code != CodePolicy {
		t.Fatalf("missing required provenance: %+v", receipt)
	}
}

func TestCoordinationStatementVerificationCannotGrantMetadataPolicy(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := Digest([]byte("complete build statement"))
	sig, err := Sign(digest, "build", key)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"build": pub}
	policy := Policy{Mode: PermissiveLocal, AllowUnsignedLocal: true}
	if err := policy.VerifyStatement(digest, "platform", "builder", nil, nil, keys); err == nil {
		t.Fatal("statement verification allowed unsigned input")
	}
	policy = Policy{Mode: StrictRemote, AcceptedSources: []string{"source"}}
	if err := policy.VerifyStatement(digest, "platform", "builder", []Signature{sig}, nil, keys); err == nil {
		t.Fatal("statement verification bypassed metadata policy")
	}
}
