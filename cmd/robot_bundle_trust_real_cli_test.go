package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/internal/trustfixture"
)

// This opt-in test drives an actual RCC executable and an exported, native
// environment archive. It never substitutes a mocked interpreter/materializer.
func TestBundleTrustRealCLI(t *testing.T) {
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
	defer zr.Close()
	var manifest environmentartifact.Manifest
	for _, f := range zr.File {
		if f.Name != environmentartifact.ArchiveManifest {
			continue
		}
		r, err := f.Open()
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
	if manifest.ArtifactDigest.Hex() == "" {
		t.Fatal("archive has no manifest")
	}
	digest := manifest.ArtifactDigest.String()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := artifacttrust.Sign(digest, "deployment", private)
	if err != nil {
		t.Fatal(err)
	}
	metadataPublic, metadataPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	revoked := []artifacttrust.Revocation{{KeyIDs: []string{"deployment"}, UpdatedAt: now.Format(time.RFC3339), Reason: "fixture revocation"}}
	cases := []struct {
		name, kind, reason string
		allow              bool
	}{
		{"signed-allow", "signed", "", true},
		{"offline-archive-carrier-allow", "offline-carrier", "", true},
		{"unsigned-strict-deny", "unsigned", "unsigned", false},
		{"revoked-deny", "revoked", "revoked", false},
		{"tampered-signature-deny", "tampered", "invalid", false},
		{"tampered-archive-deny", "archive-tampered", "digest", false},
		{"unknown-signer-deny", "unknown", "unknown-signer", false},
		{"missing-evidence-deny", "missing", "metadata-unsigned", false},
		{"invalid-evidence-deny", "invalid", "attachment", false},
		{"mutated-metadata-deny", "metadata-mutated", "metadata-invalid", false},
		{"unauthorized-metadata-deny", "metadata-unknown", "metadata-authority", false},
		{"permissive-legacy-allow", "permissive", "", true},
		{"lease-refresh-revocation-deny", "refresh", "revoked", false},
		{"explicit-strict-source-only-deny", "source-only", "requires an embedded", false},
	}
	baseline := os.Getenv("RCC_TRUST_BASELINE") == "1"
	// A warm private home tests that a prior valid receipt cannot authorize
	// later execution under missing, revoked or invalid evidence.
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0750); err != nil {
		t.Fatal(err)
	}
	if baseline {
		cases = cases[1:2]
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			caller, temporary := filepath.Join(root, "caller"), filepath.Join(root, "temp")
			for _, p := range []string{caller, temporary} {
				if err := os.Mkdir(p, 0750); err != nil {
					t.Fatal(err)
				}
			}
			// The sentinel is outside both task workarea and caller artifacts. A
			// denied task cannot hide execution behind failed copy-back or cleanup.
			sentinel := filepath.Join(root, "task-executed.txt")
			bundle := filepath.Join(root, "bundle.zip")
			selectedArchive := archive
			if tc.kind == "source-only" {
				selectedArchive = ""
			}
			if tc.kind == "archive-tampered" {
				selectedArchive = filepath.Join(root, "tampered.rcca")
				writeTamperedTrustArchive(t, archive, selectedArchive)
			}
			writeTrustCLIBundle(t, bundle, selectedArchive, sentinel)
			roots := filepath.Join(root, "roots.json")
			keys := map[string]string{"deployment": base64.RawStdEncoding.EncodeToString(public)}
			if tc.kind == "unknown" {
				keys = map[string]string{"other": base64.RawStdEncoding.EncodeToString(public)}
			}
			keyData, _ := json.Marshal(keys)
			if err := os.WriteFile(roots, keyData, 0600); err != nil {
				t.Fatal(err)
			}
			carrier := artifacttrust.NewFilesystemCarrier(filepath.Join(root, "carrier"))
			put := func(kind string, data []byte) {
				t.Helper()
				if err := artifacttrust.PutAttachment(carrier, digest, kind, data); err != nil {
					t.Fatal(err)
				}
			}
			_, revocationData, err := artifacttrust.NewRevocationBundleAt(digest, nil, now, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			_, revokedData, err := artifacttrust.NewRevocationBundleAt(digest, revoked, now, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind != "missing" {
				put("revocations", revocationData)
				if tc.kind != "unsigned" && tc.kind != "permissive" {
					sig := signature
					if tc.kind == "tampered" {
						raw, _ := base64.RawStdEncoding.DecodeString(sig.Signature)
						if len(raw) == 0 {
							raw, _ = base64.StdEncoding.DecodeString(sig.Signature)
						}
						raw[0] ^= 1
						sig.Signature = base64.RawStdEncoding.EncodeToString(raw)
					}
					_, data, err := artifacttrust.NewSignatureBundle(digest, []artifacttrust.Signature{sig})
					if err != nil {
						t.Fatal(err)
					}
					put("signature", data)
				}
			}
			if tc.kind == "revoked" {
				put("revocations", revokedData)
			}
			if tc.kind == "invalid" {
				if err := carrier.Write(artifacttrust.AttachmentName(digest, "signature"), []byte("{invalid")); err != nil {
					t.Fatal(err)
				}
			}
			payload := artifacttrust.MetadataPayload{SchemaVersion: 2, Scope: "bundle-fixture", ArtifactDigest: digest, Sequence: 1, KeyID: "independent-authority", IssuedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}
			metadataRoots := artifacttrust.MetadataRoots{SchemaVersion: 2, Scope: payload.Scope, MinimumSequence: 1, Keys: map[string]artifacttrust.MetadataKey{payload.KeyID: {PublicKey: base64.RawStdEncoding.EncodeToString(metadataPublic), NotBefore: now.Add(-time.Hour).Format(time.RFC3339), NotAfter: now.Add(2 * time.Hour).Format(time.RFC3339)}}}
			if tc.kind == "metadata-unknown" {
				metadataRoots.Keys = map[string]artifacttrust.MetadataKey{"unrelated": metadataRoots.Keys[payload.KeyID]}
			}
			metadataRootsPath := trustfixture.WriteJSON(t, root, "metadata-roots.json", metadataRoots)
			metadataState := filepath.Join(root, "retained-state")
			if tc.kind == "revoked" {
				payload.Revocations = revoked
			}
			envelope, err := artifacttrust.SignMetadata(payload, metadataPrivate)
			if err != nil {
				t.Fatal(err)
			}
			if tc.kind == "metadata-mutated" {
				payload.Sequence++
				payloadBytes, _ := json.Marshal(payload)
				envelope.Payload = base64.RawStdEncoding.EncodeToString(payloadBytes)
			}
			metadataBytes, _ := json.Marshal(envelope)
			if tc.kind != "missing" && tc.kind != "permissive" {
				put("metadata", metadataBytes)
			}
			payload.Sequence = 2
			payload.Revocations = revoked
			revokedEnvelope, err := artifacttrust.SignMetadata(payload, metadataPrivate)
			if err != nil {
				t.Fatal(err)
			}
			revokedMetadata, _ := json.Marshal(revokedEnvelope)
			args := []string{"robot", "run-from-bundle", bundle, "--task", "proof"}
			var refreshReads atomic.Int32
			if !baseline {
				args = append(args, "--trust-roots", roots, "--metadata-trust-roots", metadataRootsPath, "--metadata-state", metadataState)
				if tc.kind == "source-only" {
					args = append(args, "--strict-remote")
				}
				if tc.kind == "refresh" {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						name := strings.TrimPrefix(r.URL.Path, "/")
						data, err := carrier.Read(name)
						if err != nil {
							http.NotFound(w, r)
							return
						}
						if name == artifacttrust.AttachmentName(digest, "metadata") && refreshReads.Add(1) > 1 {
							data = revokedMetadata
						}
						_, _ = w.Write(data)
					}))
					defer server.Close()
					args = append(args, "--trust-carrier", server.URL, "--trust-carrier-type", "http")
				} else if tc.kind == "offline-carrier" {
					carrierArchive := filepath.Join(root, "signed-carrier.zip")
					if err := artifacttrust.ExportArchive(carrier, carrierArchive, digest, []string{"signature", "metadata"}); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--trust-carrier", carrierArchive, "--trust-carrier-type", "archive")
				} else {
					args = append(args, "--trust-carrier", carrier.Root, "--trust-carrier-type", "filesystem")
				}
				if tc.kind == "permissive" {
					args = append(args, "--permissive-local")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, binary, args...)
			command.Dir = caller
			command.Env = append(os.Environ(), "ROBOCORP_HOME="+home, "TMPDIR="+temporary, "TEMP="+temporary, "TMP="+temporary, "RCC_HOLOTREE_MODE=private", "CONDA_OFFLINE=true", "MAMBA_OFFLINE=true", "PIP_NO_INDEX=1", "UV_NO_INDEX=1")
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			runErr := command.Run()
			if ctx.Err() != nil {
				t.Fatalf("CLI timed out: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			exit := command.ProcessState.ExitCode()
			_, sentinelErr := os.Stat(sentinel)
			wantAllow := tc.allow || baseline
			if wantAllow {
				if runErr != nil || sentinelErr != nil {
					t.Fatalf("allowed execution: exit=%d sentinel=%v stdout=%s stderr=%s", exit, sentinelErr, stdout.String(), stderr.String())
				}
				if !baseline {
					data, err := os.ReadFile(filepath.Join(caller, "output", "result.txt"))
					if err != nil || string(data) != "durable-trust-result\n" {
						t.Fatalf("result=%q err=%v", data, err)
					}
					if !strings.Contains(stdout.String(), "trust-child-stdout") || !strings.Contains(stderr.String(), "trust-child-stderr") {
						t.Fatal("child streams disappeared or crossed")
					}
				}
			} else {
				if runErr == nil || !os.IsNotExist(sentinelErr) {
					t.Fatalf("denied task executed: exit=%d sentinel=%v", exit, sentinelErr)
				}
				if _, err := os.Stat(filepath.Join(caller, "output", "result.txt")); !os.IsNotExist(err) {
					t.Fatalf("denied task produced its result artifact: %v", err)
				}
				if strings.Contains(stdout.String(), "trust-child-stdout") || strings.Contains(stderr.String(), "trust-child-stderr") {
					t.Fatal("denied task emitted child streams")
				}
				if !strings.Contains(stderr.String(), tc.reason) {
					t.Fatalf("missing reason %q: %s", tc.reason, stderr.String())
				}
			}
			if tc.kind == "refresh" && refreshReads.Load() < 2 {
				t.Fatal("lease refresh was not observed")
			}
			if tc.kind == "signed" && os.Getenv("RCC_BUNDLE_TEST_ENV_PROBES") == "1" {
				verifyCombinedEnvironmentCommands(t, binary, home, digest, roots, carrier.Root, metadataRootsPath, metadataState, command.Env)
			}
			history, historyErr := artifacttrust.NewReceiptStore(filepath.Join(home, "artifacts", "v1", "verification")).History(digest)
			receipts := len(history)
			leased := false
			for _, receipt := range history {
				leased = leased || receipt.LeaseID != ""
			}
			if !baseline && tc.allow && (!leased || receipts < 2) {
				t.Fatalf("missing acquisition/lease receipts: count=%d leased=%v", receipts, leased)
			}
			if !baseline && tc.kind != "archive-tampered" && tc.kind != "source-only" {
				if historyErr != nil || len(history) == 0 {
					t.Fatalf("trust history unavailable: %v", historyErr)
				}
				last := history[len(history)-1]
				if last.Valid != tc.allow {
					t.Fatalf("trust receipt valid=%v, allowed=%v", last.Valid, tc.allow)
				}
				wantMode := artifacttrust.StrictRemote
				if tc.kind == "permissive" {
					wantMode = artifacttrust.PermissiveLocal
				}
				if last.PolicyMode != wantMode {
					t.Fatalf("receipt policy=%s want=%s", last.PolicyMode, wantMode)
				}
			}
			leases, _ := filepath.Glob(filepath.Join(home, "artifacts", "v1", "materializations", manifest.ArtifactDigest.Hex(), "leases", "*.json"))
			if len(leases) != 0 {
				t.Fatalf("lease leak: %v", leases)
			}
			t.Logf("binary=%s artifact=%s exit=%d executed=%v trustReceipts=%d leaseReceipt=%v refreshReads=%d baseline=%v", binary, digest, exit, sentinelErr == nil, receipts, leased, refreshReads.Load(), baseline)
		})
	}
}

