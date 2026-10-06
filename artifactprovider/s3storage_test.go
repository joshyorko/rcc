package artifactprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type storageFixtureObject struct {
	body             []byte
	digest, revision string
}
type storageFixture struct {
	mu                 sync.Mutex
	objects            map[string]storageFixtureObject
	puts, gets, heads  int
	serial             int
	forcePut, forceGet int
	contentRevision    bool
	corruptGet         bool
	omitRevision       bool
	weakRevision       bool
	lastPut            http.Header
	lastPutLength      int64
	lastPutTransfer    []string
}

func newStorageFixture() *storageFixture {
	return &storageFixture{objects: make(map[string]storageFixtureObject)}
}
func (f *storageFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.URL.Path
	obj, exists := f.objects[key]
	switch r.Method {
	case http.MethodPut:
		f.puts++
		f.lastPut = r.Header.Clone()
		f.lastPutLength = r.ContentLength
		f.lastPutTransfer = append([]string(nil), r.TransferEncoding...)
		if f.forcePut != 0 {
			w.WriteHeader(f.forcePut)
			return
		}
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(412)
			return
		}
		if match := r.Header.Get("If-Match"); match != "" && (!exists || obj.revision != match) {
			w.WriteHeader(412)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		f.serial++
		obj = storageFixtureObject{body: body, digest: r.Header.Get("X-Amz-Meta-Sha256"), revision: fmt.Sprintf(`"rev-%d"`, f.serial)}
		if f.contentRevision {
			obj.revision = `"` + storageDigest(body) + `"`
		}
		f.objects[key] = obj
		w.Header().Set("ETag", obj.revision)
		w.WriteHeader(200)
	case http.MethodHead, http.MethodGet:
		if r.Method == http.MethodGet {
			f.gets++
			if f.forceGet != 0 {
				w.WriteHeader(f.forceGet)
				return
			}
		} else {
			f.heads++
		}
		if !exists {
			w.WriteHeader(404)
			return
		}
		if !f.omitRevision {
			rev := obj.revision
			if f.weakRevision {
				rev = "W/" + rev
			}
			w.Header().Set("ETag", rev)
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(obj.body)))
		w.Header().Set("X-Amz-Meta-Sha256", obj.digest)
		w.WriteHeader(200)
		if r.Method == http.MethodGet {
			body := obj.body
			if f.corruptGet {
				body = bytes.Repeat([]byte("x"), len(body))
			}
			_, _ = w.Write(body)
		}
	default:
		w.WriteHeader(405)
	}
}
func storageTestBlob(body []byte) Blob {
	return Blob{Descriptor: environmentartifact.Descriptor{Digest: environmentartifact.DigestBytes(body), Size: int64(len(body)), MediaType: "application/octet-stream"}, Reader: bytes.NewReader(body)}
}
func fixtureStorage(t *testing.T, f *storageFixture, change func(*S3StorageOptions)) (*S3Storage, string) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	options := S3StorageOptions{Endpoint: srv.URL, Bucket: "rcc-fixture", Prefix: "test", PathStyle: true, AllowInsecureHTTP: true, Client: srv.Client(), StagingDirectory: t.TempDir(), MaxObjectBytes: 1024, ReconciliationTimeout: 100 * time.Millisecond}
	if change != nil {
		change(&options)
	}
	s, err := NewS3Storage(options)
	if err != nil {
		t.Fatal(err)
	}
	return s, options.StagingDirectory
}
func objectFixturePath(d environmentartifact.Digest) string {
	return "/rcc-fixture/test/objects/sha256/" + d.Hex()
}

