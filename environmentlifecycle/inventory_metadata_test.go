package environmentlifecycle

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
)

// A JAT-sized object set must fit the existing authenticated metadata bounds
// without dropping objects or treating content hashes as software versions.
func TestGeneratedJATClassInventoryFitsAuthenticatedV2(t *testing.T) {
	const objects = 6173
	artifact := environmentartifact.DigestBytes([]byte("large inventory artifact"))
	manifest := environmentartifact.Manifest{ArtifactDigest: artifact, Builder: environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: "v18.19.9"}}
	source := environmentartifact.Inventory{}
	hashes := map[string]string{}
	for i := 0; i < objects; i++ {
		name := fmt.Sprintf("%064x", i+1)
		digest := environmentartifact.DigestBytes([]byte("stored object " + name))
		source.Index.Entries = append(source.Index.Entries, environmentartifact.ObjectEntry{LegacyObjectID: name, StoredDigest: digest})
		hashes[name] = digest.String()
	}
	inventory, err := generatedObjectInventory(manifest, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Components) != objects+1 {
		t.Fatal("generated inventory dropped objects")
	}
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	now := time.Now().UTC().Truncate(time.Second)
	payload := artifacttrust.MetadataPayload{SchemaVersion: 2, Scope: "inventory-size-regression", ArtifactDigest: artifact.String(), Sequence: 1, KeyID: "fixture",
		IssuedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), SBOM: &inventory, Revocations: []artifacttrust.Revocation{}}
	envelope, err := artifacttrust.SignMetadata(payload, key)
	if err != nil {
		t.Fatalf("complete %d-object inventory does not fit authenticated v2: %v", objects, err)
	}
	roots := artifacttrust.MetadataRoots{SchemaVersion: 2, Scope: payload.Scope, MinimumSequence: 1, Keys: map[string]artifacttrust.MetadataKey{
		"fixture": {PublicKey: base64.RawStdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)), NotBefore: now.Add(-time.Hour).Format(time.RFC3339), NotAfter: now.Add(2 * time.Hour).Format(time.RFC3339)},
	}}
	verified, err := artifacttrust.VerifyMetadata(envelope, roots, artifact.String(), now)
	if err != nil || verified.SBOM == nil || len(verified.SBOM.Components) != objects+1 {
		t.Fatalf("complete inventory did not authenticate: %v", err)
	}
	for _, component := range verified.SBOM.Components {
		if component.PackageType == "rcc-builder" {
			continue
		}
		if component.Hash != hashes[component.Name] || component.Version != "" {
			t.Fatal("object hash was lost or presented as a software version")
		}
	}
}
