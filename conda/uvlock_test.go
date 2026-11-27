package conda

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyUvArchive(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "uv.tar.gz")
	data := []byte("archive bytes")
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	pin := UvArchivePin{Version: "0.9.1", Target: uvPlatformTarget(), SHA256: fmt.Sprintf("%x", digest)}
	if err := VerifyUvArchive(pin, archive); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []UvArchivePin{
		{Version: "latest", Target: pin.Target, SHA256: pin.SHA256},
		{Version: pin.Version, Target: "other", SHA256: pin.SHA256},
		{Version: pin.Version, Target: pin.Target, SHA256: "bad"},
		{Version: pin.Version, Target: pin.Target, SHA256: fmt.Sprintf("%064d", 0)},
	} {
		if err := VerifyUvArchive(bad, archive); err == nil {
			t.Fatalf("accepted invalid pin: %+v", bad)
		}
	}
	if err := os.WriteFile(archive, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyUvArchive(pin, archive); err == nil {
		t.Fatal("accepted modified archive")
	}
}
