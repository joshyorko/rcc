package blobs

import (
	"crypto/sha256"
	"fmt"
	"io"
	"testing"
)

func TestProviderDefaultMicromamba(t *testing.T) {
	r, version, digest, err := OpenProviderDefault("micromamba")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || version != "v2.10.0" || fmt.Sprintf("%x", h.Sum(nil)) != digest {
		t.Fatalf("invalid embedded executable: size=%d version=%s digest=%s", n, version, digest)
	}
}

func TestProviderDefaultUnavailable(t *testing.T) {
	if r, _, _, err := OpenProviderDefault("unknown"); err == nil {
		_ = r.Close()
		t.Fatal("unknown provider succeeded")
	}
}
