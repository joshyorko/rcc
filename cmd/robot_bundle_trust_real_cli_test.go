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
	now := time.Now().UTC()
	revoked := []artifacttrust.Revocation{{KeyIDs: []string{"deployment"}, UpdatedAt: now.Format(time.RFC3339), Reason: "fixture revocation"}}
	cases := []struct {
		name, kind, reason string
		allow              bool
	}{
		{"signed-allow", "signed", "", true},
		{"unsigned-strict-deny", "unsigned", "unsigned", false},
		{"revoked-deny", "revoked", "revoked", false},
		{"tampered-signature-deny", "tampered", "invalid", false},
		{"tampered-archive-deny", "archive-tampered", "digest", false},
		{"unknown-signer-deny", "unknown", "unknown-signer", false},
		{"missing-evidence-deny", "missing", "revocation-stale", false},
		{"invalid-evidence-deny", "invalid", "attachment", false},
		{"permissive-legacy-allow", "permissive", "", true},
		{"lease-refresh-revocation-deny", "refresh", "revoked", false},
	}
	baseline := os.Getenv("RCC_TRUST_BASELINE") == "1"
	if baseline {
		cases = cases[1:2]
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			caller, home, temporary := filepath.Join(root, "caller"), filepath.Join(root, "home"), filepath.Join(root, "temp")
			for _, p := range []string{caller, home, temporary} {
				if err := os.Mkdir(p, 0750); err != nil {
					t.Fatal(err)
				}
			}
			// The sentinel is outside both task workarea and caller artifacts. A
			// denied task cannot hide execution behind failed copy-back or cleanup.
			sentinel := filepath.Join(root, "task-executed.txt")
			bundle := filepath.Join(root, "bundle.zip")
			selectedArchive := archive
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
			args := []string{"robot", "run-from-bundle", bundle, "--task", "proof"}
			var refreshReads atomic.Int32
			if !baseline {
				args = append(args, "--trust-roots", roots)
				if tc.kind == "refresh" {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						name := strings.TrimPrefix(r.URL.Path, "/")
						data, err := carrier.Read(name)
						if err != nil {
							http.NotFound(w, r)
							return
						}
						if name == artifacttrust.AttachmentName(digest, "revocations") && refreshReads.Add(1) > 1 {
							data = revokedData
						}
						_, _ = w.Write(data)
					}))
					defer server.Close()
					args = append(args, "--trust-carrier", server.URL, "--trust-carrier-type", "http")
				} else {
					args = append(args, "--trust-carrier", carrier.Root, "--trust-carrier-type", "filesystem")
				}
				if tc.kind == "permissive" {
					args = append(args, "--permissive-local")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, args...)
			command.Dir = caller
			command.Env = append(os.Environ(), "ROBOCORP_HOME="+home, "TMPDIR="+temporary, "TEMP="+temporary, "TMP="+temporary, "RCC_HOLOTREE_MODE=private", "CONDA_OFFLINE=true", "MAMBA_OFFLINE=true", "PIP_NO_INDEX=1", "UV_NO_INDEX=1")
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			runErr := command.Run()
			if ctx.Err() != nil {
				t.Fatal("CLI timed out")
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
				if _, err := os.Stat(filepath.Join(caller, "output")); !os.IsNotExist(err) {
					t.Fatalf("denied task produced caller artifacts: %v", err)
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
			receipts := 0
			leased := false
			_ = filepath.WalkDir(filepath.Join(home, "artifacts", "v1", "verification"), func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil || d.IsDir() {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				var receipt artifacttrust.VerificationReceipt
				if json.Unmarshal(data, &receipt) == nil && receipt.ArtifactDigest == digest {
					receipts++
					leased = leased || receipt.LeaseID != ""
				}
				return nil
			})
			if !baseline && tc.allow && (!leased || receipts < 2) {
				t.Fatalf("missing acquisition/lease receipts: count=%d leased=%v", receipts, leased)
			}
			leases, _ := filepath.Glob(filepath.Join(home, "artifacts", "v1", "materializations", manifest.ArtifactDigest.Hex(), "leases", "*.json"))
			if len(leases) != 0 {
				t.Fatalf("lease leak: %v", leases)
			}
			t.Logf("binary=%s artifact=%s exit=%d executed=%v trustReceipts=%d leaseReceipt=%v refreshReads=%d baseline=%v", binary, digest, exit, sentinelErr == nil, receipts, leased, refreshReads.Load(), baseline)
		})
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
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
