package conda

import (
	"strings"
	"testing"
)

func TestRattlerDiagnosticsAreBoundedAndDoNotExposeSecrets(t *testing.T) {
	for input, want := range map[string]string{
		"401 secret":                      "channel authentication or authorization failed",
		"invalid peer certificate secret": "TLS or certificate failure",
		"solver failed secret":            "dependency resolution failed",
		"unknown secret":                  "helper failed; diagnostic details withheld",
	} {
		if got := rattlerDiagnostic(input); got != want || strings.Contains(got, "secret") {
			t.Errorf("diagnostic = %q, want %q", got, want)
		}
	}
	var output boundedRattlerOutput
	data := strings.Repeat("x", 20000)
	if n, err := output.Write([]byte(data)); err != nil || n != len(data) || output.buf.Len() != 16384 {
		t.Fatalf("bounded write: n=%d len=%d err=%v", n, output.buf.Len(), err)
	}
}
