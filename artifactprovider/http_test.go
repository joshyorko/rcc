package artifactprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestHTTPMultiGiByteStreamingAcceptance(t *testing.T) {
	if os.Getenv("RCC_REAL_LARGE_STREAM") != "1" {
		t.Skip("set RCC_REAL_LARGE_STREAM=1 for the opt-in 2 GiB HTTP streaming gate")
	}
	const defaultSize = int64(2 << 30)
	size := defaultSize
	if raw := os.Getenv("RCC_LARGE_STREAM_BYTES"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < defaultSize {
			t.Fatalf("RCC_LARGE_STREAM_BYTES must be at least %d: %q", defaultSize, raw)
		}
		size = parsed
	}
	pattern := bytes.Repeat([]byte("rcc-large-stream\n"), 2048)
	hash := sha256.New()
	for remaining := size; remaining > 0; {
		chunk := pattern
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		_, _ = hash.Write(chunk)
		remaining -= int64(len(chunk))
	}
	digest, err := environmentartifact.ParseDigest("sha256:" + hex.EncodeToString(hash.Sum(nil)))
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		remaining := size
		for remaining > 0 {
			chunk := pattern
			if int64(len(chunk)) > remaining {
				chunk = chunk[:remaining]
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			remaining -= int64(len(chunk))
		}
	}))
	defer server.Close()
	client, err := NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: digest, Size: size}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	reader, err := client.GetObject(context.Background(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	const transferBufferBytes = 64 * 1024
	bytesRead, err := io.CopyBuffer(io.Discard, reader, make([]byte, transferBufferBytes))
	closeErr := reader.Close()
	runtime.ReadMemStats(&after)
	if err != nil || closeErr != nil {
		t.Fatalf("stream err=%v close=%v", err, closeErr)
	}
	if bytesRead != size {
		t.Fatalf("bytes=%d want=%d", bytesRead, size)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want=1", requests.Load())
	}
	receiptPath := os.Getenv("RCC_LARGE_STREAM_RECEIPT")
	if receiptPath == "" {
		receiptPath = "tmp/large-stream-receipt.json"
	}
	receipt := map[string]any{"schemaVersion": 1, "sha256": digest.Hex(), "platform": runtime.GOOS + "/" + runtime.GOARCH, "bytes": bytesRead, "memoryBytes": after.Alloc, "memoryDeltaBytes": int64(after.Alloc) - int64(before.Alloc), "bufferBytes": transferBufferBytes, "requests": requests.Load(), "restartPolicy": "full-restart", "interruptionAcceptance": "TestHTTPInterruptedDownloadFailsVerificationThenFullRestartSucceeds"}
	if sourceSHA := os.Getenv("RCC_SOURCE_SHA"); sourceSHA != "" {
		receipt["commitSha"] = sourceSHA
	}
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPOptionsConfigureUserAgentAndTimeout(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`))
	}))
	defer server.Close()
	client, err := NewHTTPWithOptions(server.URL, HTTPOptions{UserAgent: "rcc/test", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Capabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "rcc/test" {
		t.Fatalf("user-agent = %q", got)
	}
}

func TestHTTPOptionsLoadCustomCAFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "ca-*.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString("not a certificate"); err != nil {
		t.Fatal(err)
	}
	_, err = NewHTTPWithOptions("https://example.test", HTTPOptions{CAFile: file.Name()})
	if err == nil || !strings.Contains(err.Error(), "CA") {
		t.Fatalf("expected CA error, got %v", err)
	}
}

func TestHTTPOptionsUseProxyNoProxyAndCustomCAForRealRequests(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer target.Close()
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		response, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		for key, values := range response.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer proxy.Close()
	client, err := NewHTTPWithOptions(target.URL, HTTPOptions{ProxyURL: proxy.URL, NoProxy: target.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if proxied.Load() != 0 {
		t.Fatalf("no-proxy request went through proxy: %d", proxied.Load())
	}
	client, err = NewHTTPWithOptions(target.URL, HTTPOptions{ProxyURL: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if proxied.Load() != 1 {
		t.Fatalf("proxied request count=%d, want 1", proxied.Load())
	}

	tlsTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer tlsTarget.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsTarget.Certificate().Raw})
	tlsClient, err := NewHTTPWithOptions(tlsTarget.URL, HTTPOptions{CAPEM: ca})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tlsClient.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPHealthAndCapabilitiesContract(t *testing.T) {
	filesystem, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(filesystem))
	defer server.Close()
	client, err := NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	caps, err := client.Protocol(context.Background())
	if err != nil || caps.Protocol != "rcc.artifact.v1" {
		t.Fatalf("caps=%+v err=%v", caps, err)
	}
	if caps.SelectedVersion != 1 || caps.TransferOutcome != "full-restart-only" || caps.Immutability != "content-addressed" || len(caps.Extensions) == 0 {
		t.Fatalf("negotiated diagnostics=%+v", caps)
	}
	if negotiated, err := client.ProtocolWithOptions(context.Background(), []int{1}, []string{"rcc.artifact.v1/backup", "rcc.artifact.v2/unknown"}); err != nil || negotiated.SelectedVersion != 1 || len(negotiated.Extensions) != 1 {
		t.Fatalf("negotiation=%+v err=%v", negotiated, err)
	}
	if negotiated, err := client.ProtocolWithOptions(context.Background(), []int{2}, []string{"rcc.artifact.v2/compat"}); err != nil || negotiated.SelectedVersion != 2 {
		t.Fatalf("v2 negotiation=%+v err=%v", negotiated, err)
	}
	if _, err := client.ProtocolWithOptions(context.Background(), []int{2}, nil); err == nil {
		t.Fatal("v2 negotiation without compatibility extension was accepted")
	}
	health, err := client.Health(context.Background())
	if err != nil || !health.Ready || health.Storage != "ok" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	if _, err := client.NegotiateCapabilities(context.Background(), Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}, SafeRestart: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.NegotiateCapabilities(context.Background(), Capabilities{RangeSupport: true}); err == nil {
		t.Fatal("accepted unavailable range capability")
	}
}

type slowCapabilityProvider struct{ *Filesystem }

func (p slowCapabilityProvider) Capabilities(ctx context.Context) (Capabilities, error) {
	<-ctx.Done()
	return Capabilities{}, ctx.Err()
}
func TestHTTPHandlerDeadlineStopsSlowProvider(t *testing.T) {
	p, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithOptions(slowCapabilityProvider{p}, HandlerOptions{RequestTimeout: 10 * time.Millisecond})
	req := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	rec := httptest.NewRecorder()
	started := time.Now()
	handler.ServeHTTP(rec, req)
	if time.Since(started) > time.Second {
		t.Fatal("handler exceeded deadline")
	}
	if rec.Code < 400 {
		t.Fatalf("slow provider status=%d", rec.Code)
	}
}

func TestHTTPClientNetworkBodyDeadlineStopsSlowloris(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`))
	}))
	defer server.Close()
	client, err := NewHTTPWithOptions(server.URL, HTTPOptions{Client: server.Client(), Timeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Capabilities(context.Background()); err == nil {
		t.Fatal("slow network response exceeded timeout")
	}
}

func TestHTTPServerBodyDeadlineCleansSlowloris(t *testing.T) {
	p, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandlerWithOptions(p, HandlerOptions{RequestTimeout: 20 * time.Millisecond}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(conn, "POST /v1/admin/restore HTTP/1.1\r\nHost: %s\r\nContent-Length: 100\r\nConnection: close\r\n\r\nx", u.Host)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	response, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(response) == 0 || !bytes.Contains(response, []byte("400")) && !bytes.Contains(response, []byte("422")) {
		t.Fatalf("slowloris response=%q", response)
	}
}

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

func TestHTTPInterruptedUploadIsInvisibleUntilFullRestart(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(provider))
	defer server.Close()
	client, err := NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("restartable upload")
	descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
	if err := client.PutObject(context.Background(), Blob{Descriptor: descriptor, Reader: bytes.NewReader(content[:len(content)-1])}); err == nil {
		t.Fatal("short upload unexpectedly succeeded")
	}
	if _, err := provider.GetObject(context.Background(), descriptor); err == nil {
		t.Fatal("short upload became visible")
	}
	if err := client.PutObject(context.Background(), Blob{Descriptor: descriptor, Reader: bytes.NewReader(content)}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPPolicyQuotaFailureIsExplicit(t *testing.T) {
	filesystem, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(NewPolicy(filesystem, Limits{MaxBytes: 2})))
	defer server.Close()
	content := []byte("too large")
	descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: environmentartifact.DigestBytes(content), Size: int64(len(content))}
	request, err := http.NewRequest(http.MethodPut, server.URL+"/v1/objects/sha256/"+descriptor.Digest.Hex(), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", descriptor.MediaType)
	request.ContentLength = descriptor.Size
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("quota status=%d, want %d", response.StatusCode, http.StatusInsufficientStorage)
	}
}

func TestHTTPInterruptedDownloadFailsVerificationThenFullRestartSucceeds(t *testing.T) {
	content := []byte("restartable download")
	digest := environmentartifact.DigestBytes(content)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if requests.Add(1) == 1 {
			_, _ = w.Write(content[:len(content)-1])
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()
	client, err := NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	descriptor := environmentartifact.Descriptor{MediaType: "application/octet-stream", Digest: digest, Size: int64(len(content))}
	reader, err := client.GetObject(context.Background(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("truncated download unexpectedly verified")
	}
	_ = reader.Close()
	reader, err = client.GetObject(context.Background(), descriptor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("full restart err=%v content=%q", err, got)
	}
}

func TestHTTPRangeRequestIsRejectedForFullRestartOnlyPolicy(t *testing.T) {
	provider, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("full restart only")
	descriptor := environmentartifact.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    environmentartifact.DigestBytes(content),
		Size:      int64(len(content)),
	}
	if err := provider.PutObject(context.Background(), Blob{Descriptor: descriptor, Reader: bytes.NewReader(content)}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(provider))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/objects/sha256/"+descriptor.Digest.Hex(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=4-")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("range status = %d, want %d", response.StatusCode, http.StatusRequestedRangeNotSatisfiable)
	}
	if got := response.Header.Get("Accept-Ranges"); got != "none" {
		t.Fatalf("Accept-Ranges = %q, want none", got)
	}
	if body, err := io.ReadAll(response.Body); err != nil || !bytes.Contains(body, []byte("restart the full object")) {
		t.Fatalf("range response body = %q, err=%v", body, err)
	}
}

func TestHTTPProviderStoresAndServesDetachedTrustAttachments(t *testing.T) {
	_, _, server := newHTTPProviderTestServer(t)
	artifact := "sha256:carrier"
	data := []byte(`{"mediaType":"application/vnd.rcc.environment.provenance.v1+json","artifactDigest":"sha256:carrier"}`)
	carrier := &artifacttrust.HTTPCarrier{BaseURL: server.URL, Client: server.Client()}
	if err := artifacttrust.PutAttachment(carrier, artifact, "provenance", data); err != nil {
		t.Fatal(err)
	}
	got, err := artifacttrust.GetAttachment(carrier, artifact, "provenance")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("trust attachment=%q err=%v", got, err)
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
