package artifactprovider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/joshyorko/rcc/environmentartifact"
)

func newHTTPProviderTestServer(t *testing.T) (*Filesystem, *HTTP, *httptest.Server) {
	t.Helper()
	filesystem, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(filesystem))
	t.Cleanup(server.Close)
	client, err := NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return filesystem, client, server
}

func TestHTTPProviderMatchesFilesystemProviderSemantics(t *testing.T) {
	_, client, _ := newHTTPProviderTestServer(t)
	ctx := context.Background()
	capabilities, err := client.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities.SchemaVersions) != 1 || capabilities.SchemaVersions[0] != 1 {
		t.Fatalf("capabilities = %+v", capabilities)
	}

	fixture := newProviderFixture(t)
	descriptors := make([]environmentartifact.Descriptor, 0, len(fixture.blobs))
	for _, blob := range fixture.blobs {
		descriptors = append(descriptors, blob.Descriptor)
	}
	missing, err := client.MissingObjects(ctx, descriptors)
	if err != nil || len(missing) != len(descriptors) {
		t.Fatalf("initial missing = %v, %v", missing, err)
	}
	for _, blob := range fixture.blobs {
		content, err := io.ReadAll(blob.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.PutObject(ctx, Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(content)}); err != nil {
			t.Fatal(err)
		}
	}
	missing, err = client.MissingObjects(ctx, descriptors)
	if err != nil || len(missing) != 0 {
		t.Fatalf("uploaded missing = %v, %v", missing, err)
	}

	reader, err := client.GetObject(ctx, descriptors[0])
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || environmentartifact.DigestBytes(got) != descriptors[0].Digest {
		t.Fatalf("HTTP object failed verification: %v", err)
	}
	if err := client.CommitManifest(ctx, fixture.manifestBytes); err != nil {
		t.Fatal(err)
	}
	resolved, err := client.ResolveManifest(ctx, fixture.manifest.ArtifactDigest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resolved, fixture.manifestBytes) {
		t.Fatal("HTTP-resolved manifest differs from committed bytes")
	}
}

func TestHTTPCommitKeepsIncompleteManifestInvisible(t *testing.T) {
	_, client, _ := newHTTPProviderTestServer(t)
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs[:len(fixture.blobs)-1] {
		content, err := io.ReadAll(blob.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(content)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.CommitManifest(context.Background(), fixture.manifestBytes); err == nil {
		t.Fatal("HTTP committed incomplete manifest")
	}
	if _, err := client.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err == nil {
		t.Fatal("incomplete HTTP manifest became visible")
	}
}

func TestHTTPHandlerRejectsInvalidMethodPathAndContent(t *testing.T) {
	_, _, server := newHTTPProviderTestServer(t)
	digest := environmentartifact.DigestBytes([]byte("body"))
	cases := []struct {
		name, method, path, contentType string
		body                            string
		contentLength                   int64
	}{
		{name: "wrong method", method: http.MethodPost, path: "/v1/capabilities", contentLength: 0},
		{name: "GET body", method: http.MethodGet, path: "/v1/capabilities", contentType: "application/json", body: `{}`, contentLength: 2},
		{name: "trailing path", method: http.MethodGet, path: "/v1/objects/sha256/" + digest.Hex() + "/extra", contentLength: 0},
		{name: "encoded separator", method: http.MethodGet, path: "/v1/objects/sha256%2f" + digest.Hex(), contentLength: 0},
		{name: "missing content type", method: http.MethodPut, path: "/v1/objects/sha256/" + digest.Hex(), body: "body", contentLength: 4},
		{name: "wrong declared size", method: http.MethodPut, path: "/v1/objects/sha256/" + digest.Hex(), contentType: "application/octet-stream", body: "body", contentLength: 3},
		{name: "oversized JSON", method: http.MethodPost, path: "/v1/objects/missing", contentType: "application/json", body: strings.Repeat("x", maxProviderJSONBytes+1), contentLength: maxProviderJSONBytes + 1},
		{name: "duplicate JSON key", method: http.MethodPost, path: "/v1/objects/missing", contentType: "application/json", body: `{"descriptors":[],"descriptors":[]}`, contentLength: 35},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, server.URL+test.path, strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			request.ContentLength = test.contentLength
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				return
			}
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				t.Fatalf("invalid request succeeded with %s", response.Status)
			}
		})
	}
}

func TestHTTPClientRejectsWrongJSONContentTypeAndDuplicateKeys(t *testing.T) {
	for name, response := range map[string][2]string{
		"wrong content type": {"text/plain", `{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`},
		"duplicate key":      {"application/json", `{"schemaVersions":[1],"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", response[0])
				_, _ = writer.Write([]byte(response[1]))
			}))
			defer server.Close()
			client, err := NewHTTP(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Capabilities(context.Background()); err == nil {
				t.Fatal("HTTP client accepted a non-strict JSON response")
			}
		})
	}
}

func TestHTTPClientRejectsCorruptObjectAndManifestResponses(t *testing.T) {
	digest := environmentartifact.DigestBytes([]byte("expected"))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write([]byte("corrupt!"))
	}))
	defer server.Close()
	client, err := NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: digest, Size: 8}
	reader, err := client.GetObject(context.Background(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("HTTP client accepted corrupt object response")
	}
	_ = reader.Close()
	if _, err := client.ResolveManifest(context.Background(), digest); err == nil {
		t.Fatal("HTTP client accepted corrupt manifest response")
	}
}

func TestHTTPPutObjectPreservesExactDescriptorSemantics(t *testing.T) {
	_, client, _ := newHTTPProviderTestServer(t)
	valid := testBlob([]byte("body"))
	for name, reader := range map[string]io.Reader{
		"short body":   strings.NewReader("bod"),
		"long body":    strings.NewReader("body-extra"),
		"wrong digest": strings.NewReader("copy"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := client.PutObject(context.Background(), Blob{Descriptor: valid.Descriptor, Reader: reader}); err == nil {
				t.Fatal("HTTP upload accepted bytes outside the descriptor")
			}
		})
	}
}

func TestHTTPConcurrentIdenticalManifestCommitIsIdempotent(t *testing.T) {
	_, client, _ := newHTTPProviderTestServer(t)
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		content, err := io.ReadAll(blob.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(content)}); err != nil {
			t.Fatal(err)
		}
	}
	const workers = 12
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			errors <- client.CommitManifest(context.Background(), fixture.manifestBytes)
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent HTTP commit: %v", err)
		}
	}
	resolved, err := client.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest)
	if err != nil || !bytes.Equal(resolved, fixture.manifestBytes) {
		t.Fatalf("concurrently committed manifest = %q, %v", resolved, err)
	}
}
