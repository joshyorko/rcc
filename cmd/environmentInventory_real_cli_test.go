package cmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/internal/trustfixture"
)

// Old consumers require explicitly selected legacy inventory bytes; new media
// must not be silently relabeled. Each client retains its journal across cases.
func TestObjectInventoryMigrationRealCLI(t *testing.T) {
	current, previous := os.Getenv("RCC_ACCEPTANCE_BINARY"), os.Getenv("RCC_INVENTORY_LEGACY_BINARY")
	if current == "" || previous == "" {
		t.Skip("set exact candidate and released v18.19.9 binaries")
	}
	evidence := os.Getenv("RCC_INVENTORY_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	}
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for label, binary := range map[string]string{"current": current, "previous": previous} {
		content, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		trustfixture.WriteJSON(t, evidence, label+"-binary.json", map[string]string{"path": binary, "sha256": hex.EncodeToString(sum[:])})
	}
	versionCommand := exec.Command(previous, "version")
	versionCommand.Env = append(os.Environ(), "ROBOCORP_HOME="+filepath.Join(root, "identity-home"))
	version, err := versionCommand.Output()
	if err != nil || string(bytes.TrimSpace(version)) != "v18.19.9" {
		t.Fatalf("previous binary identity: %q %v", version, err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	metadataPub, metadataKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := artifacttrust.Digest([]byte("inventory migration fixture"))
	signature, err := artifacttrust.Sign(digest, "artifact", key)
	if err != nil {
		t.Fatal(err)
	}
	signatures := trustfixture.WriteJSON(t, root, "signatures.json", []artifacttrust.Signature{signature})
	keys := trustfixture.WriteJSON(t, root, "artifact-roots.json", map[string]string{"artifact": base64.RawStdEncoding.EncodeToString(pub)})
	at := time.Now().UTC().Truncate(time.Second)
	roots := artifacttrust.MetadataRoots{SchemaVersion: 2, Scope: "inventory-migration", MinimumSequence: 1, Keys: map[string]artifacttrust.MetadataKey{
		"metadata": {PublicKey: base64.RawStdEncoding.EncodeToString(metadataPub), NotBefore: at.Add(-time.Hour).Format(time.RFC3339), NotAfter: at.Add(2 * time.Hour).Format(time.RFC3339)},
	}}
	authority := trustfixture.WriteJSON(t, root, "metadata-roots.json", roots)
	cases := []struct {
		name, media string
		sequence    uint64
	}{
		{"legacy-initial", artifacttrust.LegacySBOMMediaType, 1},
		{"new-inventory", artifacttrust.ObjectInventoryMediaType, 2},
		{"legacy-forward-recovery", artifacttrust.LegacySBOMMediaType, 3},
	}
	for _, item := range cases {
		payload := artifacttrust.MetadataPayload{SchemaVersion: 2, Scope: roots.Scope, ArtifactDigest: digest, Sequence: item.sequence, KeyID: "metadata",
			IssuedAt: at.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339),
			SBOM: &artifacttrust.SBOM{MediaType: item.media, ArtifactDigest: digest, Components: []artifacttrust.Component{{Name: "opaque-object", Version: "object-digest", PackageType: "rcc-hololib-object-v12"}}}, Revocations: []artifacttrust.Revocation{}}
		envelope, err := artifacttrust.SignMetadata(payload, metadataKey)
		if err != nil {
			t.Fatal(err)
		}
		file := trustfixture.WriteJSON(t, evidence, item.name+"-metadata.json", envelope)
		for label, binary := range map[string]string{"current": current, "previous": previous} {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			command := exec.CommandContext(ctx, binary, "env", "trust", "verify", "--artifact", digest, "--signatures", signatures, "--trust-roots", keys,
				"--metadata", file, "--metadata-trust-roots", authority, "--metadata-state", filepath.Join(root, label+"-retained-state"), "--strict-remote", "--json")
			command.Env = append(os.Environ(), "ROBOCORP_HOME="+filepath.Join(root, label+"-private-home"))
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			runErr := command.Run()
			cancel()
			if err := os.WriteFile(filepath.Join(evidence, label+"-"+item.name+".stdout.json"), stdout.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(evidence, label+"-"+item.name+".stderr.log"), stderr.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			var receipt artifacttrust.VerificationReceipt
			if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
				t.Fatalf("%s/%s receipt: %v %s", label, item.name, err, stderr.String())
			}
			want := artifacttrust.CodeValid
			if label == "previous" && item.media == artifacttrust.ObjectInventoryMediaType {
				want = artifacttrust.CodePolicy
			}
			if receipt.Code != want || receipt.Valid != (want == artifacttrust.CodeValid) || (runErr == nil) != receipt.Valid || receipt.MetadataVersion != 2 || receipt.MetadataSequence != item.sequence {
				t.Fatalf("%s/%s: expected %s, got %+v (%v)", label, item.name, want, receipt, runErr)
			}
			t.Logf("%s/%s: %s sequence=%d", label, item.name, receipt.Code, receipt.MetadataSequence)
		}
	}
}
