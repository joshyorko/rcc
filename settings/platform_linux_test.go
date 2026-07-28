//go:build linux

package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

func TestOperatingSystemFromOSRelease(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	releaseFile := filepath.Join(t.TempDir(), "os-release")
	err := os.WriteFile(releaseFile, []byte(`NAME="Dudley OS"
PRETTY_NAME="Dudley OS (nvidia-latest)"
ID=bluefin
ID_LIKE="fedora"
VERSION_ID=44
VARIANT_ID=bluefin-nvidia
OSTREE_VERSION='testing-44.20260720'
IMAGE_ID=dudley-os
IMAGE_VERSION=nvidia-latest
`), 0o600)
	must_be.Nil(err)

	details, ok := operatingSystemFromFiles([]string{
		filepath.Join(t.TempDir(), "missing"),
		releaseFile,
	})

	must_be.True(ok)
	must_be.Equal(
		"Dudley OS (nvidia-latest) [id=bluefin; id-like=fedora; version=44; variant=bluefin-nvidia; image=dudley-os:nvidia-latest; ostree=testing-44.20260720]",
		details,
	)
}