func TestS3StorageImmutablePublication(t *testing.T) {
	f := newStorageFixture()
	s, dir := fixtureStorage(t, f, nil)
	body := []byte("immutable RCC bytes")
	blob := storageTestBlob(body)
	if err := s.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	if err := s.PutObject(context.Background(), storageTestBlob(body)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	puts := f.puts
	condition := f.lastPut.Get("If-None-Match")
	f.mu.Unlock()
	if puts != 1 || condition != "*" {
		t.Fatalf("puts=%d condition=%q", puts, condition)
	}
	info, err := s.HeadObject(context.Background(), blob.Descriptor.Digest)
	if err != nil || info.Size != int64(len(body)) || info.Revision == "" {
		t.Fatalf("head=%+v err=%v", info, err)
	}
	r, err := s.GetObject(context.Background(), blob.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	closeErr := r.Close()
	if err != nil || closeErr != nil || !bytes.Equal(got, body) {
		t.Fatalf("get=%q err=%v close=%v", got, err, closeErr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging leaked: %v %v", entries, err)
	}
}
func TestS3StorageConflictingImmutableBytes(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	body := []byte("expected")
	blob := storageTestBlob(body)
	f.objects[objectFixturePath(blob.Descriptor.Digest)] = storageFixtureObject{body: []byte("different"), digest: environmentartifact.DigestBytes([]byte("different")).Hex(), revision: `"existing"`}
	if err := s.PutObject(context.Background(), blob); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("err=%v", err)
	}
	if f.puts != 0 {
		t.Fatal("conflicting existing object was overwritten")
	}
}
func TestS3StorageRejectsCorruptReadback(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	body := []byte("expected")
	blob := storageTestBlob(body)
	f.objects[objectFixturePath(blob.Descriptor.Digest)] = storageFixtureObject{body: bytes.Repeat([]byte("x"), len(body)), digest: blob.Descriptor.Digest.Hex(), revision: `"existing"`}
	if err := s.PutObject(context.Background(), blob); !errors.Is(err, ErrStorageIntegrity) {
		t.Fatalf("err=%v", err)
	}
	r, err := s.GetObject(context.Background(), blob.Descriptor)
	if err == nil {
		_, err = io.ReadAll(r)
		_ = r.Close()
	}
	if !errors.Is(err, ErrStorageIntegrity) {
		t.Fatalf("corrupt get err=%v", err)
	}
}
func TestS3StorageRejectsMalformedAndOversizeBlobs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Blob)
	}{
		{"empty digest", func(b *Blob) { b.Descriptor.Digest = environmentartifact.Digest{} }},
		{"negative size", func(b *Blob) { b.Descriptor.Size = -1 }},
		{"oversize", func(b *Blob) { b.Descriptor.Size = 1025 }},
		{"nil reader", func(b *Blob) { b.Reader = nil }},
		{"wrong digest", func(b *Blob) { b.Descriptor.Digest = environmentartifact.DigestBytes([]byte("wrong")) }},
		{"short source", func(b *Blob) { b.Descriptor.Size++ }},
		{"long source", func(b *Blob) { b.Descriptor.Size-- }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStorageFixture()
			s, dir := fixtureStorage(t, f, nil)
			blob := storageTestBlob([]byte("payload"))
			tc.change(&blob)
			if err := s.PutObject(context.Background(), blob); err == nil {
				t.Fatal("invalid upload succeeded")
			}
			if f.puts != 0 {
				t.Fatal("invalid bytes transmitted")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("stage leaked")
			}
		})
	}
}
func TestS3StorageMissingAndMalformedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*storageFixture)
	}{
		{"missing", func(*storageFixture) {}},
		{"missing revision", func(f *storageFixture) { f.omitRevision = true }},
		{"weak revision", func(f *storageFixture) { f.weakRevision = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStorageFixture()
			s, _ := fixtureStorage(t, f, nil)
			d := environmentartifact.DigestBytes([]byte("a"))
			if tc.name != "missing" {
				f.objects[objectFixturePath(d)] = storageFixtureObject{body: []byte("a"), digest: d.Hex(), revision: `"r"`}
			}
			tc.change(f)
			_, err := s.HeadObject(context.Background(), d)
			if tc.name == "missing" {
				if !errors.Is(err, ErrStorageNotFound) {
					t.Fatalf("err=%v", err)
				}
			} else if !errors.Is(err, ErrStorageIntegrity) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func TestS3StorageEarlyCloseRejectsUnverifiedRead(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	blob := storageTestBlob([]byte("payload"))
	if err := s.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetObject(context.Background(), blob.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	if _, err := r.Read(buffer); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); !errors.Is(err, ErrStorageIntegrity) {
		t.Fatalf("early close err=%v", err)
	}
}
func TestS3StorageCancellationCleansStage(t *testing.T) {
	f := newStorageFixture()
	s, dir := fixtureStorage(t, f, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.PutObject(ctx, storageTestBlob([]byte("payload"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if f.puts != 0 {
		t.Fatal("cancelled upload reached PUT")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("stage leaked")
	}
}
func TestS3StorageOptionsRejectUnsafeLocations(t *testing.T) {
	good := S3StorageOptions{Endpoint: "https://storage.example", Bucket: "rcc-test", PathStyle: true}
	for _, tc := range []struct {
		name   string
		change func(*S3StorageOptions)
	}{
		{"HTTP requires opt in", func(o *S3StorageOptions) { o.Endpoint = "http://storage.example" }},
		{"endpoint credentials", func(o *S3StorageOptions) { o.Endpoint = "https://user:secret@storage.example" }},
		{"endpoint path", func(o *S3StorageOptions) { o.Endpoint += "/objects" }},
		{"endpoint query", func(o *S3StorageOptions) { o.Endpoint += "?token=secret" }},
		{"endpoint fragment", func(o *S3StorageOptions) { o.Endpoint += "#frag" }},
		{"invalid bucket", func(o *S3StorageOptions) { o.Bucket = "../escape" }},
		{"IP shaped bucket", func(o *S3StorageOptions) { o.Bucket = "127.0.0.1" }},
		{"bad prefix", func(o *S3StorageOptions) { o.Prefix = "a/../b" }},
		{"encoded prefix", func(o *S3StorageOptions) { o.Prefix = "a/%2f/b" }},
		{"oversize cap", func(o *S3StorageOptions) { o.MaxObjectBytes = 64<<20 + 1 }},
		{"unbounded reconciliation", func(o *S3StorageOptions) { o.ReconciliationTimeout = 11 * time.Second }},
		{"dotted virtual host", func(o *S3StorageOptions) { o.Bucket = "rcc.test"; o.PathStyle = false }},
		{"IP virtual host", func(o *S3StorageOptions) { o.Endpoint = "https://127.0.0.1"; o.PathStyle = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := good
			tc.change(&o)
			if _, err := NewS3Storage(o); err == nil {
				t.Fatal("unsafe options accepted")
			}
		})
	}
}
func TestS3StorageSignerAndRedirectBoundary(t *testing.T) {
	destinationHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationHits++; w.WriteHeader(200) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "fixture-signature" {
			t.Error("signer was not applied")
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	s, err := NewS3Storage(S3StorageOptions{Endpoint: origin.URL, Bucket: "rcc-fixture", PathStyle: true, AllowInsecureHTTP: true, Signer: StorageSignFunc(func(r *http.Request) error { r.Header.Set("Authorization", "fixture-signature"); return nil })})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.HeadObject(context.Background(), environmentartifact.DigestBytes(nil)); err == nil {
		t.Fatal("redirect accepted")
	}
	if destinationHits != 0 {
		t.Fatal("authorization followed redirect")
	}
}

type storageRoundTripFunc func(*http.Request) (*http.Response, error)

func (f storageRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func storageDigest(body []byte) string                                           { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func TestS3StorageReadRejectsTrailingAndTruncatedBytes(t *testing.T) {
	for _, body := range []string{"payload-extra", "pay"} {
		t.Run(body, func(t *testing.T) {
			expected := []byte("payload")
			s, err := NewS3Storage(S3StorageOptions{Endpoint: "https://storage.example", Bucket: "rcc-fixture", PathStyle: true, Client: &http.Client{Transport: storageRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Etag": []string{`"r"`}, "X-Amz-Meta-Sha256": []string{storageDigest(expected)}, "Content-Length": []string{"7"}}, ContentLength: 7, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.GetObject(context.Background(), storageTestBlob(expected).Descriptor)
			if err == nil {
				_, err = io.ReadAll(r)
				_ = r.Close()
			}
			if !errors.Is(err, ErrStorageIntegrity) {
				t.Fatalf("body=%q err=%v", body, err)
			}
		})
	}
}

func TestS3StorageNamedHeadExpectedParentAndABA(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	ctx := context.Background()
	a := environmentartifact.DigestBytes([]byte("A"))
	b := environmentartifact.DigestBytes([]byte("B"))
	first, err := s.CompareAndSwapHead(ctx, "main", nil, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompareAndSwapHead(ctx, "main", nil, b); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("create conflict=%v", err)
	}
	second, err := s.CompareAndSwapHead(ctx, "main", &first, b)
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.CompareAndSwapHead(ctx, "main", &second, a)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == third.Revision {
		t.Fatal("ABA recreated old publication revision")
	}
	if _, err := s.CompareAndSwapHead(ctx, "main", &first, b); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("stale ABA parent err=%v", err)
	}
	wrong := third
	wrong.Digest = b
	if _, err := s.CompareAndSwapHead(ctx, "main", &wrong, b); !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("wrong parent digest err=%v", err)
	}
	got, err := s.ResolveHead(ctx, "main")
	if err != nil || got != third {
		t.Fatalf("resolved=%+v err=%v", got, err)
	}
}
func TestS3StorageNamedHeadConcurrentCAS(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	ctx := context.Background()
	first, err := s.CompareAndSwapHead(ctx, "main", nil, environmentartifact.DigestBytes([]byte("seed")))
	if err != nil {
		t.Fatal(err)
	}
	errorsCh := make(chan error, 2)
	start := make(chan struct{})
	for i := range 2 {
		go func() {
			<-start
			_, err := s.CompareAndSwapHead(ctx, "main", &first, environmentartifact.DigestBytes([]byte(fmt.Sprint(i))))
			errorsCh <- err
		}()
	}
	close(start)
	wins, conflicts := 0, 0
	for range 2 {
		err := <-errorsCh
		if err == nil {
			wins++
		} else if errors.Is(err, ErrStorageConflict) {
			conflicts++
		} else {
			t.Fatalf("CAS err=%v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}
func TestS3StorageConcurrentCreate(t *testing.T) {
	for _, heads := range []bool{false, true} {
		t.Run(fmt.Sprint(heads), func(t *testing.T) {
			f := newStorageFixture()
			s, _ := fixtureStorage(t, f, nil)
			ctx := context.Background()
			start := make(chan struct{})
			done := make(chan error, 8)
			for range 8 {
				go func() {
					<-start
					if heads {
						_, err := s.CompareAndSwapHead(ctx, "main", nil, environmentartifact.DigestBytes([]byte("same")))
						done <- err
					} else {
						done <- s.PutObject(ctx, storageTestBlob([]byte("same")))
					}
				}()
			}
			close(start)
			wins, conflicts := 0, 0
			for range 8 {
				err := <-done
				if err == nil {
					wins++
				} else if errors.Is(err, ErrStorageConflict) {
					conflicts++
				} else {
					t.Fatalf("err=%v", err)
				}
			}
			if heads && (wins != 1 || conflicts != 7) {
				t.Fatalf("head wins=%d conflicts=%d", wins, conflicts)
			}
			if !heads && wins != 8 {
				t.Fatalf("immutable wins=%d", wins)
			}
		})
	}
}
func TestS3StorageHeadRejectsMalformedPathsAndParents(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	d := environmentartifact.DigestBytes([]byte("a"))
	for _, name := range []string{"", "..", "a/../b", "/main", "a//b", "a\\b", "a/%2f", "a\x00b", strings.Repeat("a", 129)} {
		if _, err := s.CompareAndSwapHead(context.Background(), name, nil, d); err == nil {
			t.Fatalf("name %q accepted", name)
		}
	}
	for _, parent := range []*NamedHead{{Name: "other", Digest: d, Revision: `"r"`}, {Name: "main", Digest: d}, {Name: "main", Digest: d, Revision: `W/"r"`}, {Name: "main", Digest: environmentartifact.Digest{}, Revision: `"r"`}} {
		if _, err := s.CompareAndSwapHead(context.Background(), "main", parent, d); err == nil {
			t.Fatalf("invalid parent %+v accepted", parent)
		}
	}
	if f.puts != 0 {
		t.Fatal("invalid head transmitted")
	}
	if _, err := s.ResolveHead(context.Background(), "missing"); !errors.Is(err, ErrStorageNotFound) {
		t.Fatalf("missing head err=%v", err)
	}
}
func TestS3StorageHeadRejectsMalformedReadback(t *testing.T) {
	for _, body := range []string{`{}`, `{"schemaVersion":1,"name":"main","digest":"sha256:` + strings.Repeat("0", 64) + `","publicationToken":"` + strings.Repeat("a", 32) + `","extra":true}`, strings.Repeat("x", 4097)} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			f := newStorageFixture()
			s, _ := fixtureStorage(t, f, nil)
			f.objects["/rcc-fixture/test/heads/main"] = storageFixtureObject{body: []byte(body), digest: storageDigest([]byte(body)), revision: `"r"`}
			if _, err := s.ResolveHead(context.Background(), "main"); !errors.Is(err, ErrStorageIntegrity) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func TestS3StorageLostWriteResponseReconcilesOnce(t *testing.T) {
	for _, head := range []bool{false, true} {
		for _, mode := range []string{"committed", "not committed", "unreadable", "corrupt", "cancelled committed"} {
			t.Run(fmt.Sprint(head)+"/"+mode, func(t *testing.T) {
				f := newStorageFixture()
				s, _ := fixtureStorage(t, f, func(o *S3StorageOptions) {
					base := o.Client.Transport
					if base == nil {
						base = http.DefaultTransport
					}
					o.Client = &http.Client{Transport: storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						if r.Method != http.MethodPut {
							return base.RoundTrip(r)
						}
						if mode == "not committed" {
							return nil, errors.New("synthetic lost response")
						}
						resp, err := base.RoundTrip(r)
						if err != nil {
							return nil, err
						}
						resp.Body.Close()
						if mode == "unreadable" {
							f.mu.Lock()
							f.forceGet = 503
							f.mu.Unlock()
						}
						if mode == "corrupt" {
							f.mu.Lock()
							f.corruptGet = true
							f.mu.Unlock()
						}
						return nil, errors.New("synthetic lost response")
					})}
				})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if mode == "cancelled committed" {
					base := s.client.Transport
					s.client.Transport = storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						resp, err := base.RoundTrip(r)
						if r.Method == http.MethodPut {
							cancel()
						}
						return resp, err
					})
				}
				var err error
				if head {
					_, err = s.CompareAndSwapHead(ctx, "main", nil, environmentartifact.DigestBytes([]byte("next")))
				} else {
					err = s.PutObject(ctx, storageTestBlob([]byte("payload")))
				}
				if mode == "committed" || mode == "cancelled committed" {
					if err != nil {
						t.Fatalf("verified commit err=%v", err)
					}
				} else if !errors.Is(err, ErrStorageAmbiguous) {
					t.Fatalf("unknown outcome err=%v", err)
				}
				f.mu.Lock()
				puts := f.puts
				f.mu.Unlock()
				if puts > 1 {
					t.Fatalf("mutation retried %d times", puts)
				}
			})
		}
	}
}
func TestS3StorageConditional409And412NeverRetry(t *testing.T) {
	for _, code := range []int{409, 412} {
		for _, head := range []bool{false, true} {
			t.Run(fmt.Sprint(code, head), func(t *testing.T) {
				f := newStorageFixture()
				f.forcePut = code
				s, _ := fixtureStorage(t, f, nil)
				var err error
				if head {
					_, err = s.CompareAndSwapHead(context.Background(), "main", nil, environmentartifact.DigestBytes([]byte("a")))
				} else {
					err = s.PutObject(context.Background(), storageTestBlob([]byte("a")))
				}
				if !errors.Is(err, ErrStorageConflict) || errors.Is(err, ErrStorageAmbiguous) {
					t.Fatalf("err=%v", err)
				}
				if f.puts != 1 {
					t.Fatalf("puts=%d", f.puts)
				}
			})
		}
	}
}
func TestS3StorageReconciliationDeadline(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, func(o *S3StorageOptions) {
		base := o.Client.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		afterPut := false
		o.ReconciliationTimeout = 20 * time.Millisecond
		o.Client = &http.Client{Transport: storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPut {
				afterPut = true
				return nil, errors.New("lost response")
			}
			if afterPut {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return base.RoundTrip(r)
		})}
	})
	started := time.Now()
	err := s.PutObject(context.Background(), storageTestBlob([]byte("a")))
	if !errors.Is(err, ErrStorageAmbiguous) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("reconciliation unbounded: %v", elapsed)
	}
}

func TestS3StorageRejectsChangedGETRevision(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, func(o *S3StorageOptions) {
		base := o.Client.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		o.Client = &http.Client{Transport: storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := base.RoundTrip(r)
			if err == nil && r.Method == http.MethodGet {
				resp.Header.Set("ETag", `"different-revision"`)
			}
			return resp, err
		})}
	})
	blob := storageTestBlob([]byte("payload"))
	f.objects[objectFixturePath(blob.Descriptor.Digest)] = storageFixtureObject{body: []byte("payload"), digest: blob.Descriptor.Digest.Hex(), revision: `"head-revision"`}
	if err := s.PutObject(context.Background(), blob); !errors.Is(err, ErrStorageIntegrity) {
		t.Fatalf("GET revision change accepted: %v", err)
	}
}
func TestS3StorageSignerFailureDoesNotReconcile(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, func(o *S3StorageOptions) {
		o.Signer = StorageSignFunc(func(r *http.Request) error {
			if r.Method == http.MethodPut {
				return errors.New("secret signer failure")
			}
			return nil
		})
	})
	err := s.PutObject(context.Background(), storageTestBlob([]byte("payload")))
	if err == nil || errors.Is(err, ErrStorageAmbiguous) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("signer error=%v", err)
	}
	if f.puts != 0 || f.heads != 1 {
		t.Fatalf("puts=%d heads=%d", f.puts, f.heads)
	}
	_, err = s.CompareAndSwapHead(context.Background(), "main", nil, environmentartifact.DigestBytes([]byte("payload")))
	if err == nil || errors.Is(err, ErrStorageAmbiguous) {
		t.Fatalf("head signer error=%v", err)
	}
	if f.gets != 0 {
		t.Fatalf("signer failure triggered GET: %d", f.gets)
	}
}
func TestS3StorageHeadRejectsUnchangedRevision(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	ctx := context.Background()
	first, err := s.CompareAndSwapHead(ctx, "main", nil, environmentartifact.DigestBytes([]byte("first")))
	if err != nil {
		t.Fatal(err)
	}
	base := s.client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	s.client.Transport = storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if err == nil && r.Method == http.MethodGet {
			resp.Header.Set("ETag", string(first.Revision))
		}
		return resp, err
	})
	if _, err := s.CompareAndSwapHead(ctx, "main", &first, environmentartifact.DigestBytes([]byte("next"))); !errors.Is(err, ErrStorageAmbiguous) {
		t.Fatalf("unchanged revision accepted: %v", err)
	}
}
func TestS3StorageBodyReadErrorClosesAndCleansStage(t *testing.T) {
	f := newStorageFixture()
	s, dir := fixtureStorage(t, f, nil)
	blob := storageTestBlob([]byte("payload"))
	blob.Reader = storageFailReader{}
	if err := s.PutObject(context.Background(), blob); err == nil {
		t.Fatal("failed source accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 || f.puts != 0 {
		t.Fatalf("stage=%v puts=%d", entries, f.puts)
	}
}

type storageFailReader struct{}

func (storageFailReader) Read([]byte) (int, error) { return 0, errors.New("source read failed") }

func TestS3Storage5xxWriteReconciliation(t *testing.T) {
	for _, head := range []bool{false, true} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprint(head, committed), func(t *testing.T) {
				f := newStorageFixture()
				s, _ := fixtureStorage(t, f, func(o *S3StorageOptions) {
					base := o.Client.Transport
					if base == nil {
						base = http.DefaultTransport
					}
					o.Client = &http.Client{Transport: storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						if r.Method != http.MethodPut {
							return base.RoundTrip(r)
						}
						if !committed {
							return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("secret backend diagnostic"))}, nil
						}
						resp, err := base.RoundTrip(r)
						if err == nil {
							resp.StatusCode = 503
						}
						return resp, err
					})}
				})
				var err error
				if head {
					_, err = s.CompareAndSwapHead(context.Background(), "main", nil, environmentartifact.DigestBytes([]byte("a")))
				} else {
					err = s.PutObject(context.Background(), storageTestBlob([]byte("a")))
				}
				if committed {
					if err != nil {
						t.Fatalf("committed 5xx err=%v", err)
					}
				} else if !errors.Is(err, ErrStorageAmbiguous) {
					t.Fatalf("uncommitted 5xx err=%v", err)
				}
				if err != nil && strings.Contains(err.Error(), "secret") {
					t.Fatal("backend diagnostic leaked")
				}
				if f.puts > 1 {
					t.Fatal("PUT retried")
				}
			})
		}
	}
}
func TestS3StorageHeadCancellationAndReconciliationBound(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.CompareAndSwapHead(ctx, "main", nil, environmentartifact.DigestBytes([]byte("a"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	if f.puts != 0 {
		t.Fatal("cancelled head wrote")
	}
	base := s.client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	afterPut := false
	s.reconcileTimeout = 20 * time.Millisecond
	s.client.Transport = storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPut {
			afterPut = true
			return nil, errors.New("lost response")
		}
		if afterPut {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return base.RoundTrip(r)
	})
	start := time.Now()
	if _, err := s.CompareAndSwapHead(context.Background(), "main", nil, environmentartifact.DigestBytes([]byte("a"))); !errors.Is(err, ErrStorageAmbiguous) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("head reconciliation unbounded")
	}
}
func TestS3StorageMalformedResponseHeaders(t *testing.T) {
	body := []byte("payload")
	blob := storageTestBlob(body)
	for _, tc := range []struct {
		name   string
		change func(http.Header)
	}{
		{"duplicate ETag", func(h http.Header) { h.Add("ETag", `"other"`) }},
		{"unquoted ETag", func(h http.Header) { h.Set("ETag", "rev") }},
		{"duplicate digest", func(h http.Header) { h.Add("X-Amz-Meta-Sha256", blob.Descriptor.Digest.Hex()) }},
		{"missing digest", func(h http.Header) { h.Del("X-Amz-Meta-Sha256") }},
		{"bad digest", func(h http.Header) { h.Set("X-Amz-Meta-Sha256", "garbage") }},
		{"negative length", func(h http.Header) { h.Set("Content-Length", "-1") }},
		{"oversize length", func(h http.Header) { h.Set("Content-Length", "67108865") }},
		{"noncanonical length", func(h http.Header) { h.Set("Content-Length", "07") }},
		{"duplicate length", func(h http.Header) { h.Add("Content-Length", "7") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewS3Storage(S3StorageOptions{Endpoint: "https://storage.example", Bucket: "rcc-fixture", PathStyle: true, Client: &http.Client{Transport: storageRoundTripFunc(func(*http.Request) (*http.Response, error) {
				h := http.Header{"Etag": []string{`"r"`}, "X-Amz-Meta-Sha256": []string{blob.Descriptor.Digest.Hex()}, "Content-Length": []string{"7"}}
				tc.change(h)
				return &http.Response{StatusCode: 200, Header: h, ContentLength: -1, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetObject(context.Background(), blob.Descriptor); !errors.Is(err, ErrStorageIntegrity) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func TestS3StorageEmptyObjectAndZeroLengthRead(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	blob := storageTestBlob(nil)
	if err := s.PutObject(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetObject(context.Background(), blob.Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Fatalf("zero read=%d,%v", n, err)
	}
	if got, err := io.ReadAll(r); err != nil || len(got) != 0 {
		t.Fatalf("empty get=%v,%v", got, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed read=%v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second close=%v", err)
	}
}

func TestS3StorageSignerReceivesExactPayloadHash(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, func(o *S3StorageOptions) {
		o.Signer = StorageSignFunc(func(r *http.Request) error {
			want := storageDigest(nil)
			if r.Method == http.MethodPut {
				want = r.Header.Get("X-Amz-Meta-Sha256")
			}
			if r.Header.Get("X-Amz-Content-Sha256") != want {
				t.Errorf("signer payload hash=%q want=%q", r.Header.Get("X-Amz-Content-Sha256"), want)
			}
			return nil
		})
	})
	if err := s.PutObject(context.Background(), storageTestBlob([]byte("payload"))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompareAndSwapHead(context.Background(), "main", nil, environmentartifact.DigestBytes([]byte("payload"))); err != nil {
		t.Fatal(err)
	}
}
func TestS3StorageRejectsOversizeCombinedKey(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	part := strings.Repeat("a", 128)
	name := strings.Join([]string{part, part, part, part, part, part, part, part}, "/")
	if _, err := s.CompareAndSwapHead(context.Background(), name, nil, environmentartifact.DigestBytes(nil)); err == nil {
		t.Fatal("oversize complete S3 key accepted")
	}
	if f.puts != 0 {
		t.Fatal("oversize key transmitted")
	}
}

func TestS3StorageSuccessfulHeadSupersededBeforeReadbackIsAmbiguous(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, func(o *S3StorageOptions) {
		base := o.Client.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		o.Client = &http.Client{Transport: storageRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := base.RoundTrip(r)
			if err == nil && r.Method == http.MethodPut {
				f.mu.Lock()
				obj := f.objects[r.URL.Path]
				var doc storageHeadDocument
				if err := json.Unmarshal(obj.body, &doc); err != nil {
					f.mu.Unlock()
					t.Error(err)
					return resp, nil
				}
				doc.Digest = environmentartifact.DigestBytes([]byte("newer winner"))
				doc.PublicationToken = strings.Repeat("f", 32)
				obj.body, _ = json.Marshal(doc)
				obj.digest = storageDigest(obj.body)
				obj.revision = `"newer-revision"`
				f.objects[r.URL.Path] = obj
				f.mu.Unlock()
			}
			return resp, err
		})}
	})
	if _, err := s.CompareAndSwapHead(context.Background(), "main", nil, environmentartifact.DigestBytes([]byte("proposed"))); !errors.Is(err, ErrStorageAmbiguous) {
		t.Fatalf("superseded successful write err=%v", err)
	}
	if f.puts != 1 {
		t.Fatal("superseded write retried")
	}
	head, err := s.ResolveHead(context.Background(), "main")
	if err != nil || head.Digest != environmentartifact.DigestBytes([]byte("newer winner")) {
		t.Fatalf("newer head=%+v err=%v", head, err)
	}
}
func TestS3StorageConcurrentABARejectsOriginalRevision(t *testing.T) {
	f := newStorageFixture()
	f.contentRevision = true
	s, _ := fixtureStorage(t, f, nil)
	ctx := context.Background()
	a := environmentartifact.DigestBytes([]byte("A"))
	b := environmentartifact.DigestBytes([]byte("B"))
	first, err := s.CompareAndSwapHead(ctx, "main", nil, a)
	if err != nil {
		t.Fatal(err)
	}
	transitioned := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		next, err := s.CompareAndSwapHead(ctx, "main", &first, b)
		if err == nil {
			last, e := s.CompareAndSwapHead(ctx, "main", &next, a)
			err = e
			if err == nil && last.Revision == first.Revision {
				err = errors.New("ABA recreated content-derived revision")
			}
		}
		transitioned <- err
	}()
	go func() {
		if err := <-transitioned; err != nil {
			done <- err
			return
		}
		_, err := s.CompareAndSwapHead(ctx, "main", &first, b)
		done <- err
	}()
	if err := <-done; !errors.Is(err, ErrStorageConflict) {
		t.Fatalf("stale concurrent ABA err=%v", err)
	}
}

func TestS3StorageEmptyObjectUsesExplicitZeroLength(t *testing.T) {
	f := newStorageFixture()
	s, _ := fixtureStorage(t, f, nil)
	if err := s.PutObject(context.Background(), storageTestBlob(nil)); err != nil {
		t.Fatal(err)
	}
	if f.lastPutLength != 0 || len(f.lastPutTransfer) != 0 {
		t.Fatalf("empty PUT length=%d transfer=%v", f.lastPutLength, f.lastPutTransfer)
	}
}