func verifyCombinedEnvironmentCommands(t *testing.T, binary, home, digest, roots, carrier, metadataRoots, metadataState string, environment []string) {
	t.Helper()
	host := filepath.Join(t.TempDir(), "host-bin")
	if err := os.Mkdir(host, 0750); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(host, "rcc-issue258-probe")
	if err := os.WriteFile(decoy, []byte("#!/bin/sh\nprintf 'host-decoy\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	environment = append(append([]string{}, environment...), "PATH="+host+string(os.PathListSeparator)+os.Getenv("PATH"))
	for index, probe := range []struct {
		command    []string
		output     string
		json, fail bool
	}{
		{[]string{"rcc-issue258-unique"}, "artifact-stdout\n", false, false},
		{[]string{"rcc-issue258-probe"}, "artifact-stdout\n", false, false},
		{[]string{"rcc-issue258-probe"}, "", true, false},
		{[]string{decoy}, "host-decoy\n", false, false},
		{[]string{"python", "-c", "print('artifact-python')"}, "artifact-python\n", false, false},
		{[]string{"rcc-issue258-missing"}, "", false, true},
	} {
		args := []string{"env", "exec", "--artifact", digest, "--trust-roots", roots, "--trust-carrier", carrier, "--strict-remote", "--metadata-trust-roots", metadataRoots, "--metadata-state", metadataState}
		receiptPath := filepath.Join(home, fmt.Sprintf("combined-command-%d.json", index))
		if probe.json {
			args = append(args, "--json")
		} else {
			args = append(args, "--inherit-streams", "--receipt-file", receiptPath)
		}
		args = append(append(args, "--"), probe.command...)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = environment
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		cancel()
		if (err != nil) != probe.fail {
			t.Fatalf("combined probe %v err=%v stdout=%s stderr=%s", probe.command, err, stdout.String(), stderr.String())
		}
		var receipt environmentExecResult
		if probe.json {
			if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
				t.Fatalf("JSON channel contains child output: %s", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("JSON mode leaked child stderr: %s", stderr.String())
			}
		} else {
			data, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &receipt); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != probe.output {
				t.Fatalf("combined probe %v stdout=%q want=%q", probe.command, stdout.String(), probe.output)
			}
			if index < 2 && stderr.String() != "artifact-stderr\n" {
				t.Fatalf("combined probe stderr=%q", stderr.String())
			}
		}
		if receipt.ArtifactDigest.String() != digest || receipt.Verification == nil || !receipt.Verification.Valid || receipt.Verification.PolicyMode != artifacttrust.StrictRemote {
			t.Fatalf("combined probe lost strict receipt: %+v", receipt)
		}
		if probe.fail && (receipt.Status != "failed" || !strings.Contains(receipt.Reason, "rcc-issue258-missing")) {
			t.Fatalf("lookup failure receipt=%+v", receipt)
		}
		if !probe.fail && (receipt.Status != "completed" || receipt.ExitCode != 0 || receipt.LeaseID == "") {
			t.Fatalf("execution receipt=%+v", receipt)
		}
		t.Logf("combined command=%v status=%s exit=%d mode=%s", probe.command, receipt.Status, receipt.ExitCode, receipt.Verification.PolicyMode)
	}
}

func writeTamperedTrustArchive(t *testing.T, archive, target string) {
	t.Helper()
	source, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	out, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	changed := false
	for _, file := range source.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !changed && strings.HasPrefix(file.Name, environmentartifact.ArchiveObjectDirectory) && len(data) > 0 {
			data[0] ^= 1
			changed = true
		}
		member, err := w.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("no artifact object was tampered")
	}
}

func writeTrustCLIBundle(t *testing.T, bundle, archive, sentinel string) {
	t.Helper()
	f, err := os.Create(bundle)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	marker, _ := json.Marshal(sentinel)
	files := map[string]string{
		"robot/robot.yaml": "tasks:\n  proof:\n    command: [python, task.py]\nartifactsDir: output\n",
		"robot/task.py":    fmt.Sprintf("from pathlib import Path\nimport sys\nPath(%s).write_text('executed\\n')\nPath('output').mkdir(exist_ok=True)\nPath('output/result.txt').write_text('durable-trust-result\\n')\nprint('trust-child-stdout',flush=True)\nprint('trust-child-stderr',file=sys.stderr,flush=True)\n", marker),
	}
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, data); err != nil {
			t.Fatal(err)
		}
	}
	if archive != "" {
		w, err := zw.Create("environment/artifact.rcca")
		if err != nil {
			t.Fatal(err)
		}
		source, err := os.Open(archive)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(w, source)
		_ = source.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
