package artifacttrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in evidence against the retained pre-fix CLI, not an acceptance test for v2.
func TestMetadataV1AttackBaseline(t *testing.T) {
	binary, output := os.Getenv("RCC_V1_BASELINE_BINARY"), os.Getenv("RCC_V1_BASELINE_OUTPUT")
	if binary == "" || output == "" {
		t.Skip("requires retained v1 executable and evidence directory")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	artifact := Digest([]byte("metadata-attack-fixture"))
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := Sign(artifact, "artifact-signer", key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	provenance := Provenance{MediaType: ProvenanceMediaType, ArtifactDigest: artifact, Platform: "linux_amd64", Builder: "fixture", SourceRepository: "forbidden", DependencySources: []string{"forbidden"}, CreatedAt: now.Format(time.RFC3339)}
	q := VerifyRequest{ArtifactDigest: artifact, Platform: "linux_amd64", Builder: "fixture", Keys: map[string]ed25519.PublicKey{"artifact-signer": pub}, Signatures: []Signature{sig}, At: now, RevocationFetchedAt: now, Provenance: &provenance}
	policy := Policy{Mode: StrictRemote, FailClosedRevocations: true, AcceptedSources: []string{"allowed"}, AcceptedDependencySources: []string{"allowed"}}
	if policy.Verify(q).Valid {
		t.Fatal("disallowed provenance unexpectedly accepted")
	}
	provenance.SourceRepository = "allowed"
	provenance.DependencySources = []string{"allowed"}
	if DefaultPolicyRevision == "artifacttrust/v1" {
		if !policy.Verify(q).Valid {
			t.Fatal("baseline source/dependency assertion attack not reproduced")
		}
	} else {
		if policy.Verify(q).Valid {
			t.Fatal("new source accepted unsigned assertion mutation")
		}
		t.Log("API baseline reproduction requires v1 source; current source denies it. CLI probes below use the supplied old executable.")
	}
	write := func(name string, v any) string {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(output, name)
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	roots := write("roots.json", map[string]string{"artifact-signer": base64.RawStdEncoding.EncodeToString(pub)})
	sigs := write("signatures.json", []Signature{sig})
	prov := write("provenance.json", provenance)
	old := now.Add(-time.Minute)
	for _, tc := range []struct {
		name              string
		at                time.Time
		revoked, accepted bool
	}{
		{"revoked", now, true, false}, {"newer-empty", now.Add(time.Second), false, true}, {"older-empty-replay", old, false, true},
	} {
		var entries []Revocation
		if tc.revoked {
			entries = []Revocation{{KeyIDs: []string{"artifact-signer"}, UpdatedAt: old.Format(time.RFC3339)}}
		}
		bundle, _, err := NewRevocationBundleAt(artifact, entries, tc.at, "baseline-carrier")
		if err != nil {
			t.Fatal(err)
		}
		rev := write(tc.name+"-revocations.json", bundle)
		cmd := exec.Command(binary, "env", "trust", "verify", "--artifact", artifact, "--platform", "linux_amd64", "--builder", "fixture", "--provenance", prov, "--signatures", sigs, "--trust-roots", roots, "--revocations", rev, "--verification-time", now.Add(2*time.Second).Format(time.RFC3339), "--strict-remote", "--json")
		cmd.Env = append(os.Environ(), "ROBOCORP_HOME="+filepath.Join(output, "home"), "RCC_HOLOTREE_MODE=private")
		stdout, runErr := cmd.Output()
		code := 0
		if runErr != nil {
			code = cmd.ProcessState.ExitCode()
		}
		if err := os.WriteFile(filepath.Join(output, tc.name+"-stdout.json"), stdout, 0600); err != nil {
			t.Fatal(err)
		}
		write(tc.name+"-command.json", map[string]any{"argv": cmd.Args, "exit": code})
		if exit, ok := runErr.(*exec.ExitError); ok {
			if err := os.WriteFile(filepath.Join(output, tc.name+"-stderr.txt"), exit.Stderr, 0600); err != nil {
				t.Fatal(err)
			}
		}
		var receipt VerificationReceipt
		if err := json.Unmarshal(stdout, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Valid != tc.accepted || (runErr == nil) != tc.accepted {
			t.Fatalf("%s: unexpected baseline result: %s %v", tc.name, stdout, runErr)
		}
		t.Logf("baseline %s: exit=%d valid=%v code=%s", tc.name, code, receipt.Valid, receipt.Code)
	}
	t.Log("Source/dependency allowlist bypass reproduced against Policy.Verify; CLI has no source allowlist flag. Private signing key never persisted.")
}
