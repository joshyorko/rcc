package artifactprovider

import (
	"archive/tar"
	"bytes"
	"context"
	"github.com/joshyorko/rcc/environmentartifact"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type failingReader struct {
	data []byte
	cut  int
	sent int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.sent >= r.cut {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, r.data[r.sent:minInt(r.cut, r.sent+len(p))])
	r.sent += n
	return n, nil
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestFilesystemRestorePreflightsConflictsBeforePublishing(t *testing.T) {
	p, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	conflict := testBlob([]byte("old"))
	putFixtureBlob(t, p, conflict)
	newBlob := testBlob([]byte("new"))
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, item := range []struct {
		name string
		data []byte
	}{{"objects/sha256/" + newBlob.Descriptor.Digest.Hex(), []byte("new")}, {"objects/sha256/" + conflict.Descriptor.Digest.Hex(), []byte("different")}} {
		if err := tw.WriteHeader(&tar.Header{Name: item.name, Typeflag: tar.TypeReg, Size: int64(len(item.data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(item.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Restore(context.Background(), &archive); err == nil {
		t.Fatal("restore accepted immutable conflict")
	}
	if _, err := p.GetObject(context.Background(), environmentartifact.Descriptor{Digest: newBlob.Descriptor.Digest, Size: newBlob.Descriptor.Size}); err == nil {
		t.Fatal("restore published staged object before conflict validation")
	}
}

func TestProviderContractFilesystemAndJournal(t *testing.T) {
	providers := []struct {
		name string
		new  func() (Provider, error)
	}{
		{"filesystem", func() (Provider, error) { p, e := NewFilesystem(t.TempDir()); return p, e }},
		{"journal", func() (Provider, error) { p, e := NewJournal(t.TempDir() + "/provider.log"); return p, e }},
	}
	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			p, e := tc.new()
			if e != nil {
				t.Fatal(e)
			}
			f := newProviderFixture(t)
			for _, b := range f.blobs {
				raw, e := io.ReadAll(b.Reader)
				if e != nil {
					t.Fatal(e)
				}
				if e = p.PutObject(context.Background(), Blob{Descriptor: b.Descriptor, Reader: bytes.NewReader(raw)}); e != nil {
					t.Fatal(e)
				}
			}
			if e = p.CommitManifest(context.Background(), f.manifestBytes); e != nil {
				t.Fatal(e)
			}
			got, e := p.ResolveManifest(context.Background(), f.manifest.ArtifactDigest)
			if e != nil || !bytes.Equal(got, f.manifestBytes) {
				t.Fatalf("manifest resolve err=%v", e)
			}
		})
	}
}

func TestHTTPAdminContractFilesystemAndJournal(t *testing.T) {
	providers := []struct {
		name string
		new  func() (Provider, error)
	}{
		{"filesystem", func() (Provider, error) { return NewFilesystem(t.TempDir()) }},
		{"journal", func() (Provider, error) { return NewJournal(t.TempDir() + "/provider.log") }},
	}
	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := tc.new()
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(NewHandler(provider))
			defer server.Close()
			for _, path := range []string{"/v1/health", "/v1/capabilities", "/v1/admin/objects", "/v1/admin/manifests", "/v1/admin/audit", "/v1/admin/repair"} {
				method := http.MethodGet
				if strings.HasPrefix(path, "/v1/admin/") && path != "/v1/admin/objects" && path != "/v1/admin/manifests" && path != "/v1/admin/audit" {
					method = http.MethodPost
				}
				req, _ := http.NewRequest(method, server.URL+path, nil)
				resp, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					t.Fatalf("%s status=%d", path, resp.StatusCode)
				}
			}
		})
	}
}

func TestProviderBackupRestoreRestartGCParity(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func(string) (Provider, error)
	}{
		{"filesystem", func(root string) (Provider, error) { return NewFilesystem(root) }},
		{"journal", func(root string) (Provider, error) { return NewJournal(filepath.Join(root, "provider.log")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			source, err := tc.new(root)
			if err != nil {
				t.Fatal(err)
			}
			fixture := newProviderFixture(t)
			for _, blob := range fixture.blobs {
				raw, _ := io.ReadAll(blob.Reader)
				if err := source.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(raw)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := source.CommitManifest(context.Background(), fixture.manifestBytes); err != nil {
				t.Fatal(err)
			}
			var archive bytes.Buffer
			if err := source.(ProviderV1Backup).Backup(context.Background(), &archive); err != nil {
				t.Fatal(err)
			}
			targetRoot := t.TempDir()
			restored, err := tc.new(targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			if err := restored.(ProviderV1Backup).Restore(context.Background(), bytes.NewReader(archive.Bytes())); err != nil {
				t.Fatal(err)
			}
			if _, err := restored.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err != nil {
				t.Fatal(err)
			}
			orphan := testBlob([]byte("orphan parity object"))
			if err := restored.PutObject(context.Background(), orphan); err != nil {
				t.Fatal(err)
			}
			if _, err := restored.(ProviderV1Admin).GarbageCollect(context.Background(), Retention{MaxAge: -1}); err != nil {
				t.Fatal(err)
			}
			if _, err := restored.GetObject(context.Background(), orphan.Descriptor); err == nil {
				t.Fatal("GC retained unreferenced object")
			}
			restarted, err := tc.new(targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPJournalProviderFullParity(t *testing.T) {
	first, err := NewJournal(t.TempDir() + "/first.log")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(first))
	defer server.Close()
	client, err := NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	fixture := newProviderFixture(t)
	for _, blob := range fixture.blobs {
		raw, _ := io.ReadAll(blob.Reader)
		if err := client.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(raw)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.CommitManifest(context.Background(), fixture.manifestBytes); err != nil {
		t.Fatal(err)
	}
	backup, err := server.Client().Get(server.URL + "/v1/admin/backup")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(backup.Body)
	backup.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewJournal(t.TempDir() + "/second.log")
	if err != nil {
		t.Fatal(err)
	}
	restoreServer := httptest.NewServer(NewHandler(second))
	defer restoreServer.Close()
	req, _ := http.NewRequest(http.MethodPost, restoreServer.URL+"/v1/admin/restore", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/x-tar")
	req.ContentLength = int64(len(payload))
	resp, err := restoreServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("restore status=%d", resp.StatusCode)
	}
	client2, err := NewHTTP(restoreServer.URL, restoreServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client2.ResolveManifest(context.Background(), fixture.manifest.ArtifactDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	var audit []AuditRecord
	if err := client2.doJSON(context.Background(), http.MethodGet, "/v1/admin/audit", nil, &audit); err != nil || len(audit) == 0 {
		t.Fatalf("audit=%v err=%v", audit, err)
	}
}

func TestInterruptedUploadsLeaveNoProvisionalFilesAndCanRestart(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func(string) (Provider, error)
	}{{"filesystem", func(root string) (Provider, error) { return NewFilesystem(root) }}, {"journal", func(root string) (Provider, error) { return NewJournal(root + "/provider.log") }}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			p, e := tc.new(root)
			if e != nil {
				t.Fatal(e)
			}
			content := bytes.Repeat([]byte("x"), 1024)
			blob := testBlob(content)
			if e = p.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: &failingReader{data: content, cut: 100}}); e == nil {
				t.Fatal("interrupted upload accepted")
			}
			if e = p.PutObject(context.Background(), Blob{Descriptor: blob.Descriptor, Reader: bytes.NewReader(content)}); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestHTTPProxyAndNoProxyRealRequests(t *testing.T) {
	var proxyHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`))
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}`))
	}))
	defer proxy.Close()
	via, e := NewHTTPWithOptions(target.URL, HTTPOptions{Client: target.Client(), ProxyURL: proxy.URL})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = via.Capabilities(context.Background()); e != nil {
		t.Fatal(e)
	}
	if proxyHits.Load() != 1 {
		t.Fatalf("proxy hits=%d", proxyHits.Load())
	}
	direct, e := NewHTTPWithOptions(target.URL, HTTPOptions{Client: target.Client(), ProxyURL: proxy.URL, NoProxy: "127.0.0.1"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = direct.Capabilities(context.Background()); e != nil {
		t.Fatal(e)
	}
	if proxyHits.Load() != 1 {
		t.Fatalf("no-proxy request used proxy: hits=%d", proxyHits.Load())
	}
}

func TestProviderNoProxyMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, host, port, rules string
		want                    bool
	}{
		{"cidr", "10.2.3.4", "443", "10.0.0.0/8", true}, {"cidr miss", "192.0.2.4", "443", "10.0.0.0/8", false},
		{"ipv6", "2001:db8::4", "443", "2001:db8::/32", true}, {"domain suffix", "api.example.test", "443", ".example.test", true},
		{"domain miss", "example.test.evil", "443", ".example.test", false}, {"port precedence", "127.0.0.1", "8443", "127.0.0.1:443,127.0.0.1:8443", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := "https://" + tc.host + ":" + tc.port
			if strings.Contains(tc.host, ":") {
				raw = "https://[" + tc.host + "]:" + tc.port
			}
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := providerNoProxyMatch(u, tc.rules); got != tc.want {
				t.Fatalf("match=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestHTTPAbuseBoundaries(t *testing.T) {
	p, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(p)
	for _, tc := range []struct{ name, method, path, body string }{{"encoded traversal", http.MethodGet, "/v1/objects/sha256/%2e%2e/secret", ""}, {"oversized missing request", http.MethodPost, "/v1/objects/missing", strings.Repeat("x", maxProviderJSONBytes+1)}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code < 400 || rec.Code >= 500 {
				t.Fatalf("abuse request status=%d", rec.Code)
			}
		})
	}
}

func TestHTTPRejectsChunkAmbiguityAndMediaTypeInjection(t *testing.T) {
	p, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(p)
	digest := environmentartifact.DigestBytes([]byte("body"))
	for _, tc := range []struct {
		name, contentType string
		length            int64
		body              string
	}{
		{"chunked upload", "application/octet-stream", -1, "body"}, {"media type parameters", "application/octet-stream; x=y", 4, "body"}, {"header injection", "application/octet-stream\r\nX-Injected: yes", 4, "body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/v1/objects/sha256/"+digest.Hex(), strings.NewReader(tc.body))
			req.ContentLength = tc.length
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code >= 200 && rec.Code < 300 {
				t.Fatalf("abuse request succeeded: %d", rec.Code)
			}
		})
	}
}
