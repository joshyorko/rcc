package artifactprovider

import (
	"context"
	"testing"

	"github.com/joshyorko/rcc/environmentartifact"
)

func TestObjectFilesystemDoesNotReplayAdministrativeHistory(t *testing.T) {
	root := t.TempDir()
	provider, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	digest := environmentartifact.DigestBytes([]byte("record"))
	for range 3 {
		if err := provider.appendAudit("test", digest); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := NewFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if admin.requests.Load() != 3 {
		t.Fatal("existing administrative constructor changed")
	}
	objects, err := NewObjectFilesystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if objects.requests.Load() != 0 {
		t.Fatal("object constructor replayed history")
	}
	records, err := objects.Audit(context.Background())
	if err != nil || len(records) != 3 {
		t.Fatal("object constructor changed persistent audit history", err)
	}
}
