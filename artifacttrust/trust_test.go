package artifacttrust

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemAndOfflineCarriersBindSameArtifact(t *testing.T) {
	root := t.TempDir()
	c := NewFilesystemCarrier(root)
	data := []byte(`{"mediaType":"application/vnd.rcc.environment.provenance.v1+json","artifactDigest":"sha256:a"}`)
	if err := PutAttachment(c, "sha256:a", "provenance", data); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "carrier.zip")
	if err := ExportArchive(c, archive, "sha256:a", []string{"provenance"}); err != nil {
		t.Fatal(err)
	}
	off, err := OpenArchiveCarrier(archive)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GetAttachment(off, "sha256:a", "provenance")
	if err != nil || string(got) != string(data) {
		t.Fatalf("offline=%s err=%v", got, err)
	}
}

func TestReceiptStoreDoesNotPersistSecrets(t *testing.T) {
	root := t.TempDir()
	store := NewReceiptStore(root)
	r := VerificationReceipt{Valid: true, Code: CodeValid, ArtifactDigest: "sha256:a", Diagnostic: "safe"}
	if err := store.Put(r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "sha256_a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || string(b) == "secret" {
		t.Fatal("unsafe receipt")
	}
}

func TestProvenanceAndSBOMBindToArtifact(t *testing.T) {
	p := Provenance{ArtifactDigest: "sha256:a", SpecificationDigest: "sha256:s", Platform: "linux/amd64", Builder: "b", RCCVersion: "v1", CreatedAt: FreshTimestamp(time.Unix(1, 0))}
	b, err := CanonicalProvenance(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttestationBinding(b, ProvenanceMediaType, "sha256:a"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttestationBinding(b, ProvenanceMediaType, "sha256:tampered"); err == nil {
		t.Fatal("tampered provenance accepted")
	}
}

func TestStrictRemoteRejectsUnsignedAndReceiptIsMachineReadable(t *testing.T) {
	p := Policy{Mode: StrictRemote}
	r := p.Verify(VerifyRequest{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "b"})
	if r.Valid || r.Code != CodeMetadataUnsigned {
		t.Fatalf("receipt=%+v", r)
	}
	if _, err := r.JSON(); err != nil {
		t.Fatal(err)
	}
}

func TestSignatureExpiryAndRevocation(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	s, _ := SignAt(Digest([]byte("signature-fixture")), "k", priv, time.Unix(1, 0), time.Unix(2, 0))
	p := Policy{Mode: StrictRemote, AcceptedKeys: []string{"k"}}
	r := p.Verify(authenticatedTestRequest(t, VerifyRequest{ArtifactDigest: Digest([]byte("signature-fixture")), Platform: "linux/amd64", Builder: "b", Signatures: []Signature{s}, Keys: map[string]ed25519.PublicKey{"k": pub}, At: time.Unix(3, 0)}))
	if r.Valid || r.Code != CodeExpired {
		t.Fatalf("receipt=%+v", r)
	}
}

func TestSignatureNotBeforeIsRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now().UTC()
	s, _ := SignAt(Digest([]byte("signature-fixture")), "k", priv, now.Add(time.Hour), now.Add(2*time.Hour))
	p := Policy{Mode: StrictRemote, AcceptedKeys: []string{"k"}}
	r := p.Verify(authenticatedTestRequest(t, VerifyRequest{ArtifactDigest: Digest([]byte("signature-fixture")), Platform: "linux/amd64", Builder: "b", Signatures: []Signature{s}, Keys: map[string]ed25519.PublicKey{"k": pub}, At: now}))
	if r.Valid || r.Code != CodeInvalid {
		t.Fatalf("receipt=%+v", r)
	}
}

func TestSignatureValidityWindowAndKeyIdentityAreSigned(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(nil)
	signature, err := SignAt("sha256:a", "k", private, time.Unix(1, 0), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	tampered := signature
	tampered.NotAfter = FreshTimestamp(time.Unix(200, 0))
	if err := VerifySignature(tampered, public, "sha256:a"); err == nil {
		t.Fatal("signature validity window was not signed")
	}
	tampered = signature
	tampered.KeyID = "other"
	if err := VerifySignature(tampered, public, "sha256:a"); err == nil {
		t.Fatal("signature key identity was not signed")
	}
}

func TestSBOMIsDeterministicallyOrderedAndBound(t *testing.T) {
	first, a, err := NewSBOM("sha256:artifact", []Component{{Name: "z", Version: "1", PackageType: "p"}, {Name: "a", Version: "2", PackageType: "p"}})
	if err != nil || first.Components[0].Name != "a" {
		t.Fatalf("SBOM: %#v %v", first, err)
	}
	_, b, err := NewSBOM("sha256:artifact", []Component{{Name: "a", Version: "2", PackageType: "p"}, {Name: "z", Version: "1", PackageType: "p"}})
	if err != nil || string(a) != string(b) {
		t.Fatalf("SBOM is not deterministic")
	}
}

func TestGeneratedInventoryDoesNotClaimSPDX(t *testing.T) {
	inventory, _, err := NewObjectInventory("sha256:artifact", []Component{{Name: "rcc", Version: "1", PackageType: "rcc-builder"}})
	if err != nil {
		t.Fatal(err)
	}
	if inventory.MediaType != "application/vnd.rcc.environment.inventory.v1+json" {
		t.Fatalf("custom object inventory has misleading media type %q", inventory.MediaType)
	}
}

func TestLegacySPDXLabeledInventoryRemainsVerifiableAndBytePreserved(t *testing.T) {
	artifact := "sha256:legacy"
	legacy := []byte(`{"mediaType":"application/vnd.rcc.environment.sbom.spdx+json","artifactDigest":"sha256:legacy","components":[{"name":"hololib-object","version":"sha256:object","packageType":"rcc-hololib-object-v12"}]}`)
	carrier := NewFilesystemCarrier(t.TempDir())
	if err := PutAttachment(carrier, artifact, "sbom", legacy); err != nil {
		t.Fatalf("write legacy attachment: %v", err)
	}
	got, err := GetAttachment(carrier, artifact, "sbom")
	if err != nil || !bytes.Equal(got, legacy) {
		t.Fatalf("legacy attachment changed or failed validation: bytesEqual=%t err=%v", bytes.Equal(got, legacy), err)
	}
	var inventory ObjectInventory
	if err := decodeStrict(got, &inventory); err != nil {
		t.Fatal(err)
	}
	receipt := (Policy{Mode: PermissiveLocal, AllowUnsignedLocal: true}).Verify(VerifyRequest{ArtifactDigest: artifact, SBOM: &inventory})
	if !receipt.Valid {
		t.Fatalf("legacy inventory rejected by verifier: %+v", receipt)
	}
	misbound := bytes.Replace(legacy, []byte(`"artifactDigest":"sha256:legacy"`), []byte(`"artifactDigest":"sha256:other"`), 1)
	if err := PutAttachment(NewFilesystemCarrier(t.TempDir()), artifact, "sbom", misbound); err == nil {
		t.Fatal("misbound legacy inventory accepted")
	}
	credentialBearing := []byte(`{"mediaType":"application/vnd.rcc.environment.sbom.spdx+json","artifactDigest":"sha256:legacy","components":[{"name":"pkg","version":"1","packageType":"pip","source":"https://example.invalid/simple?token=secret"}]}`)
	if err := PutAttachment(NewFilesystemCarrier(t.TempDir()), artifact, "sbom", credentialBearing); err == nil {
		t.Fatal("credential-bearing legacy inventory accepted")
	}

	migrated, _, err := NewObjectInventory(artifact, inventory.Components)
	if err != nil || migrated.MediaType != ObjectInventoryMediaType {
		t.Fatalf("migration did not produce the RCC inventory contract: inventory=%+v err=%v", migrated, err)
	}
}

func TestAttestationsRejectCredentialBearingSources(t *testing.T) {
	p := Provenance{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "b", RCCVersion: "v1", CreatedAt: FreshTimestamp(time.Now()), DependencySources: []string{"https://user:password@example.invalid/simple"}}
	if _, err := CanonicalProvenance(p); err == nil {
		t.Fatal("credential-bearing provenance accepted")
	}
	if _, _, err := NewSBOM("sha256:a", []Component{{Name: "x", Version: "1", PackageType: "p", Source: "https://token=secret@example.invalid/x"}}); err == nil {
		t.Fatal("credential-bearing SBOM accepted")
	}
	if _, err := CanonicalProvenance(Provenance{ArtifactDigest: "sha256:a", BuildIdentity: "Authorization: Bearer secret"}); err == nil {
		t.Fatal("credential-bearing build identity accepted")
	}
	if _, _, err := NewSBOM("sha256:a", []Component{{Name: "x", Version: "1", PackageType: "p", Source: "https://example.invalid/x?sig=secret"}}); err == nil {
		t.Fatal("credential-bearing SBOM query accepted")
	}
}

func TestPolicySignatureAndRevocation(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(nil)
	artifact := Digest([]byte("signed-artifact"))
	sig, err := Sign(artifact, "build-key", private)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{Mode: StrictRemote, RequireSignature: true, AcceptedKeys: []string{"build-key"}, AcceptedBuilders: []string{"builder-v1"}}
	keys := map[string]ed25519.PublicKey{"build-key": public}
	if err := policy.Evaluate(false, artifact, "linux/amd64", "builder-v1", []Signature{sig}, nil, keys); !metadataCodeIs(err, CodeMetadataUnsigned) {
		t.Fatalf("signature-only strict admission: %v", err)
	}
	q := VerifyRequest{ArtifactDigest: artifact, Platform: "linux/amd64", Builder: "builder-v1", Signatures: []Signature{sig}, Keys: keys}
	if r := policy.Verify(authenticatedTestRequest(t, q)); !r.Valid {
		t.Fatalf("authenticated strict decision: %+v", r)
	}
	q.Revocations = []Revocation{{KeyIDs: []string{"build-key"}, Reason: "incident"}}
	if r := policy.Verify(authenticatedTestRequest(t, q)); r.Valid || r.Code != CodeRevoked {
		t.Fatalf("revoked signer: %+v", r)
	}
}

func TestUnsignedLocalPolicy(t *testing.T) {
	p := Policy{Mode: PermissiveLocal, AllowUnsignedLocal: true}
	if err := p.Evaluate(true, "sha256:artifact", "linux/amd64", "builder", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Evaluate(false, "sha256:artifact", "linux/amd64", "builder", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyUsesRequestTimeAndBindsProvenanceIdentity(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	p := Policy{Mode: PermissiveLocal, AcceptedBuilders: []string{"builder-v1"}, AcceptedPlatforms: []string{"linux/amd64"}}
	provenance := &Provenance{
		MediaType: ProvenanceMediaType, ArtifactDigest: "sha256:a", Platform: "linux/amd64",
		Builder: "builder-v1", RCCVersion: "v1", CreatedAt: FreshTimestamp(now),
	}
	r := p.Verify(VerifyRequest{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder-v1", Provenance: provenance, At: now})
	if !r.Valid || r.VerifiedAt != FreshTimestamp(now) || r.PolicyRevision == "" || r.DecisionID == "" {
		t.Fatalf("receipt=%+v", r)
	}
	provenance.Builder = "other-builder"
	r = p.Verify(VerifyRequest{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder-v1", Provenance: provenance, At: now})
	if r.Valid || r.Code != CodeBinding {
		t.Fatalf("mismatched builder receipt=%+v", r)
	}
}

func TestStrictDefaultDoesNotAcceptUnsignedVerifyRequest(t *testing.T) {
	r := (Policy{}).Verify(VerifyRequest{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder", At: time.Unix(1, 0)})
	if r.Valid || r.Code != CodeMetadataUnsigned || r.PolicyMode != StrictRemote {
		t.Fatalf("receipt=%+v", r)
	}
}

func TestAcceptedDependencySourcesAreEnforced(t *testing.T) {
	p := Policy{Mode: PermissiveLocal, AcceptedDependencySources: []string{"https://pypi.org/simple"}}
	provenance := &Provenance{
		MediaType: ProvenanceMediaType, ArtifactDigest: "sha256:a", Platform: "linux/amd64",
		Builder: "builder", CreatedAt: FreshTimestamp(time.Unix(1, 0)),
		DependencySources: []string{"https://evil.invalid/simple"},
	}
	r := p.Verify(VerifyRequest{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder", Provenance: provenance, At: time.Unix(2, 0)})
	if r.Valid || r.Code != CodePolicy {
		t.Fatalf("receipt=%+v", r)
	}
}

func TestFailClosedRevocationsRejectMissingAndStaleSnapshots(t *testing.T) {
	at := time.Unix(100, 0).UTC()
	p := Policy{Mode: PermissiveLocal, FailClosedRevocations: true, RevocationMaxAge: time.Hour}
	missing := p.Verify(VerifyRequest{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder", At: at})
	if missing.Valid || missing.Code != CodeRevocationStale {
		t.Fatalf("missing snapshot receipt=%+v", missing)
	}
	stale := p.Verify(VerifyRequest{
		ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder", At: at,
		Revocations: []Revocation{{UpdatedAt: FreshTimestamp(at.Add(-2 * time.Hour))}},
	})
	if stale.Valid || stale.Code != CodeRevocationStale {
		t.Fatalf("stale snapshot receipt=%+v", stale)
	}
	freshEmpty := p.Verify(VerifyRequest{ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder", At: at, RevocationFetchedAt: at})
	if !freshEmpty.Valid || freshEmpty.Code != CodeValid {
		t.Fatalf("fresh empty snapshot receipt=%+v", freshEmpty)
	}
}

func TestArtifactRevocationOverridesPermissiveUnsignedLocal(t *testing.T) {
	p := Policy{Mode: PermissiveLocal, AllowUnsignedLocal: true}
	r := p.Verify(VerifyRequest{
		ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder", At: time.Unix(1, 0),
		Revocations: []Revocation{{ArtifactDigests: []string{"sha256:a"}, UpdatedAt: FreshTimestamp(time.Unix(1, 0))}},
	})
	if r.Valid || r.Code != CodeRevoked {
		t.Fatalf("receipt=%+v", r)
	}
}

func TestSignatureBundleIsCanonicalAndArtifactBound(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(nil)
	sig, err := Sign("sha256:a", "build-key", private)
	if err != nil {
		t.Fatal(err)
	}
	bundle, data, err := NewSignatureBundle("sha256:a", []Signature{sig})
	if err != nil || bundle.ArtifactDigest != "sha256:a" {
		t.Fatalf("bundle=%+v err=%v", bundle, err)
	}
	if err := VerifyAttestationBinding(data, SignatureMediaType, "sha256:a"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttestationBinding(data, SignatureMediaType, "sha256:other"); err == nil {
		t.Fatal("mismatched signature bundle accepted")
	}
}

func TestReceiptStoreAppendsHistoryAndPreservesTrustLinkage(t *testing.T) {
	root := t.TempDir()
	store := NewReceiptStore(root)
	first := VerificationReceipt{
		Valid: true, Code: CodeValid, ArtifactDigest: "sha256:a", VerifiedAt: FreshTimestamp(time.Unix(1, 0)),
		PolicyRevision: "policy-1", KeyID: "key-1", RevocationSnapshot: "sha256:rev-1",
	}
	second := first
	second.VerifiedAt = FreshTimestamp(time.Unix(2, 0))
	second.LeaseID = "lease-2"
	if err := store.Put(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(second); err != nil {
		t.Fatal(err)
	}
	history, err := store.History("sha256:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[1].LeaseID != "lease-2" || history[1].KeyID != "key-1" {
		t.Fatalf("history=%+v", history)
	}
}

func TestVerificationAndReceiptDoNotEchoCredentialBearingInputs(t *testing.T) {
	r := (Policy{}).Verify(VerifyRequest{
		ArtifactDigest: "sha256:a", Platform: "linux/amd64", Builder: "builder", RevocationSource: "https://token=secret@example.invalid", At: time.Unix(1, 0),
	})
	data, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("secret")) || bytes.Contains(data, []byte("token=")) {
		t.Fatalf("receipt leaked credential: %s", data)
	}
}

func TestPersistedReceiptRedactsProviderAndBuildDiagnostics(t *testing.T) {
	root := t.TempDir()
	store := NewReceiptStore(root)
	receipt := (Policy{}).FailureReceipt(
		"sha256:a", "linux/amd64", "builder", CodeInvalid,
		"provider build failed: Authorization: Bearer provider-secret", time.Unix(1, 0),
	)
	if err := store.Put(receipt); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "sha256_a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("provider-secret")) || bytes.Contains(data, []byte("Authorization:")) {
		t.Fatalf("persisted receipt leaked provider/build diagnostic: %s", data)
	}
	if !bytes.Contains(data, []byte("trust attachment could not be decoded")) {
		t.Fatalf("persisted receipt lacks bounded diagnostic: %s", data)
	}
}

func TestStrictTrustBundleEvaluatesAttestationsAndFailureReceipt(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	artifact := Digest([]byte("bundle"))
	const (
		platform = "linux/amd64"
		builder  = "builder-v1"
	)
	at := time.Unix(100, 0).UTC()
	provenance := &Provenance{
		MediaType: ProvenanceMediaType, ArtifactDigest: artifact, Platform: platform,
		Builder: builder, RCCVersion: "v1", CreatedAt: FreshTimestamp(at),
	}
	sbom, _, err := NewSBOM(artifact, []Component{{Name: "python", Version: "3.10", PackageType: "conda"}})
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign(artifact, "build-key", private)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{Mode: StrictRemote, AcceptedKeys: []string{"build-key"}, AcceptedPlatforms: []string{platform}, AcceptedBuilders: []string{builder}}
	request := VerifyRequest{
		ArtifactDigest: artifact, Platform: platform, Builder: builder,
		Provenance: provenance, SBOM: &sbom, Signatures: []Signature{signature},
		Keys: map[string]ed25519.PublicKey{"build-key": public}, At: at,
	}
	valid := policy.Verify(authenticatedTestRequest(t, request))
	if !valid.Valid || valid.Code != CodeValid || valid.ProvenanceDigest == "" || valid.SBOMDigest == "" || valid.KeyID != "build-key" {
		t.Fatalf("valid trust bundle receipt=%+v", valid)
	}

	tampered := request
	tampered.Provenance = &Provenance{}
	*tampered.Provenance = *provenance
	tampered.Provenance.Builder = "untrusted-builder"
	tamperedReceipt := policy.Verify(authenticatedTestRequest(t, tampered))
	if tamperedReceipt.Valid || tamperedReceipt.Code != CodeBinding {
		t.Fatalf("tampered trust bundle receipt=%+v", tamperedReceipt)
	}

	revoked := request
	revoked.Revocations = []Revocation{{KeyIDs: []string{"build-key"}, UpdatedAt: FreshTimestamp(at)}}
	revokedReceipt := policy.Verify(authenticatedTestRequest(t, revoked))
	if revokedReceipt.Valid || revokedReceipt.Code != CodeRevoked {
		t.Fatalf("revoked trust bundle receipt=%+v", revokedReceipt)
	}

	failure := policy.FailureReceipt(artifact, platform, builder, CodeInvalid, "provider build failed: Authorization: Bearer bundle-secret", at)
	data, err := failure.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("bundle-secret")) || bytes.Contains(data, []byte("Authorization:")) || !bytes.Contains(data, []byte("trust attachment could not be decoded")) {
		t.Fatalf("redacted failure receipt=%s", data)
	}
}
