package artifacttrust

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPAttachmentVerificationBindsArtifact(t *testing.T) {
	artifact := "sha256:http"
	data := []byte(`{"mediaType":"application/vnd.rcc.environment.provenance.v1+json","artifactDigest":"sha256:http"}`)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || (r.URL.Path != "/sha256:http/provenance.json" && r.URL.Path != "/sha256:other/provenance.json") {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write(data)
	}))
	defer s.Close()
	got, err := GetAttachment(&HTTPCarrier{BaseURL: s.URL}, artifact, "provenance")
	if err != nil || string(got) != string(data) {
		t.Fatalf("got=%s err=%v", got, err)
	}
	if _, err := GetAttachment(&HTTPCarrier{BaseURL: s.URL}, "sha256:other", "provenance"); err == nil {
		t.Fatal("mismatched HTTP attachment accepted")
	}
}
