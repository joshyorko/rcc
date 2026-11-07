package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/internal/trustfixture"
)

func TestMetadataOptionsRejectCarrierAliasesBeforeCreatingState(t *testing.T) {
	root := t.TempDir()
	carrier := filepath.Join(root, "carrier")
	if err := os.Mkdir(carrier, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(carrier, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	q := trustfixture.Authenticate(t, artifacttrust.VerifyRequest{ArtifactDigest: artifacttrust.Digest([]byte("fixture"))})
	roots := trustfixture.WriteJSON(t, root, "roots.json", q.MetadataRoots)
	var request *artifacttrust.VerifyRequest
	state := filepath.Join(alias, "not-created", "state")
	if err := (metadataOptions{roots: roots, state: state}).apply(&request, artifacttrust.NewFilesystemCarrier(carrier)); err == nil {
		t.Fatal("carrier alias accepted as retained trusted state")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("denied state path was created: %v", err)
	}
	if err := (metadataOptions{roots: roots, state: filepath.Join(root, "independent-state")}).apply(&request, artifacttrust.NewFilesystemCarrier(carrier)); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataSignerRequiresExplicitRevocationSnapshot(t *testing.T) {
	command := newMetadataSignCommand()
	err := runCobraCommand(command, []string{"--metadata-signing-key", "unused", "--output", filepath.Join(t.TempDir(), "unsigned-default.json")})
	if err == nil || !strings.Contains(err.Error(), "explicit --revocations") {
		t.Fatalf("missing reviewed snapshot did not fail before signing: %v", err)
	}
}

func TestMetadataEnvelopeFileReadIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", (3<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	var envelope artifacttrust.MetadataEnvelope
	if err := readMetadataEnvelopeFile(path, &envelope); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("unbounded metadata input: %v", err)
	}
}
