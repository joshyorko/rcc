package artifactprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNormalizeHTTPURL(t *testing.T) {
	for _, raw := range []string{"https://example.test", "http://localhost", "http://127.42.0.1", "http://[::1]"} {
		got, err := NormalizeHTTPURL(raw)
		if err != nil || (got != raw && got != "https://example.test") {
			t.Fatalf("NormalizeHTTPURL(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"https://user@example.test", "https://user:secret@example.test", "https://example.test?x=1", "https://example.test#fragment", "https://example.test/v1", "http://example.test", "http://localhost.example.test"} {
		if _, err := NormalizeHTTPURL(raw); err == nil {
			t.Fatalf("NormalizeHTTPURL(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestHTTPAuthorizationAndRejectsRedirect(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	const env = "RCC_TEST_AUTH"
	t.Setenv(env, "Bearer test-secret-value")
	client, err := NewHTTPWithOptions(source.URL, HTTPOptions{Client: source.Client(), AuthorizationEnv: env})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Capabilities(context.Background())
	if err == nil || redirected.Load() || strings.Contains(err.Error(), "test-secret-value") {
		t.Fatalf("redirect=%v err=%v", redirected.Load(), err)
	}
}

func TestHTTPAuthorizationUsesCompleteValueAndNamesMissingVariable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret-value" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`))
	}))
	defer server.Close()
	const env = "RCC_TEST_AUTH_MISSING"
	os.Unsetenv(env)
	client, err := NewHTTPWithOptions(server.URL, HTTPOptions{Client: server.Client(), AuthorizationEnv: env})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Capabilities(context.Background()); err == nil || !strings.Contains(err.Error(), env) || strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("missing auth error = %v", err)
	}
	t.Setenv(env, "Bearer test-secret-value")
	if _, err := client.Capabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
}
