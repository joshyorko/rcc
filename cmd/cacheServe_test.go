package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"
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
