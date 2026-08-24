package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
)

func TestCacheServeCommandDefaultsToEphemeralLoopback(t *testing.T) {
	var gotRoot, gotListen string
	command := newCacheCommand(cacheCommandDependencies{
		serve: func(_ context.Context, root, listen string, _ io.Writer) error {
			gotRoot, gotListen = root, listen
			return nil
		},
	})
	arguments := []string{"serve", "--root", "provider", "--json"}
	if err := runCobraCommand(command, arguments); err != nil {
		t.Fatal(err)
	}
	if gotRoot != "provider" || gotListen != "127.0.0.1:0" {
		t.Fatalf("cache serve root/listen = %q/%q", gotRoot, gotListen)
	}
}

func TestCacheServeCommandPassesProviderLimits(t *testing.T) {
	var got artifactprovider.Limits
	command := newCacheCommand(cacheCommandDependencies{
		serveWithLimit: func(_ context.Context, _, _ string, _ io.Writer, limits artifactprovider.Limits) error {
			got = limits
			return nil
		},
	})
	if err := runCobraCommand(command, []string{"serve", "--root", "provider", "--json", "--max-bytes", "10", "--max-objects", "2", "--max-manifests", "3", "--max-uploads", "4", "--requests-per-second", "5"}); err != nil {
		t.Fatal(err)
	}
	if got != (artifactprovider.Limits{MaxBytes: 10, MaxObjects: 2, MaxManifests: 3, MaxUploads: 4, RequestsPerSecond: 5}) {
		t.Fatalf("provider limits = %+v", got)
	}
}

func TestCacheServeRejectsNonLoopbackListen(t *testing.T) {
	if err := serveArtifactCache(context.Background(), t.TempDir(), "0.0.0.0:0", io.Discard); err == nil {
		t.Fatal("cache provider accepted non-loopback listen address")
	}
}

func TestCacheServePublishesJSONAndShutsDownWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	root := filepath.Join(t.TempDir(), "provider")
	go func() {
		done <- serveArtifactCache(ctx, root, "127.0.0.1:0", writer)
		_ = writer.Close()
	}()

	var started cacheServeResult
	if err := json.NewDecoder(reader).Decode(&started); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(started.URL + "/v1/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || started.Root != root {
		t.Fatalf("started cache provider = %+v, status=%d", started, response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cache provider did not shut down")
	}
}

func TestCacheServePolicyLimitsAreVisibleOverHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	root := filepath.Join(t.TempDir(), "provider")
	go func() {
		done <- serveArtifactCacheWithOptions(ctx, root, "127.0.0.1:0", writer, artifactprovider.Limits{RequestsPerSecond: 1})
		_ = writer.Close()
	}()

	var started cacheServeResult
	if err := json.NewDecoder(reader).Decode(&started); err != nil {
		t.Fatal(err)
	}
	first, err := http.Post(started.URL+"/v1/objects/missing", "application/json", strings.NewReader(`{"descriptors":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Body.Close()
	second, err := http.Post(started.URL+"/v1/objects/missing", "application/json", strings.NewReader(`{"descriptors":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = second.Body.Close()
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("policy health statuses = %d/%d, want 200/429", first.StatusCode, second.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
