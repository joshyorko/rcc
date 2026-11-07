// Package trustfixture builds disposable, independently signed test metadata.
// It is imported only by tests; no fixture keys or state are production roots.
package trustfixture

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
)

func Authenticate(t testing.TB, q artifacttrust.VerifyRequest) artifacttrust.VerifyRequest {
	t.Helper()
	if q.At.IsZero() {
		q.At = time.Now().UTC()
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := artifacttrust.MetadataPayload{SchemaVersion: 2, Scope: "fixture-authority", ArtifactDigest: q.ArtifactDigest, Sequence: 1, KeyID: "metadata-fixture", IssuedAt: q.At.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: q.At.Add(time.Hour).Format(time.RFC3339), Provenance: q.Provenance, SBOM: q.SBOM, Revocations: q.Revocations}
	e, err := artifacttrust.SignMetadata(p, private)
	if err != nil {
		t.Fatal(err)
	}
	q.Metadata = &e
	q.MetadataRoots = &artifacttrust.MetadataRoots{SchemaVersion: 2, Scope: p.Scope, MinimumSequence: 1, Keys: map[string]artifacttrust.MetadataKey{p.KeyID: {PublicKey: base64.RawStdEncoding.EncodeToString(public), NotBefore: q.At.Add(-time.Hour).Format(time.RFC3339), NotAfter: q.At.Add(2 * time.Hour).Format(time.RFC3339)}}}
	q.MetadataState = &artifacttrust.MetadataState{Root: t.TempDir()}
	return q
}

func WriteJSON(t testing.TB, root, name string, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func Put(t testing.TB, carrier artifacttrust.Carrier, q artifacttrust.VerifyRequest) {
	t.Helper()
	data, err := json.Marshal(q.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := artifacttrust.PutAttachment(carrier, q.ArtifactDigest, "metadata", data); err != nil {
		t.Fatal(err)
	}
}
