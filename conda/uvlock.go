package conda

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
)

// UvArchivePin describes an archive whose digest was obtained from a trusted
// source. It does not authenticate the source of the digest itself.
type UvArchivePin struct {
	Version string
	Target  string
	SHA256  string
}

var exactUvVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// VerifyUvArchive verifies an existing archive without network access. Callers
// must authenticate the pin independently and verify before extracting it.
func VerifyUvArchive(pin UvArchivePin, archive string) error {
	if !exactUvVersion.MatchString(pin.Version) {
		return fmt.Errorf("uv pin requires an exact stable version")
	}
	target := ""
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64":
		target = uvPlatformTarget()
	default:
		return fmt.Errorf("unsupported uv platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if pin.Target != target {
		return fmt.Errorf("uv pin target %q does not match %q", pin.Target, target)
	}
	expected, err := hex.DecodeString(pin.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("uv pin requires a SHA-256 digest")
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(h.Sum(nil), expected) != 1 {
		return fmt.Errorf("uv archive SHA-256 mismatch")
	}
	return nil
}
