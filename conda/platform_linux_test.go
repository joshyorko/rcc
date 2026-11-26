package conda

import (
	"runtime"
	"strings"
	"testing"
)

func TestLinuxMicromambaLinkMatchesArchitecture(t *testing.T) {
	platform := "linux64"
	if runtime.GOARCH == "arm64" {
		platform = "linuxarm64"
	}
	if got := MicromambaLink(); !strings.Contains(got, "/"+platform+"/micromamba") {
		t.Fatalf("micromamba link %q does not match %s", got, runtime.GOARCH)
	}
}
