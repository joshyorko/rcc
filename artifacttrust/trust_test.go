package artifacttrust

import (
	"crypto/ed25519"
	"testing"
)

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

func TestPolicySignatureAndRevocation(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(nil)
	sig, err := Sign("sha256:artifact", "build-key", private)
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{RequireSignature: true, AcceptedKeys: []string{"build-key"}, AcceptedBuilders: []string{"builder-v1"}}
	keys := map[string]ed25519.PublicKey{"build-key": public}
	if err := policy.Evaluate(false, "sha256:artifact", "linux/amd64", "builder-v1", []Signature{sig}, nil, keys); err != nil {
		t.Fatal(err)
	}
	revoked := []Revocation{{KeyIDs: []string{"build-key"}, Reason: "incident"}}
	if err := policy.Evaluate(false, "sha256:artifact", "linux/amd64", "builder-v1", []Signature{sig}, revoked, keys); err == nil {
		t.Fatal("revoked signer accepted")
	}
}

func TestUnsignedLocalPolicy(t *testing.T) {
	p := Policy{AllowUnsignedLocal: true}
	if err := p.Evaluate(true, "sha256:artifact", "linux/amd64", "builder", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.Evaluate(false, "sha256:artifact", "linux/amd64", "builder", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}
