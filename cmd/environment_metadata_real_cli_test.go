package cmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/internal/trustfixture"
)

// Every verification is a fresh process with one retained deployment journal.
// No environment build, network or shared RCC home is needed for these attacks.
func TestMetadataSecurityRealCLI(t *testing.T) {
	binary := os.Getenv("RCC_ACCEPTANCE_BINARY")
	if binary == "" {
		t.Skip("set RCC_ACCEPTANCE_BINARY to the candidate executable")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	evidence := os.Getenv("RCC_METADATA_EVIDENCE")
	if evidence == "" {
		evidence = filepath.Join(root, "evidence")
	}
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	build, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{}
	for _, setting := range build.Settings {
		settings[setting.Key] = setting.Value
	}
	if expected := os.Getenv("RCC_CANDIDATE_SHA"); expected != "" && (settings["vcs.revision"] != expected || settings["vcs.modified"] != "false") {
		t.Fatalf("candidate source identity mismatch: revision=%s modified=%s expected=%s", settings["vcs.revision"], settings["vcs.modified"], expected)
	}
	trustfixture.WriteJSON(t, evidence, "candidate-identity.json", map[string]any{"binary": binary, "sha256": fmt.Sprintf("%x", sha256.Sum256(binaryBytes)), "goVersion": build.GoVersion, "settings": settings})
	at := time.Now().UTC().Truncate(time.Second)
	digest := artifacttrust.Digest([]byte("immutable metadata CLI fixture"))
	artifactPublic, artifactPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	metadataPublic, metadataPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := artifacttrust.Sign(digest, "artifact-key", artifactPrivate)
	if err != nil {
		t.Fatal(err)
	}
	sigs := trustfixture.WriteJSON(t, root, "signatures.json", []artifacttrust.Signature{sig})
	keys := trustfixture.WriteJSON(t, root, "artifact-roots.json", map[string]string{"artifact-key": base64.RawStdEncoding.EncodeToString(artifactPublic)})
	start, end := at.Add(-time.Hour).Format(time.RFC3339), at.Add(3*time.Hour).Format(time.RFC3339)
	roots := artifacttrust.MetadataRoots{SchemaVersion: 2, Scope: "CLI-deployment", MinimumSequence: 10, Keys: map[string]artifacttrust.MetadataKey{"authority-A": {PublicKey: base64.RawStdEncoding.EncodeToString(metadataPublic), NotBefore: start, NotAfter: end}}}
	rootsPath := trustfixture.WriteJSON(t, root, "metadata-roots.json", roots)
	state := filepath.Join(root, "retained-state")
	provenance := artifacttrust.Provenance{MediaType: artifacttrust.ProvenanceMediaType, ArtifactDigest: digest, Platform: "fixture-platform", Builder: "fixture-builder", SourceRepository: "approved-source", DependencySources: []string{"approved-dependency"}}
	payload := artifacttrust.MetadataPayload{SchemaVersion: 2, Scope: roots.Scope, ArtifactDigest: digest, Sequence: 10, KeyID: "authority-A", IssuedAt: at.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: at.Add(time.Hour).Format(time.RFC3339), Provenance: &provenance, SBOM: &artifacttrust.SBOM{MediaType: artifacttrust.SBOMMediaType, ArtifactDigest: digest, Components: []artifacttrust.Component{{Name: "package", Version: "1"}}}, Revocations: []artifacttrust.Revocation{}}
	sign := func(p artifacttrust.MetadataPayload, key ed25519.PrivateKey) artifacttrust.MetadataEnvelope {
		t.Helper()
		e, err := artifacttrust.SignMetadata(p, key)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	run := func(name string, envelope *artifacttrust.MetadataEnvelope, mode, want string, raw ...[]byte) artifacttrust.VerificationReceipt {
		t.Helper()
		args := []string{"env", "trust", "verify", "--artifact", digest, "--platform", provenance.Platform, "--builder", provenance.Builder, "--signatures", sigs, "--trust-roots", keys, "--metadata-trust-roots", rootsPath, "--metadata-state", state, mode, "--json"}
		if envelope != nil {
			file := trustfixture.WriteJSON(t, evidence, name+"-metadata.json", envelope)
			if len(raw) > 0 {
				if err := os.WriteFile(file, raw[0], 0600); err != nil {
					t.Fatal(err)
				}
			}
			args = append(args, "--metadata", file)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(), "ROBOCORP_HOME="+filepath.Join(root, "private-home"), "RCC_HOLOTREE_MODE=private", "TMPDIR="+root, "TEMP="+root, "TMP="+root)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if cmd.ProcessState == nil {
			t.Fatalf("%s: launch: %v", name, err)
		}
		for suffix, data := range map[string][]byte{"stdout.json": stdout.Bytes(), "stderr.log": stderr.Bytes()} {
			if err := os.WriteFile(filepath.Join(evidence, name+"-"+suffix), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		trustfixture.WriteJSON(t, evidence, name+"-command.json", struct {
			Binary string
			Args   []string
			Exit   int
		}{binary, args, cmd.ProcessState.ExitCode()})
		var receipt artifacttrust.VerificationReceipt
		if decodeErr := json.Unmarshal(stdout.Bytes(), &receipt); decodeErr != nil {
			t.Fatalf("%s: invalid JSON %v, stderr=%s", name, decodeErr, stderr.String())
		}
		if receipt.Code != want || receipt.Valid != (want == artifacttrust.CodeValid) || (err == nil) != receipt.Valid {
			t.Fatalf("%s: exit=%v receipt=%+v stderr=%s", name, err, receipt, stderr.String())
		}
		if receipt.ArtifactDigest != digest || receipt.DecisionID == "" {
			t.Fatalf("%s: lost immutable identity or decision", name)
		}
		t.Logf("%s: exit=%d code=%s metadataSequence=%d decision=%s", name, cmd.ProcessState.ExitCode(), receipt.Code, receipt.MetadataSequence, receipt.DecisionID)
		return receipt
	}
	// Exercise the migration signer, not only the Go signing helper.
	privatePath := filepath.Join(root, "disposable-metadata.key")
	if err := os.WriteFile(privatePath, metadataPrivate, 0600); err != nil {
		t.Fatal(err)
	}
	provenancePath := trustfixture.WriteJSON(t, root, "reviewed-provenance.json", payload.Provenance)
	sbomPath := trustfixture.WriteJSON(t, root, "reviewed-sbom.json", payload.SBOM)
	reviewedRevocations := trustfixture.WriteJSON(t, root, "reviewed-revocations.json", []artifacttrust.Revocation{})
	signedPath := filepath.Join(root, "signed-by-cli.json")
	signArgs := []string{"env", "trust", "sign-metadata", "--artifact", digest, "--scope", payload.Scope, "--sequence", "10", "--metadata-key-id", payload.KeyID, "--metadata-signing-key", privatePath, "--issued-at", payload.IssuedAt, "--expires-at", payload.ExpiresAt, "--provenance", provenancePath, "--sbom", sbomPath, "--revocations", reviewedRevocations, "--output", signedPath}
	signContext, signCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer signCancel()
	signer := exec.CommandContext(signContext, binary, signArgs...)
	signer.Env = append(os.Environ(), "ROBOCORP_HOME="+filepath.Join(root, "signer-home"))
	if output, err := signer.CombinedOutput(); err != nil {
		t.Fatalf("CLI metadata signing: %v %s", err, output)
	}
	signedBytes, err := os.ReadFile(signedPath)
	if err != nil {
		t.Fatal(err)
	}
	var first artifacttrust.MetadataEnvelope
	if err := json.Unmarshal(signedBytes, &first); err != nil {
		t.Fatal(err)
	}
	if first != sign(payload, metadataPrivate) {
		t.Fatal("CLI signer did not cover the reviewed complete payload")
	}
	accepted := run("authenticated", &first, "--strict-remote", artifacttrust.CodeValid)
	run("malformed-envelope", &first, "--strict-remote", artifacttrust.CodeMetadataInvalid, []byte("{invalid"))
	t.Run("N-minus-one-migration", func(t *testing.T) {
		older := os.Getenv("RCC_N_MINUS_ONE_BINARY")
		if older == "" {
			t.Skip("set RCC_N_MINUS_ONE_BINARY to the released v18.19.7 executable")
		}
		data, err := os.ReadFile(older)
		if err != nil {
			t.Fatal(err)
		}
		version, err := exec.CommandContext(signContext, older, "version").Output()
		if err != nil || strings.TrimSpace(string(version)) != "v18.19.7" {
			t.Fatalf("N-1 identity: %q %v", version, err)
		}
		bundle, _, err := artifacttrust.NewRevocationBundleAt(digest, nil, at, "reviewed-legacy")
		if err != nil {
			t.Fatal(err)
		}
		revocations := trustfixture.WriteJSON(t, root, "legacy-revocations.json", bundle)
		legacyArgs := []string{"env", "trust", "verify", "--artifact", digest, "--platform", provenance.Platform, "--builder", provenance.Builder, "--signatures", sigs, "--trust-roots", keys, "--provenance", provenancePath, "--sbom", sbomPath, "--revocations", revocations, "--strict-remote", "--json"}
		old := exec.CommandContext(signContext, older, legacyArgs...)
		old.Env = append(os.Environ(), "ROBOCORP_HOME="+filepath.Join(root, "n-minus-one-home"))
		stdout, err := old.Output()
		if err != nil {
			t.Fatalf("N-1 legacy fixture: %v", err)
		}
		var receipt artifacttrust.VerificationReceipt
		if err := json.Unmarshal(stdout, &receipt); err != nil || !receipt.Valid {
			t.Fatalf("N-1 legacy receipt: %+v %v", receipt, err)
		}
		if err := os.WriteFile(filepath.Join(evidence, "n-minus-one-legacy.stdout.json"), stdout, 0600); err != nil {
			t.Fatal(err)
		}
		v2 := exec.CommandContext(signContext, older, append(legacyArgs, "--metadata", signedPath, "--metadata-trust-roots", rootsPath, "--metadata-state", state)...)
		v2.Env = old.Env
		output, err := v2.CombinedOutput()
		if err == nil || !bytes.Contains(output, []byte("unknown flag")) {
			t.Fatalf("N-1 silently accepted v2 requirement: %v %s", err, output)
		}
		if err := os.WriteFile(filepath.Join(evidence, "n-minus-one-v2.stderr.log"), output, 0600); err != nil {
			t.Fatal(err)
		}
		trustfixture.WriteJSON(t, evidence, "n-minus-one-identity.json", map[string]any{"binary": older, "version": strings.TrimSpace(string(version)), "sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "legacyArgs": legacyArgs, "legacyExit": old.ProcessState.ExitCode(), "v2Args": v2.Args, "v2Exit": v2.ProcessState.ExitCode()})
	})
	for _, field := range []string{"source", "dependency", "sbom", "revocations"} {
		data, _ := base64.RawStdEncoding.DecodeString(first.Payload)
		var changed artifacttrust.MetadataPayload
		if err := json.Unmarshal(data, &changed); err != nil {
			t.Fatal(err)
		}
		switch field {
		case "source":
			changed.Provenance.SourceRepository = "attacker-source"
		case "dependency":
			changed.Provenance.DependencySources = []string{"attacker-dependency"}
		case "sbom":
			changed.SBOM.Components[0].Version = "hidden"
		case "revocations":
			changed.Sequence = 999
			changed.IssuedAt = at.Format(time.RFC3339)
			changed.Revocations = nil
		}
		data, _ = json.Marshal(changed)
		mutated := first
		mutated.Payload = base64.RawStdEncoding.EncodeToString(data)
		run("unsigned-mutation-"+field, &mutated, "--strict-remote", artifacttrust.CodeMetadataInvalid)
	}
	// Unauthenticated high sequence cannot poison the retained checkpoint.
	run("valid-after-forged-high-sequence", &first, "--strict-remote", artifacttrust.CodeValid)
	payload.Sequence = 11
	payload.Revocations = []artifacttrust.Revocation{{KeyIDs: []string{"artifact-key"}, UpdatedAt: at.Format(time.RFC3339)}}
	revoked := sign(payload, metadataPrivate)
	denied := run("authorized-revocation", &revoked, "--strict-remote", artifacttrust.CodeRevoked)
	run("restart-replay-older-empty", &first, "--strict-remote", artifacttrust.CodeMetadataRollback)
	if accepted.DecisionID == denied.DecisionID || accepted.MetadataSequence != 10 {
		t.Fatal("old decision identity changed")
	}
	payload.Revocations = nil
	equivocal := sign(payload, metadataPrivate)
	run("same-sequence-empty", &equivocal, "--strict-remote", artifacttrust.CodeMetadataRollback)
	rotatedPublic, rotatedPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload.KeyID = "authority-B"
	payload.Sequence = 12
	rotated := sign(payload, rotatedPrivate)
	run("unauthorized-rotation", &rotated, "--strict-remote", artifacttrust.CodeMetadataAuthority)
	roots.Keys[payload.KeyID] = artifacttrust.MetadataKey{PublicKey: base64.RawStdEncoding.EncodeToString(rotatedPublic), NotBefore: start, NotAfter: end}
	trustfixture.WriteJSON(t, root, "metadata-roots.json", roots)
	run("authorized-rotation", &rotated, "--strict-remote", artifacttrust.CodeValid)
	delete(roots.Keys, "authority-A")
	trustfixture.WriteJSON(t, root, "metadata-roots.json", roots)
	run("retired-authority", &first, "--strict-remote", artifacttrust.CodeMetadataAuthority)
	for _, scenario := range []string{"expired", "future", "wrong-scope", "wrong-artifact"} {
		p := payload
		p.Sequence = 13
		want := artifacttrust.CodeMetadataExpired
		switch scenario {
		case "expired":
			p.IssuedAt = at.Add(-30 * time.Minute).Format(time.RFC3339)
			p.ExpiresAt = at.Add(-time.Minute).Format(time.RFC3339)
		case "future":
			p.IssuedAt = at.Add(time.Hour).Format(time.RFC3339)
			p.ExpiresAt = at.Add(2 * time.Hour).Format(time.RFC3339)
		case "wrong-scope":
			p.Scope = "other-deployment"
			want = artifacttrust.CodeMetadataAuthority
		case "wrong-artifact":
			p.ArtifactDigest = artifacttrust.Digest([]byte("other"))
			p.Provenance = nil
			p.SBOM = nil
			want = artifacttrust.CodeMetadataAuthority
		}
		e := sign(p, rotatedPrivate)
		run(scenario, &e, "--strict-remote", want)
	}
	run("unsigned-v1-strict", nil, "--strict-remote", artifacttrust.CodeMetadataUnsigned)
	// Legacy mode is explicit and makes no authenticated-metadata assertion.
	run("explicit-legacy", nil, "--permissive-local", artifacttrust.CodeValid)
	roots.MinimumSequence = 13
	trustfixture.WriteJSON(t, root, "metadata-roots.json", roots)
	run("deployment-floor", &rotated, "--strict-remote", artifacttrust.CodeMetadataRollback)
	roots.MinimumSequence = 10
	trustfixture.WriteJSON(t, root, "metadata-roots.json", roots)
	journals, err := filepath.Glob(filepath.Join(state, "*.jsonl"))
	if err != nil || len(journals) != 1 {
		t.Fatal(journals, err)
	}
	f, err := os.OpenFile(journals[0], os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"sequence\":13"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	run("partial-journal-restart", &rotated, "--strict-remote", artifacttrust.CodeMetadataState)
}
