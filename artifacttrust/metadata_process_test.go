package artifacttrust

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMetadataStateIndependentProcesses(t *testing.T) {
	if root := os.Getenv("RCC_METADATA_PROCESS_FIXTURE"); root != "" {
		data, err := os.ReadFile(filepath.Join(root, "input.json"))
		if err != nil {
			t.Fatal(err)
		}
		var q struct {
			Payload  MetadataPayload
			Envelope MetadataEnvelope
			Roots    MetadataRoots
			At       time.Time
		}
		if err := json.Unmarshal(data, &q); err != nil {
			t.Fatal(err)
		}
		verified, err := VerifyMetadata(q.Envelope, q.Roots, q.Payload.ArtifactDigest, q.At)
		if err != nil {
			t.Fatal(err)
		}
		if err := (MetadataState{Root: filepath.Join(root, "state")}).Admit(verified, q.Envelope); err != nil {
			t.Fatal(err)
		}
		return
	}
	p, roots, key, at := metadataFixture(t)
	envelope, err := SignMetadata(p, key)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	data, err := json.Marshal(struct {
		Payload  MetadataPayload
		Envelope MetadataEnvelope
		Roots    MetadataRoots
		At       time.Time
	}{p, envelope, roots, at})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "input.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		output bytes.Buffer
	}
	children := make([]*child, 4)
	for i := range children {
		c := &child{cmd: exec.CommandContext(ctx, executable, "-test.run=^TestMetadataStateIndependentProcesses$")}
		c.cmd.Env = append(os.Environ(), "RCC_METADATA_PROCESS_FIXTURE="+root)
		c.cmd.Stdout, c.cmd.Stderr = &c.output, &c.output
		children[i] = c
		if err := c.cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range children {
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("independent admission failed: %v %s", err, c.output.String())
		}
	}
	files, err := filepath.Glob(filepath.Join(root, "state", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	journal, err := os.ReadFile(files[0])
	if err != nil || bytes.Count(journal, []byte{'\n'}) != 1 {
		t.Fatalf("concurrent admission corrupted/duplicated journal: %q %v", journal, err)
	}
	// New process identities are gone; the retained checkpoint still rejects
	// a conflicting authority statement at the already admitted sequence.
	p.Revocations = []Revocation{{KeyIDs: []string{"revoked"}}}
	conflicting, err := SignMetadata(p, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := (MetadataState{Root: filepath.Join(root, "state")}).Admit(p, conflicting); !metadataCodeIs(err, CodeMetadataRollback) {
		t.Fatalf("post-process equivocation: %v", err)
	}
}
