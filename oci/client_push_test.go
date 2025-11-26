package oci

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestClientPushWithBearerAuth(t *testing.T) {
	const (
		username = "user"
		password = "pass"
	)

	content := []byte(`{"sbom":true}`)
	configDigest := calculateDigest([]byte("{}"))
	contentDigest := calculateDigest(content)
	manifestDigest := "sha256:manifestdigest"
	expectedBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	expectedBearer := "Bearer bearertoken"

	expectedDigests := []string{configDigest, contentDigest}
	var tokenRequested bool
	uploadCalls := 0

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping, cannot start test registry listener: %v", err)
	}

	var server *httptest.Server
	server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.example",scope="repository:test/repo:pull,push"`, server.URL))
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/token":
			tokenRequested = true
			if got := r.Header.Get("Authorization"); got != expectedBasic {
				t.Errorf("token exchange used auth %q, want %q", got, expectedBasic)
			}
			if service := r.URL.Query().Get("service"); service != "registry.example" {
				t.Errorf("token exchange service %q", service)
			}
			if scope := r.URL.Query().Get("scope"); scope != "repository:test/repo:pull,push" {
				t.Errorf("token exchange scope %q", scope)
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"token":"bearertoken"}`)
		case strings.HasPrefix(r.URL.Path, "/v2/test/repo/blobs/") && r.Method == http.MethodHead:
			if got := r.Header.Get("Authorization"); got != expectedBearer {
				t.Errorf("blob HEAD auth %q, want %q", got, expectedBearer)
			}
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/v2/test/repo/blobs/uploads/" && r.Method == http.MethodPost:
			if got := r.Header.Get("Authorization"); got != expectedBearer {
				t.Errorf("upload init auth %q, want %q", got, expectedBearer)
			}
			uploadCalls++
			location := fmt.Sprintf("/upload/%d", uploadCalls)
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusAccepted)
		case strings.HasPrefix(r.URL.Path, "/upload/") && r.Method == http.MethodPut:
			if got := r.Header.Get("Authorization"); got != expectedBearer {
				t.Errorf("upload auth %q, want %q", got, expectedBearer)
			}
			index, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/upload/"))
			if index <= 0 || index > len(expectedDigests) {
				t.Fatalf("unexpected upload index %d", index)
			}
			if got := r.URL.Query().Get("digest"); got != expectedDigests[index-1] {
				t.Errorf("upload digest %q, want %q", got, expectedDigests[index-1])
			}
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusCreated)
		case r.URL.Path == "/v2/test/repo/manifests/latest":
			if got := r.Header.Get("Authorization"); got != expectedBearer {
				t.Errorf("manifest auth %q, want %q", got, expectedBearer)
			}
			w.Header().Set("Docker-Content-Digest", manifestDigest)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	client := NewClient(Config{
		Registry: fmt.Sprintf("%s/test/repo", server.URL),
		Tag:      "latest",
		Username: username,
		Password: password,
	})

	result, err := client.Push(context.Background(), content, "application/test")
	if err != nil {
		t.Fatalf("unexpected push error: %v", err)
	}

	if !tokenRequested {
		t.Fatalf("token endpoint was not called")
	}
	if got := uploadCalls; got != len(expectedDigests) {
		t.Fatalf("expected %d uploads, got %d", len(expectedDigests), got)
	}
	if result.Digest != manifestDigest {
		t.Fatalf("result digest %q, want %q", result.Digest, manifestDigest)
	}
	if result.Tag != "latest" {
		t.Fatalf("result tag %q, want latest", result.Tag)
	}
}
