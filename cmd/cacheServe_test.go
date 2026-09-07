package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestCacheServeCommandDefaultsToCanonicalRootAndHumanOutput(t *testing.T) {
	home := filepath.Join(t.TempDir(), "fresh-home")
	previousHome := common.Product.Home()
	common.Product.ForceHome(home)
	defer common.Product.ForceHome(previousHome)

	var gotRoot, gotListen, gotBackend string
	var gotJSON bool
	command := newCacheCommand(cacheCommandDependencies{
		serveConfiguredWithOutput: func(_ context.Context, root, listen string, _ io.Writer, backend string, _ artifactprovider.Limits, jsonOutput bool) error {
			gotRoot, gotListen, gotBackend, gotJSON = root, listen, backend, jsonOutput
			return nil
		},
	})
	if err := runCobraCommand(command, []string{"serve"}); err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(home, "artifacts", "v1", "provider")
	if gotRoot != wantRoot || gotListen != "127.0.0.1:0" || gotBackend != "filesystem" || gotJSON {
		t.Fatalf("cache serve defaults = root %q, listen %q, backend %q, json %t", gotRoot, gotListen, gotBackend, gotJSON)
	}
}

func TestCacheServeZeroConfigCreatesFreshHomeAndPrintsHumanStartup(t *testing.T) {
	home := filepath.Join(t.TempDir(), "fresh-home")
	previousHome := common.Product.Home()
	common.Product.ForceHome(home)
	defer common.Product.ForceHome(previousHome)

	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveArtifactCacheConfiguredOutput(ctx, defaultCacheProviderRoot(), "127.0.0.1:0", writer, "filesystem", artifactprovider.Limits{}, false)
		_ = writer.Close()
	}()

	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	startup := line
	if !strings.Contains(startup, "url=http://127.0.0.1:") || !strings.Contains(startup, "root="+defaultCacheProviderRoot()) || !strings.Contains(startup, "backend=filesystem") {
		t.Fatalf("human startup = %q", startup)
	}
	info, err := os.Stat(defaultCacheProviderRoot())
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("provider root is not a directory: %s", defaultCacheProviderRoot())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCacheServeJSONReceiptShapeRemainsStable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveArtifactCacheConfiguredOutput(ctx, filepath.Join(t.TempDir(), "provider"), "127.0.0.1:0", writer, "journal", artifactprovider.Limits{}, true)
		_ = writer.Close()
	}()
	var receipt map[string]json.RawMessage
	if err := json.NewDecoder(reader).Decode(&receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt) != 3 || receipt["url"] == nil || receipt["root"] == nil || receipt["listen"] == nil {
		t.Fatalf("JSON startup receipt keys = %v", receipt)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCacheServeCommandPassesProviderLimits(t *testing.T) {
	var got artifactprovider.Limits
	var gotRoot, gotListen, gotBackend string
	var gotJSON bool
	command := newCacheCommand(cacheCommandDependencies{
		serveConfiguredWithOutput: func(_ context.Context, root, listen string, _ io.Writer, backend string, limits artifactprovider.Limits, jsonOutput bool) error {
			gotRoot, gotListen, gotBackend, gotJSON = root, listen, backend, jsonOutput
			got = limits
			return nil
		},
	})
	if err := runCobraCommand(command, []string{"serve", "--root", "provider", "--listen", "127.0.0.1:8080", "--backend", "journal", "--json", "--max-bytes", "10", "--max-objects", "2", "--max-manifests", "3", "--max-uploads", "4", "--requests-per-second", "5"}); err != nil {
		t.Fatal(err)
	}
	if got != (artifactprovider.Limits{MaxBytes: 10, MaxObjects: 2, MaxManifests: 3, MaxUploads: 4, RequestsPerSecond: 5}) {
		t.Fatalf("provider limits = %+v", got)
	}
	if gotRoot != "provider" || gotListen != "127.0.0.1:8080" || gotBackend != "journal" || !gotJSON {
		t.Fatalf("provider overrides = root %q, listen %q, backend %q, json %t", gotRoot, gotListen, gotBackend, gotJSON)
	}
}

func TestCacheServeCommandSelectsJournalBackend(t *testing.T) {
	var gotBackend string
	var gotJSON bool
	command := newCacheCommand(cacheCommandDependencies{
		serveConfiguredWithOutput: func(_ context.Context, _, _ string, _ io.Writer, backend string, _ artifactprovider.Limits, jsonOutput bool) error {
			gotBackend = backend
			gotJSON = jsonOutput
			return nil
		},
	})
	if err := runCobraCommand(command, []string{"serve", "--root", "provider", "--backend", "journal", "--json"}); err != nil {
		t.Fatal(err)
	}
	if gotBackend != "journal" || !gotJSON {
		t.Fatalf("provider backend/json = %q/%t", gotBackend, gotJSON)
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

func TestCacheServePolicyQuotaIsVisibleOverHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	root := filepath.Join(t.TempDir(), "provider")
	go func() {
		done <- serveArtifactCacheWithOptions(ctx, root, "127.0.0.1:0", writer, artifactprovider.Limits{MaxUploads: 1})
		_ = writer.Close()
	}()

	var started cacheServeResult
	if err := json.NewDecoder(reader).Decode(&started); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second}
	put := func(content []byte) (int, error) {
		descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
		request, err := http.NewRequest(http.MethodPut, started.URL+"/v1/objects/sha256/"+descriptor.Digest.Hex(), bytes.NewReader(content))
		if err != nil {
			return 0, err
		}
		request.Header.Set("Content-Type", descriptor.MediaType)
		response, err := client.Do(request)
		if err != nil {
			return 0, err
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return response.StatusCode, nil
	}
	first, err := put([]byte("first quota object"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := put([]byte("second quota object"))
	if err != nil {
		t.Fatal(err)
	}
	if first != http.StatusCreated || second != http.StatusInsufficientStorage {
		t.Fatalf("policy quota statuses = %d/%d, want 201/507", first, second)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCacheServeJournalBackendPersistsAcrossRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "journal-provider")
	content := []byte("journal-shared-provider")
	descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
	start := func() (context.CancelFunc, <-chan error, artifactprovider.Provider) {
		ctx, cancel := context.WithCancel(context.Background())
		reader, writer := io.Pipe()
		done := make(chan error, 1)
		go func() {
			done <- serveArtifactCacheConfigured(ctx, root, "127.0.0.1:0", writer, "journal", artifactprovider.Limits{})
			_ = writer.Close()
		}()
		var started cacheServeResult
		if err := json.NewDecoder(reader).Decode(&started); err != nil {
			t.Fatal(err)
		}
		provider, err := artifactprovider.NewHTTP(started.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		return cancel, done, provider
	}
	cancel, done, provider := start()
	if err := provider.PutObject(context.Background(), artifactprovider.Blob{Descriptor: descriptor, Reader: bytes.NewReader(content)}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	cancel, done, provider = start()
	reader, err := provider.GetObject(context.Background(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("restarted journal content = %q, err=%v", got, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCacheServeJournalBackendOpensExistingJournalRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "legacy-journal-provider")
	content := []byte("legacy-journal-object")
	descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
	legacy, err := artifactprovider.NewJournal(filepath.Join(root, "provider.journal"))
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.PutObject(context.Background(), artifactprovider.Blob{Descriptor: descriptor, Reader: bytes.NewReader(content)}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveArtifactCacheConfigured(ctx, root, "127.0.0.1:0", writer, "journal", artifactprovider.Limits{})
		_ = writer.Close()
	}()
	var started cacheServeResult
	if err := json.NewDecoder(reader).Decode(&started); err != nil {
		t.Fatal(err)
	}
	provider, err := artifactprovider.NewHTTP(started.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	object, err := provider.GetObject(context.Background(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(object)
	_ = object.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("legacy journal content = %q, err=%v", got, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
