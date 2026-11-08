package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/internal/trustfixture"
)

func TestRevocationOperatorRequiresExplicitInputs(t *testing.T) {
	for _, action := range []string{"issue", "renew", "revoke", "distribute"} {
		t.Run(action, func(t *testing.T) {
			command := newEnvironmentTrustCommand()
			var out bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&out)
			command.SetArgs([]string{"revocations", action, "--json"})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), "--artifact") {
				t.Fatalf("operator command must require explicit artifact and deployment inputs: %v %s", err, out.String())
			}
		})
	}
}

func TestRevocationOperatorRejectsArchiveAndCarrierOwnedState(t *testing.T) {
	root := t.TempDir()
	q := trustfixture.Authenticate(t, artifacttrust.VerifyRequest{ArtifactDigest: artifacttrust.Digest([]byte("operator boundary"))})
	roots := trustfixture.WriteJSON(t, root, "roots.json", q.MetadataRoots)
	envelope := trustfixture.WriteJSON(t, root, "envelope.json", q.Metadata)
	for _, kind := range []string{"archive", "carrier-state"} {
		t.Run(kind, func(t *testing.T) {
			carrier := filepath.Join(root, kind+"-carrier")
			state := filepath.Join(root, kind+"-state")
			carrierType := "filesystem"
			if kind == "archive" {
				carrierType = "archive"
				carrier = filepath.Join(root, "carrier.zip")
				file, err := os.Create(carrier)
				if err != nil {
					t.Fatal(err)
				}
				if err = zip.NewWriter(file).Close(); err != nil {
					t.Fatal(err)
				}
				if err = file.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				state = filepath.Join(carrier, "state")
			}
			args := []string{"revocations", "distribute", "--artifact", q.ArtifactDigest, "--metadata", envelope, "--metadata-trust-roots", roots, "--metadata-state", state, "--trust-carrier", carrier, "--trust-carrier-type", carrierType, "--json"}
			command := newEnvironmentTrustCommand()
			var stdout bytes.Buffer
			command.SetOut(&stdout)
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(args)
			if err := command.Execute(); err == nil {
				t.Fatal("unsafe distribution accepted")
			}
			var r artifacttrust.MetadataOperatorReceipt
			if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || r.Valid || r.Retained || r.Distributed {
				t.Fatal(r, err)
			}
			if _, err := os.Stat(state); !os.IsNotExist(err) {
				t.Fatalf("rejected distribution created retained state: %v", err)
			}
		})
	}
}

func TestRevocationOperatorNeverOverwritesOutput(t *testing.T) {
	root := t.TempDir()
	_, private := trustVerifyTestKey()
	q := trustfixture.Authenticate(t, artifacttrust.VerifyRequest{ArtifactDigest: artifacttrust.Digest([]byte("operator exclusive output"))})
	roots := trustfixture.WriteJSON(t, root, "roots.json", q.MetadataRoots)
	revocations := trustfixture.WriteJSON(t, root, "revocations.json", []artifacttrust.Revocation{})
	key := filepath.Join(root, "disposable.key")
	if err := os.WriteFile(key, private, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "existing.json")
	original := []byte("operator-owned existing asset")
	if err := os.WriteFile(output, original, 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "journal")
	command := newEnvironmentTrustCommand()
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"revocations", "issue", "--artifact", q.ArtifactDigest, "--metadata-trust-roots", roots, "--metadata-state", state, "--metadata-key-id", "test", "--metadata-signing-key", key, "--sequence", "1", "--issued-at", "2026-11-07T00:00:00Z", "--expires-at", "2026-11-07T01:00:00Z", "--revocations", revocations, "--output", output, "--json"})
	if err := command.Execute(); err == nil {
		t.Fatal("existing output accepted")
	}
	var r artifacttrust.MetadataOperatorReceipt
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || r.Code != "metadata-output" || r.Retained {
		t.Fatal(r, err)
	}
	after, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("overwrote operator output", err)
	}
	if _, err = os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("output conflict advanced state", err)
	}
}
