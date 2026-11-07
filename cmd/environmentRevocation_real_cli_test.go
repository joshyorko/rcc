package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/joshyorko/rcc/internal/trustfixture"
)

// Uses the same native archive/binary inputs as TestBundleTrustRealCLI. All
// authority keys, homes and journals are disposable; no provider is contacted.
func TestRevocationOperatorRealCLI(t *testing.T) {
	binary, archive := os.Getenv("RCC_ACCEPTANCE_BINARY"), os.Getenv("RCC_BUNDLE_TEST_ARCHIVE")
	if binary == "" || archive == "" {
		t.Skip("set RCC_ACCEPTANCE_BINARY and RCC_BUNDLE_TEST_ARCHIVE to native executable/archive")
	}
	binary, _ = filepath.Abs(binary)
	archive, _ = filepath.Abs(archive)
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	var manifest environmentartifact.Manifest
	for _, entry := range zr.File {
		if entry.Name == environmentartifact.ArchiveManifest {
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil {
				t.Fatal(err)
			}
			manifest, err = environmentartifact.DecodeManifest(data)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	digest := manifest.ArtifactDigest.String()
	if manifest.ArtifactDigest.Hex() == "" {
		t.Fatal("missing native manifest")
	}
	evidence := os.Getenv("RCC_REVOCATION_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	}
	if err = os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	binaryData, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	buildSettings := map[string]string{}
	for _, s := range info.Settings {
		buildSettings[s.Key] = s.Value
	}
	if expected := os.Getenv("RCC_CANDIDATE_SHA"); expected != "" && (buildSettings["vcs.revision"] != expected || buildSettings["vcs.modified"] != "false") {
		t.Fatal("binary is not clean exact-head source", buildSettings)
	}
	archiveData, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	archiveHash := sha256.Sum256(archiveData)
	trustfixture.WriteJSON(t, evidence, "inputs.json", map[string]any{"binary": binary, "binarySHA256": fmt.Sprintf("%x", sha256.Sum256(binaryData)), "goVersion": info.GoVersion, "settings": buildSettings, "archive": archive, "archiveSHA256": fmt.Sprintf("%x", archiveHash), "artifact": digest, "platform": manifest.Platform})
	for _, target := range []string{"artifact", "signer"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "worker-home")
			operatorHome := filepath.Join(root, "operator-home")
			// Keep the unrelated CLI update check offline in both private homes.
			for _, privateHome := range []string{home, operatorHome} {
				if err := os.MkdirAll(filepath.Join(privateHome, "templates"), 0700); err != nil {
					t.Fatal(err)
				}
				trustfixture.WriteJSON(t, filepath.Join(privateHome, "templates"), "rcc.json", map[string]any{"tested": []any{}})
			}
			caseEvidence := filepath.Join(evidence, target)
			if err := os.MkdirAll(caseEvidence, 0700); err != nil {
				t.Fatal(err)
			}
			at := time.Now().UTC().Truncate(time.Second)
			pub, key, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			artifactPublic, artifactPrivate, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			roots := artifacttrust.MetadataRoots{SchemaVersion: 2, Scope: "operator-CLI-" + target, MinimumSequence: 10, Keys: map[string]artifacttrust.MetadataKey{"authority": {PublicKey: base64.RawStdEncoding.EncodeToString(pub), NotBefore: at.Add(-48 * time.Hour).Format(time.RFC3339), NotAfter: at.Add(48 * time.Hour).Format(time.RFC3339)}}}
			rootsPath := trustfixture.WriteJSON(t, root, "metadata-roots.json", roots)
			artifactRoots := trustfixture.WriteJSON(t, root, "artifact-roots.json", map[string]string{"artifact-signer": base64.RawStdEncoding.EncodeToString(artifactPublic)})
			keyFile := filepath.Join(root, "disposable-authority.key")
			if err := os.WriteFile(keyFile, key, 0600); err != nil {
				t.Fatal(err)
			}
			carrier := artifacttrust.NewFilesystemCarrier(filepath.Join(root, "carrier"))
			sig, err := artifacttrust.Sign(digest, "artifact-signer", artifactPrivate)
			if err != nil {
				t.Fatal(err)
			}
			_, sigData, err := artifacttrust.NewSignatureBundle(digest, []artifacttrust.Signature{sig})
			if err != nil {
				t.Fatal(err)
			}
			if err = artifacttrust.PutAttachment(carrier, digest, "signature", sigData); err != nil {
				t.Fatal(err)
			}
			signatures := trustfixture.WriteJSON(t, root, "signatures.json", []artifacttrust.Signature{sig})
			revocations := trustfixture.WriteJSON(t, root, "reviewed-revocations.json", []artifacttrust.Revocation{})
			provenance := trustfixture.WriteJSON(t, root, "provenance.json", artifacttrust.Provenance{MediaType: artifacttrust.ProvenanceMediaType, ArtifactDigest: digest, Platform: manifest.Platform.RCCPlatform, Builder: manifest.Builder.Kind, SourceRepository: "reviewed-fixture"})
			sbom := trustfixture.WriteJSON(t, root, "sbom.json", artifacttrust.SBOM{MediaType: artifacttrust.SBOMMediaType, ArtifactDigest: digest, Components: []artifacttrust.Component{{Name: "native-fixture", Version: "1"}}})
			issuerState, distributionState, workerState := filepath.Join(root, "issuer-journal"), filepath.Join(root, "distribution-journal"), filepath.Join(root, "worker-journal")
			environment := func(home string) []string {
				return append(os.Environ(), "ROBOCORP_HOME="+home, "RCC_HOLOTREE_MODE=private", "RCC_AUTOUPDATES_RCC_INDEX=", "RCC_AUTOUPDATES_TEMPLATES=", "TMPDIR="+root, "TEMP="+root, "TMP="+root)
			}
			run := func(name, selectedHome string, args ...string) ([]byte, error) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				command := exec.CommandContext(ctx, binary, args...)
				command.Env = environment(selectedHome)
				var out, stderr bytes.Buffer
				command.Stdout = &out
				command.Stderr = &stderr
				err := command.Run()
				for suffix, data := range map[string][]byte{"stdout.json": out.Bytes(), "stderr.log": stderr.Bytes()} {
					if e := os.WriteFile(filepath.Join(caseEvidence, name+"-"+suffix), data, 0600); e != nil {
						t.Fatal(e)
					}
				}
				exit := -1
				if command.ProcessState != nil {
					exit = command.ProcessState.ExitCode()
				}
				trustfixture.WriteJSON(t, caseEvidence, name+"-command.json", map[string]any{"binary": binary, "args": args, "home": selectedHome, "exit": exit})
				if err != nil {
					err = fmt.Errorf("%w: %s", err, stderr.String())
				}
				return out.Bytes(), err
			}
			operation := func(name, action, input string, sequence int, want string, extra ...string) string {
				t.Helper()
				output := filepath.Join(root, name+"-metadata.json")
				state := issuerState
				if action == "distribute" {
					state = distributionState
				}
				args := []string{"env", "trust", "revocations", action, "--artifact", digest, "--metadata-trust-roots", rootsPath, "--metadata-state", state, "--json"}
				if input != "" {
					args = append(args, "--metadata", input)
				}
				if action == "distribute" {
					args = append(args, "--trust-carrier", carrier.Root)
				} else {
					args = append(args, "--sequence", fmt.Sprint(sequence), "--metadata-key-id", "authority", "--metadata-signing-key", keyFile, "--issued-at", at.Add(-time.Minute).Format(time.RFC3339), "--expires-at", at.Add(time.Hour).Format(time.RFC3339), "--output", output)
				}
				args = append(args, extra...)
				stdout, err := run(name, operatorHome, args...)
				var r artifacttrust.MetadataOperatorReceipt
				if e := json.Unmarshal(stdout, &r); e != nil {
					t.Fatalf("%s invalid operator receipt: %v %s %v", name, e, stdout, err)
				}
				if r.Code != want || r.Valid != (want == artifacttrust.CodeValid) || (err == nil) != r.Valid {
					t.Fatalf("%s: %+v %v", name, r, err)
				}
				if r.Valid && (!r.Retained || r.MetadataDigest == "" || r.ArtifactDigest != digest || (action == "distribute" && !r.Distributed)) {
					t.Fatal("lost operator identity/side effects", r)
				}
				if action != "distribute" && r.Valid {
					data, e := os.ReadFile(output)
					if e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(filepath.Join(caseEvidence, name+"-metadata.json"), data, 0600); e != nil {
						t.Fatal(e)
					}
				}
				return output
			}
			first := operation("issue", "issue", "", 10, artifacttrust.CodeValid, "--revocations", revocations, "--provenance", provenance, "--sbom", sbom)
			operation("distribute-initial", "distribute", first, 0, artifacttrust.CodeValid)
			workerArgs := []string{"--artifact", digest, "--trust-carrier", carrier.Root, "--trust-roots", artifactRoots, "--metadata-trust-roots", rootsPath, "--metadata-state", workerState, "--strict-remote"}
			acquire := append([]string{"env", "acquire", "--json"}, workerArgs...)
			// Only the initial strict acquisition imports/materializes the existing native
			// archive. The operator lifecycle below never calls publish or a provider.
			stdout, err := run("initial-acquire", home, append(acquire, "--archive", archive)...)
			if err != nil {
				t.Fatal(err)
			}
			var cold environmentAcquireResult
			if err = json.Unmarshal(stdout, &cold); err != nil {
				t.Fatal(err)
			}
			if cold.Verification == nil || !cold.Verification.Valid || cold.Verification.MetadataSequence != 10 {
				t.Fatal(cold)
			}
			renewed := operation("renew", "renew", first, 11, artifacttrust.CodeValid)
			operation("distribute-renewed", "distribute", renewed, 0, artifacttrust.CodeValid)
			stdout, err = run("renewed-acquire", home, acquire...)
			if err != nil {
				t.Fatal(err)
			}
			var warm environmentAcquireResult
			if err = json.Unmarshal(stdout, &warm); err != nil {
				t.Fatal(err)
			}
			if warm.ArtifactDigest != cold.ArtifactDigest || warm.MaterializationID != cold.MaterializationID || warm.Path != cold.Path || warm.CacheHit != environmentlifecycle.CacheLocalMaterialization || warm.Verification == nil || warm.Verification.MetadataSequence != 11 {
				t.Fatalf("renewal rebuilt or failed strict reuse: %+v", warm)
			}
			execArgs := append([]string{"env", "exec", "--json"}, workerArgs...)
			stdout, err = run("renewed-new-lease", home, append(execArgs, "--", "python", "-c", "print('renewed-native-child')")...)
			if err != nil {
				t.Fatal(err)
			}
			var executed environmentExecResult
			if err = json.Unmarshal(stdout, &executed); err != nil {
				t.Fatal(err)
			}
			if executed.LeaseID == "" || executed.Status != "completed" || executed.Verification == nil || executed.Verification.MetadataSequence != 11 || executed.MaterializationID != cold.MaterializationID {
				t.Fatal(executed)
			}
			// A real HTTP carrier receives only detached metadata, then supplies
			// a strict new native lease. No object-store endpoint is available.
			var remoteMu sync.Mutex
			remote := map[string][]byte{"/" + artifacttrust.AttachmentName(digest, "signature"): sigData}
			metadataPath := "/" + artifacttrust.AttachmentName(digest, "metadata")
			puts, invalidPuts := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				remoteMu.Lock()
				defer remoteMu.Unlock()
				if r.Method == http.MethodPut {
					if r.URL.Path != metadataPath {
						invalidPuts++
						http.Error(w, "only detached metadata is writable", http.StatusForbidden)
						return
					}
					data, err := io.ReadAll(io.LimitReader(r.Body, 3<<20))
					if err != nil {
						http.Error(w, "read failed", http.StatusBadRequest)
						return
					}
					puts++
					remote[r.URL.Path] = data
					return
				}
				data, ok := remote[r.URL.Path]
				if !ok {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()
			operation("http-distribute", "distribute", renewed, 0, artifacttrust.CodeValid, "--trust-carrier", server.URL, "--trust-carrier-type", "http")
			remoteExec := append(append([]string{}, execArgs...), "--trust-carrier", server.URL, "--trust-carrier-type", "http", "--", "python", "-c", "print('remote-renewed-native-child')")
			if _, err := run("http-new-lease", home, remoteExec...); err != nil {
				t.Fatal(err)
			}
			remoteMu.Lock()
			if puts != 1 || invalidPuts != 0 {
				t.Errorf("operator HTTP writes: metadata=%d other=%d", puts, invalidPuts)
			}
			remoteMu.Unlock()
			ready, completed, receiptFile := filepath.Join(root, "ready"), filepath.Join(root, "completed"), filepath.Join(root, "running-result.json")
			childCode := fmt.Sprintf("from pathlib import Path;import sys;Path(%q).write_text('ready');sys.stdin.readline();Path(%q).write_text('finished');print('running-native-child-finished')", ready, completed)
			childArgs := append([]string{"env", "exec", "--inherit-streams", "--receipt-file", receiptFile}, workerArgs...)
			childArgs = append(childArgs, "--", "python", "-c", childCode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			child := exec.CommandContext(ctx, binary, childArgs...)
			child.Env = environment(home)
			var childOut, childErr bytes.Buffer
			child.Stdout = &childOut
			child.Stderr = &childErr
			input, err := child.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = input.Close() }()
			if err = child.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- child.Wait() }()
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("child died before ready: %v %s", err, childErr.String())
				case <-ctx.Done():
					t.Fatal("child readiness timeout")
				case <-time.After(10 * time.Millisecond):
				}
			}
			store := artifacttrust.NewReceiptStore(filepath.Join(home, "artifacts", "v1", "verification"))
			history, err := store.History(digest)
			if err != nil {
				t.Fatal(err)
			}
			var original artifacttrust.VerificationReceipt
			for _, r := range history {
				if r.LeaseID != "" && r.MetadataSequence == 11 {
					original = r
				}
			}
			if original.DecisionID == "" {
				t.Fatal("missing running lease decision")
			}
			flags := []string{"--reason", "native operator acceptance"}
			if target == "artifact" {
				flags = append(flags, "--revoke-artifact")
			} else {
				flags = append(flags, "--revoke-key-id", "artifact-signer")
			}
			revoked := operation("revoke", "revoke", renewed, 12, artifacttrust.CodeValid, flags...)
			operation("distribute-revoked", "distribute", revoked, 0, artifacttrust.CodeValid)
			if _, err = run("revoked-new-lease", home, append(execArgs, "--", "python", "-c", "raise SystemExit('must not execute')")...); err == nil || !strings.Contains(err.Error(), artifacttrust.CodeRevoked) {
				t.Fatalf("revoked new lease: %v", err)
			}
			select {
			case err := <-done:
				t.Fatalf("revocation interrupted running child: %v", err)
			default:
			}
			if _, err = io.WriteString(input, "finish\n"); err != nil {
				t.Fatal(err)
			}
			_ = input.Close()
			if err = <-done; err != nil {
				t.Fatalf("running child: %v %s", err, childErr.String())
			}
			finalData, err := os.ReadFile(receiptFile)
			if err != nil {
				t.Fatal(err)
			}
			var final environmentExecResult
			if err = json.Unmarshal(finalData, &final); err != nil {
				t.Fatal(err)
			}
			if final.Status != "completed" || final.ExitCode != 0 || final.Verification == nil || final.Verification.MetadataSequence != 11 || final.Verification.DecisionID != original.DecisionID {
				t.Fatalf("running decision changed: %+v original=%+v", final, original)
			}
			if _, err = os.Stat(completed); err != nil {
				t.Fatal("child did not complete actual work", err)
			}
			if err = os.WriteFile(filepath.Join(caseEvidence, "running-result.json"), finalData, 0600); err != nil {
				t.Fatal(err)
			}
			trustfixture.WriteJSON(t, caseEvidence, "running-command.json", childArgs)
			for suffix, data := range map[string][]byte{"stdout.log": childOut.Bytes(), "stderr.log": childErr.Bytes()} {
				if err = os.WriteFile(filepath.Join(caseEvidence, "running-"+suffix), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Failed admissions emit specific receipts. Authentic high sequence denials
			// commit the journal, and neither carrier rollback nor issuer replay clears it.
			verify := func(name, input, want string) {
				t.Helper()
				args := []string{"env", "trust", "verify", "--artifact", digest, "--platform", manifest.Platform.RCCPlatform, "--builder", manifest.Builder.Kind, "--signatures", signatures, "--trust-roots", artifactRoots, "--metadata-trust-roots", rootsPath, "--metadata-state", workerState, "--strict-remote", "--json"}
				if input != "" {
					args = append(args, "--metadata", input)
				}
				out, err := run(name, home, args...)
				var r artifacttrust.VerificationReceipt
				if e := json.Unmarshal(out, &r); e != nil || r.Code != want || (err == nil) != r.Valid {
					t.Fatalf("%s: %+v %v %v", name, r, err, e)
				}
			}
			verify("revoked-receipt", revoked, artifacttrust.CodeRevoked)
			verify("old-replay", first, artifacttrust.CodeMetadataRollback)
			verify("missing", "", artifacttrust.CodeMetadataUnsigned)
			operation("distributor-replay", "distribute", first, 0, artifacttrust.CodeMetadataRollback)
			operation("issuer-replay", "renew", first, 13, artifacttrust.CodeMetadataRollback)
			var envelope artifacttrust.MetadataEnvelope
			if err = readMetadataEnvelopeFile(renewed, &envelope); err != nil {
				t.Fatal(err)
			}
			payloadBytes, _ := base64.RawStdEncoding.DecodeString(envelope.Payload)
			var payload artifacttrust.MetadataPayload
			if err = json.Unmarshal(payloadBytes, &payload); err != nil {
				t.Fatal(err)
			}
			for _, scenario := range []string{"unauthorized", "expired", "future", "tampered"} {
				p := payload
				p.Sequence = 999
				want := artifacttrust.CodeMetadataExpired
				switch scenario {
				case "unauthorized":
					p.KeyID = "attacker"
					want = artifacttrust.CodeMetadataAuthority
				case "expired":
					p.IssuedAt = at.Add(-2 * time.Hour).Format(time.RFC3339)
					p.ExpiresAt = at.Add(-time.Hour).Format(time.RFC3339)
				case "future":
					p.IssuedAt = at.Add(time.Hour).Format(time.RFC3339)
					p.ExpiresAt = at.Add(2 * time.Hour).Format(time.RFC3339)
				case "tampered":
					want = artifacttrust.CodeMetadataInvalid
				}
				e, err := artifacttrust.SignMetadata(p, key)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "tampered" {
					e.Signature = base64.RawStdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
				}
				path := trustfixture.WriteJSON(t, caseEvidence, scenario+"-metadata.json", e)
				verify(scenario, path, want)
				operation("distributor-"+scenario, "distribute", path, 0, want)
			}
			// Unauthenticated high sequence attempts did not poison the retained journal.
			operation("valid-retry-after-attacks", "distribute", revoked, 0, artifacttrust.CodeValid)
			verify("still-revoked-after-attacks", revoked, artifacttrust.CodeRevoked)
			leases, _ := filepath.Glob(filepath.Join(home, "artifacts", "v1", "materializations", manifest.ArtifactDigest.Hex(), "leases", "*.json"))
			if len(leases) != 0 {
				t.Fatal("lease leak", leases)
			}
			t.Logf("artifact=%s materialization=%s renewal=11 revoke=12 target=%s runningDecision=%s", digest, cold.MaterializationID, target, original.DecisionID)
		})
	}
	after, err := os.ReadFile(archive)
	if err != nil || sha256.Sum256(after) != archiveHash {
		t.Fatal("operator lifecycle changed immutable archive", err)
	}
}
