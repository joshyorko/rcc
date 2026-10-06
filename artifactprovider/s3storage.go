package artifactprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

// MaxS3StorageObjectBytes is this single-PUT adapter's cap, not an artifact
// format limit. Larger objects require a separately verified multipart adapter.
const MaxS3StorageObjectBytes int64 = 64 << 20

// StorageSigner signs the final request at runtime. Credentials must stay in
// the signer; these options and persistent object metadata contain no secrets.
type StorageSigner interface{ Sign(*http.Request) error }
type StorageSignFunc func(*http.Request) error

func (f StorageSignFunc) Sign(r *http.Request) error { return f(r) }

type S3StorageOptions struct {
	Endpoint              string
	Bucket                string
	Prefix                string
	PathStyle             bool
	AllowInsecureHTTP     bool
	Client                *http.Client
	Signer                StorageSigner
	StagingDirectory      string
	MaxObjectBytes        int64
	ReconciliationTimeout time.Duration
}

type S3Storage struct {
	endpoint              *url.URL
	bucket, prefix, stage string
	pathStyle             bool
	client                *http.Client
	signer                StorageSigner
	maxBytes              int64
	reconcileTimeout      time.Duration
}

var errStorageSigning = errors.New("S3 storage signing failed")

var storageBucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
var storageSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func NewS3Storage(o S3StorageOptions) (*S3Storage, error) {
	u, err := url.Parse(o.Endpoint)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("invalid S3 storage endpoint")
	}
	if u.Scheme == "http" && !o.AllowInsecureHTTP || u.Scheme == "https" && o.AllowInsecureHTTP {
		return nil, errors.New("S3 storage transport policy does not match endpoint")
	}
	if !storageBucketPattern.MatchString(o.Bucket) || strings.Contains(o.Bucket, "..") || strings.Contains(o.Bucket, ".-") || strings.Contains(o.Bucket, "-.") || net.ParseIP(o.Bucket) != nil {
		return nil, errors.New("invalid S3 storage bucket")
	}
	if !o.PathStyle && (net.ParseIP(u.Hostname()) != nil || strings.Contains(o.Bucket, ".")) {
		return nil, errors.New("S3 storage bucket or endpoint requires path-style addressing")
	}
	if o.Prefix != "" {
		if !validStorageName(o.Prefix) {
			return nil, errors.New("invalid S3 storage prefix")
		}
	}
	if o.MaxObjectBytes == 0 {
		o.MaxObjectBytes = MaxS3StorageObjectBytes
	}
	if o.MaxObjectBytes < 1 || o.MaxObjectBytes > MaxS3StorageObjectBytes {
		return nil, errors.New("S3 storage object limit is out of bounds")
	}
	if o.ReconciliationTimeout == 0 {
		o.ReconciliationTimeout = 5 * time.Second
	}
	if o.ReconciliationTimeout < 0 || o.ReconciliationTimeout > 10*time.Second {
		return nil, errors.New("S3 storage reconciliation timeout is out of bounds")
	}
	client := http.Client{Timeout: 30 * time.Second}
	if o.Client != nil {
		client = *o.Client
		if client.Timeout == 0 {
			client.Timeout = 30 * time.Second
		}
	}
	if client.Timeout < 0 || client.Timeout > time.Minute {
		return nil, errors.New("S3 storage HTTP timeout is out of bounds")
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	u.Path = ""
	return &S3Storage{endpoint: u, bucket: o.Bucket, prefix: o.Prefix, pathStyle: o.PathStyle, client: &client, signer: o.Signer, stage: o.StagingDirectory, maxBytes: o.MaxObjectBytes, reconcileTimeout: o.ReconciliationTimeout}, nil
}

