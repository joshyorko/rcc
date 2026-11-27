package conda

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

func TestVerifyUvArchive(t *testing.T) {
	must, mustNot := hamlet.Specifications(t)
	archive := filepath.Join(t.TempDir(), "uv.tar.gz")
	data := []byte("archive bytes")
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	pin := UvArchivePin{Version: "0.9.1", Target: uvPlatformTarget(), SHA256: fmt.Sprintf("%x", digest)}
	must.Nil(VerifyUvArchive(pin, archive))
	for _, bad := range []UvArchivePin{
		{Version: "latest", Target: pin.Target, SHA256: pin.SHA256},
		{Version: pin.Version, Target: "other", SHA256: pin.SHA256},
		{Version: pin.Version, Target: pin.Target, SHA256: "bad"},
		{Version: pin.Version, Target: pin.Target, SHA256: fmt.Sprintf("%064d", 0)},
	} {
		mustNot.Nil(VerifyUvArchive(bad, archive))
	}
	if err := os.WriteFile(archive, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustNot.Nil(VerifyUvArchive(pin, archive))
}
