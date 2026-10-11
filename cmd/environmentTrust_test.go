package cmd

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
)

func TestEnvironmentTrustVerifyAcceptsRawAndPaddedPublicKeys(t *testing.T) {
	public, private := trustVerifyTestKey()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	artifact := "sha256:fixture"
	signature, err := artifacttrust.Sign(artifact, "fixture-key", private)
	if err != nil {
		t.Fatal(err)
	}
	_, signatureBytes, err := artifacttrust.NewSignatureBundle(artifact, []artifacttrust.Signature{signature})
	if err != nil {
		t.Fatal(err)
	}
	_, revocationBytes, err := artifacttrust.NewRevocationBundleAt(artifact, nil, at, "fixture")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		key  string
	}{
		{name: "raw base64", key: base64.RawStdEncoding.EncodeToString(public)},
		{name: "padded standard base64", key: base64.StdEncoding.EncodeToString(public)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "padded standard base64" && !strings.HasSuffix(test.key, "=") {
				t.Fatal("test fixture public key is not padded")
			}
			stdout, err := runTrustVerifyFixture(t, artifact, test.key, signatureBytes, revocationBytes, at)
			if err != nil {
				t.Fatalf("strict verification failed: %v", err)
			}
			var receipt artifacttrust.VerificationReceipt
			if err := json.Unmarshal(stdout, &receipt); err != nil {
				t.Fatalf("decode verification receipt: %v", err)
			}
			if !receipt.Valid || receipt.Code != artifacttrust.CodeValid || receipt.KeyID != "fixture-key" {
				t.Fatalf("verification receipt = %+v", receipt)
			}
		})
	}
}

func TestEnvironmentTrustVerifyRejectsMalformedPublicKeys(t *testing.T) {
	public, private := trustVerifyTestKey()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	artifact := "sha256:fixture"
	signature, err := artifacttrust.Sign(artifact, "fixture-key", private)
	if err != nil {
		t.Fatal(err)
	}
	_, signatureBytes, err := artifacttrust.NewSignatureBundle(artifact, []artifacttrust.Signature{signature})
	if err != nil {
		t.Fatal(err)
	}
	_, revocationBytes, err := artifacttrust.NewRevocationBundleAt(artifact, nil, at, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		key  string
	}{
		{name: "invalid alphabet", key: "%%%"},
		{name: "truncated key", key: base64.StdEncoding.EncodeToString(public[:ed25519.PublicKeySize-1])},
		{name: "wrong-size key", key: base64.StdEncoding.EncodeToString(append(append([]byte(nil), public...), 0))},
	} {
		t.Run(test.name, func(t *testing.T) {
			stdout, err := runTrustVerifyFixture(t, artifact, test.key, signatureBytes, revocationBytes, at)
			if err == nil || !strings.Contains(err.Error(), "invalid trust root") {
				t.Fatalf("invalid key error = %v", err)
			}
			if bytes.Contains(stdout, []byte(test.key)) {
				t.Fatal("verification output exposed the supplied trust-root value")
			}
		})
	}
}

func TestEnvironmentTrustVerifyRejectsInvalidSignatureWithValidKey(t *testing.T) {
	public, private := trustVerifyTestKey()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	artifact := "sha256:fixture"
	signature, err := artifacttrust.Sign(artifact, "fixture-key", private)
	if err != nil {
		t.Fatal(err)
	}
	signature.Signature = base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	_, signatureBytes, err := artifacttrust.NewSignatureBundle(artifact, []artifacttrust.Signature{signature})
	if err != nil {
		t.Fatal(err)
	}
	_, revocationBytes, err := artifacttrust.NewRevocationBundleAt(artifact, nil, at, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := runTrustVerifyFixture(t, artifact, base64.StdEncoding.EncodeToString(public), signatureBytes, revocationBytes, at)
	if err == nil || !strings.Contains(err.Error(), "artifact trust verification failed: invalid") {
		t.Fatalf("invalid signature error = %v", err)
	}
	var receipt artifacttrust.VerificationReceipt
	if err := json.Unmarshal(stdout, &receipt); err != nil {
		t.Fatalf("decode failed verification receipt: %v", err)
	}
	if receipt.Valid || receipt.Code != artifacttrust.CodeInvalid {
		t.Fatalf("invalid signature receipt = %+v", receipt)
	}
}

func trustVerifyTestKey() (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	private := ed25519.NewKeyFromSeed(seed)
	public := append(ed25519.PublicKey(nil), private[ed25519.SeedSize:]...)
	return public, private
}

func runTrustVerifyFixture(t *testing.T, artifact, key string, signatureBytes, revocationBytes []byte, at time.Time) ([]byte, error) {
	t.Helper()
	root := t.TempDir()
	keysPath := filepath.Join(root, "trust-roots.json")
	signaturesPath := filepath.Join(root, "signatures.json")
	revocationsPath := filepath.Join(root, "revocations.json")
	keys, err := json.Marshal(map[string]string{"fixture-key": key})
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		keysPath: keys, signaturesPath: signatureBytes, revocationsPath: revocationBytes,
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	command := newEnvironmentTrustCommand()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	arguments := []string{
		"verify", "--artifact", artifact, "--platform", "linux_amd64", "--builder", "fixture-builder",
		"--signatures", signaturesPath, "--trust-roots", keysPath, "--revocations", revocationsPath,
		"--verification-time", at.Format(time.RFC3339), "--strict-remote", "--json",
	}
	err = runCobraCommand(command, arguments)
	return stdout.Bytes(), err
}