func validStorageName(name string) bool {
	if name == "" || len(name) > 1024 {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if !storageSegmentPattern.MatchString(part) || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func validStorageDigest(d environmentartifact.Digest) bool { return len(d.Hex()) == 64 }
func strongStorageRevision(value string) bool {
	if len(value) < 3 || len(value) > 1024 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for _, r := range value[1 : len(value)-1] {
		if r < 0x21 || r > 0x7e || r == '"' {
			return false
		}
	}
	return true
}
func (s *S3Storage) objectKey(d environmentartifact.Digest) (string, error) {
	if !validStorageDigest(d) {
		return "", fmt.Errorf("invalid object digest: %w", ErrStorageIntegrity)
	}
	return "objects/sha256/" + d.Hex(), nil
}
func (s *S3Storage) request(ctx context.Context, method, key string, body io.Reader) (*http.Request, error) {
	if len(key)+len(s.prefix)+1 > 1024 {
		return nil, fmt.Errorf("S3 storage key exceeds bound: %w", ErrStorageIntegrity)
	}
	u := *s.endpoint
	prefix := ""
	if s.prefix != "" {
		prefix = s.prefix + "/"
	}
	if s.pathStyle {
		u.Path = "/" + s.bucket + "/" + prefix + key
	} else {
		u.Host = s.bucket + "." + u.Host
		u.Path = "/" + prefix + key
	}
	r, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errors.New("create S3 storage request")
	}
	r.Header.Set("X-Amz-Content-Sha256", environmentartifact.DigestBytes(nil).Hex())
	return r, nil
}
func (s *S3Storage) do(r *http.Request) (*http.Response, error) {
	if s.signer != nil {
		if err := s.signer.Sign(r); err != nil {
			return nil, errStorageSigning
		}
	}
	resp, err := s.client.Do(r)
	if err != nil {
		if contextErr := r.Context().Err(); contextErr != nil {
			return nil, fmt.Errorf("S3 storage request failed: %w", contextErr)
		}
		return nil, errors.New("S3 storage request failed")
	}
	return resp, nil
}

// No backend response body or signer error is reported: both may contain secrets.
func storageStatus(code int) error {
	switch code {
	case 404:
		return ErrStorageNotFound
	case 409, 412:
		return ErrStorageConflict
	default:
		return fmt.Errorf("S3 storage HTTP status %d", code)
	}
}
func (s *S3Storage) metadata(r *http.Response, d environmentartifact.Digest) (StoredObject, error) {
	revisions := r.Header.Values("ETag")
	digestValues := r.Header.Values("X-Amz-Meta-Sha256")
	lengths := r.Header.Values("Content-Length")
	if len(revisions) != 1 || !strongStorageRevision(revisions[0]) || len(digestValues) != 1 || len(lengths) != 1 {
		return StoredObject{}, fmt.Errorf("invalid S3 object metadata: %w", ErrStorageIntegrity)
	}
	if _, err := environmentartifact.ParseDigest("sha256:" + digestValues[0]); err != nil {
		return StoredObject{}, fmt.Errorf("invalid S3 digest metadata: %w", ErrStorageIntegrity)
	}
	if digestValues[0] != d.Hex() {
		return StoredObject{}, ErrStorageConflict
	}
	size, err := strconv.ParseInt(lengths[0], 10, 64)
	if err != nil || size < 0 || size > s.maxBytes || strconv.FormatInt(size, 10) != lengths[0] {
		return StoredObject{}, fmt.Errorf("invalid S3 object size: %w", ErrStorageIntegrity)
	}
	if r.ContentLength >= 0 && r.ContentLength != size {
		return StoredObject{}, fmt.Errorf("contradictory S3 object size: %w", ErrStorageIntegrity)
	}
	return StoredObject{Digest: d, Size: size, Revision: StorageRevision(revisions[0])}, nil
}
func (s *S3Storage) HeadObject(ctx context.Context, d environmentartifact.Digest) (StoredObject, error) {
	key, err := s.objectKey(d)
	if err != nil {
		return StoredObject{}, err
	}
	r, err := s.request(ctx, http.MethodHead, key, nil)
	if err != nil {
		return StoredObject{}, err
	}
	resp, err := s.do(r)
	if err != nil {
		return StoredObject{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return StoredObject{}, storageStatus(resp.StatusCode)
	}
	return s.metadata(resp, d)
}
func (s *S3Storage) GetObject(ctx context.Context, d environmentartifact.Descriptor) (io.ReadCloser, error) {
	return s.openObject(ctx, d, "")
}

func (s *S3Storage) openObject(ctx context.Context, d environmentartifact.Descriptor, revision StorageRevision) (io.ReadCloser, error) {
	key, err := s.objectKey(d.Digest)
	if err != nil {
		return nil, err
	}
	if d.Size < 0 || d.Size > s.maxBytes {
		return nil, fmt.Errorf("object size out of bounds: %w", ErrStorageIntegrity)
	}
	r, err := s.request(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	if revision != "" {
		r.Header.Set("If-Match", string(revision))
	}
	resp, err := s.do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, storageStatus(resp.StatusCode)
	}
	info, err := s.metadata(resp, d.Digest)
	if err != nil || info.Size != d.Size || (revision != "" && info.Revision != revision) {
		resp.Body.Close()
		return nil, fmt.Errorf("object metadata disagrees with descriptor: %w", ErrStorageIntegrity)
	}
	return &storageVerifiedReader{body: resp.Body, ctx: ctx, hash: sha256.New(), digest: d.Digest.Hex(), size: d.Size}, nil
}

type storageVerifiedReader struct {
	body             io.ReadCloser
	ctx              context.Context
	hash             hash.Hash
	digest           string
	size, read       int64
	verified, closed bool
	failure          error
}

func (r *storageVerifiedReader) Read(b []byte) (int, error) {
	if r.closed {
		return 0, os.ErrClosed
	}
	if r.failure != nil {
		return 0, r.failure
	}
	if r.verified {
		return 0, io.EOF
	}
	if len(b) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		r.failure = err
		return 0, err
	}
	if r.read == r.size {
		var extra [1]byte
		n, err := r.body.Read(extra[:])
		if n != 0 {
			r.failure = fmt.Errorf("object has trailing bytes: %w", ErrStorageIntegrity)
			return 0, r.failure
		}
		if err == io.EOF {
			if hex.EncodeToString(r.hash.Sum(nil)) != r.digest {
				r.failure = fmt.Errorf("object digest mismatch: %w", ErrStorageIntegrity)
				return 0, r.failure
			}
			r.verified = true
			return 0, io.EOF
		}
		if err != nil {
			r.failure = err
		}
		return 0, err
	}
	if remaining := r.size - r.read; int64(len(b)) > remaining {
		b = b[:remaining]
	}
	n, err := r.body.Read(b)
	if n > 0 {
		r.read += int64(n)
		_, _ = r.hash.Write(b[:n])
	}
	if err == io.EOF {
		if r.read != r.size {
			r.failure = fmt.Errorf("object ended before expected size: %w", ErrStorageIntegrity)
			return n, r.failure
		}
		if hex.EncodeToString(r.hash.Sum(nil)) != r.digest {
			r.failure = fmt.Errorf("object digest mismatch: %w", ErrStorageIntegrity)
			return n, r.failure
		}
		r.verified = true
	} else if err != nil {
		r.failure = err
	}
	return n, err
}
func (r *storageVerifiedReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.body.Close()
	if !r.verified {
		return errors.Join(fmt.Errorf("object read did not reach verified EOF: %w", ErrStorageIntegrity), r.failure, err)
	}
	return err
}

func (s *S3Storage) verifyObject(ctx context.Context, d environmentartifact.Descriptor, revision StorageRevision) error {
	r, err := s.openObject(ctx, d, revision)
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, r)
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	// The GET and HEAD revisions must describe one immutable readback, even when
	// the backend was misconfigured to permit object replacement.
	observed, err := s.HeadObject(ctx, d.Digest)
	if err != nil {
		return err
	}
	if observed.Revision != revision {
		return fmt.Errorf("object revision changed during readback: %w", ErrStorageIntegrity)
	}
	return nil
}
func (s *S3Storage) stageBlob(ctx context.Context, b Blob) (file *os.File, err error) {
	if b.Reader == nil || !validStorageDigest(b.Descriptor.Digest) || b.Descriptor.Size < 0 || b.Descriptor.Size > s.maxBytes {
		return nil, fmt.Errorf("invalid object upload: %w", ErrStorageIntegrity)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	file, err = os.CreateTemp(s.stage, ".rcc-object-")
	if err != nil {
		return nil, errors.New("create S3 object staging file")
	}
	staged := file
	okay := false
	defer func() {
		if !okay {
			err = errors.Join(err, staged.Close(), os.Remove(staged.Name()))
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, h), io.LimitReader(&contextReader{ctx: ctx, reader: b.Reader}, b.Descriptor.Size+1))
	if err != nil {
		return nil, err
	}
	if n != b.Descriptor.Size || hex.EncodeToString(h.Sum(nil)) != b.Descriptor.Digest.Hex() {
		return nil, fmt.Errorf("upload size or digest mismatch: %w", ErrStorageIntegrity)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, errors.New("rewind S3 object staging file")
	}
	okay = true
	return file, nil
}
func (s *S3Storage) PutObject(ctx context.Context, b Blob) (resultErr error) {
	f, err := s.stageBlob(ctx, b)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close(), os.Remove(f.Name())) }()
	existing, err := s.HeadObject(ctx, b.Descriptor.Digest)
	if err == nil {
		if existing.Size != b.Descriptor.Size {
			return ErrStorageConflict
		}
		return s.verifyObject(ctx, b.Descriptor, existing.Revision)
	}
	if !errors.Is(err, ErrStorageNotFound) {
		return err
	}
	key, _ := s.objectKey(b.Descriptor.Digest)
	r, err := s.request(ctx, http.MethodPut, key, io.LimitReader(f, b.Descriptor.Size))
	if err != nil {
		return err
	}
	r.ContentLength = b.Descriptor.Size
	if b.Descriptor.Size == 0 {
		r.Body = http.NoBody
	}
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-Amz-Meta-Sha256", b.Descriptor.Digest.Hex())
	r.Header.Set("X-Amz-Content-Sha256", b.Descriptor.Digest.Hex())
	r.Header.Set("If-None-Match", "*")
	if err := ctx.Err(); err != nil {
		return err
	}
	resp, writeErr := s.do(r)
	code := 0
	if resp != nil {
		code = resp.StatusCode
		_ = resp.Body.Close()
	}
	if errors.Is(writeErr, errStorageSigning) {
		return writeErr
	}
	uncertain := writeErr != nil || code >= 500 || ctx.Err() != nil
	if writeErr == nil && code != 200 && code != 201 && code != 204 && code != 409 && code != 412 && !uncertain {
		return storageStatus(code)
	}
	observeCtx := ctx
	cancel := func() {}
	if uncertain {
		observeCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), s.reconcileTimeout)
	}
	defer cancel()
	observed, observeErr := s.HeadObject(observeCtx, b.Descriptor.Digest)
	if observeErr == nil && observed.Size == b.Descriptor.Size {
		observeErr = s.verifyObject(observeCtx, b.Descriptor, observed.Revision)
	}
	if observeErr == nil && observed.Size == b.Descriptor.Size {
		return nil
	}
	if uncertain {
		return errors.Join(ErrStorageAmbiguous, writeErr, observeErr)
	}
	if code == 409 || code == 412 {
		return ErrStorageConflict
	}
	return errors.Join(ErrStorageAmbiguous, observeErr)
}

var _ ObjectStorage = (*S3Storage)(nil)
