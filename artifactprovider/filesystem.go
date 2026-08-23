package artifactprovider

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (it *contextReader) Read(target []byte) (int, error) {
	if err := it.ctx.Err(); err != nil {
		return 0, err
	}
	return it.reader.Read(target)
}

const maxManifestBytes = 16 << 20
const filesystemRestoreMarker = ".restore-state"
const filesystemAuditFile = ".audit"

type filesystemRestoreState struct {
	Created []string `json:"created"`
}

var filesystemRestorePublishHook func(string) error

type Filesystem struct {
	root         string
	commitMu     sync.Mutex
	gcActive     atomic.Bool
	repairActive atomic.Bool
	requests     atomic.Int64
	errors       atomic.Int64
	corruptions  atomic.Int64
}

// Cleanup removes only private interrupted-upload files. Immutable objects and
// manifests are never selected, so restart cleanup cannot delete published data.
func (it *Filesystem) Cleanup(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	removed := 0
	err := filepath.Walk(it.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		name := info.Name()
		if len(name) > 0 && (strings.HasPrefix(name, ".upload-") || strings.HasPrefix(name, ".manifest-")) {
			if err := os.Remove(path); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

func NewFilesystem(root string) (*Filesystem, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve provider root: %w", err)
	}
	if absolute == string(filepath.Separator) {
		return nil, fmt.Errorf("provider root cannot be the filesystem root")
	}
	provider := &Filesystem{root: absolute}
	if err := provider.initialize(); err != nil {
		return nil, err
	}
	if err := provider.recoverRestore(); err != nil {
		return nil, err
	}
	return provider, nil
}

func (it *Filesystem) recoverRestore() error {
	b, err := os.ReadFile(filepath.Join(it.root, filesystemRestoreMarker))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state filesystemRestoreState
	if err := json.Unmarshal(b, &state); err != nil {
		return fmt.Errorf("invalid restore marker: %w", err)
	}
	for _, rel := range state.Created {
		if rel == "" || filepath.IsAbs(rel) || rel == ".." || filepath.Clean(rel) != rel || strings.Contains(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid restore marker path")
		}
		if err := os.Remove(filepath.Join(it.root, rel)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(filepath.Join(it.root, filesystemRestoreMarker)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncFilesystemDir(it.root)
}

func syncFilesystemDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (it *Filesystem) Audit(ctx context.Context) ([]AuditRecord, error) {
	f, err := os.Open(filepath.Join(it.root, filesystemAuditFile))
	if os.IsNotExist(err) {
		return []AuditRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []AuditRecord{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), maxManifestBytes)
	for s.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var record AuditRecord
		if err := json.Unmarshal(s.Bytes(), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, s.Err()
}

func (it *Filesystem) appendAudit(action string, digest environmentartifact.Digest) error {
	record, _ := json.Marshal(AuditRecord{At: time.Now(), Action: action, Actor: "rcc", Provider: "artifactprovider/filesystem", Reference: it.root, Digest: digest.Hex()})
	f, err := os.OpenFile(filepath.Join(it.root, filesystemAuditFile), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(record, '\n')); err == nil {
		err = f.Sync()
	}
	_ = f.Close()
	return err
}

func (it *Filesystem) objectPath(digest environmentartifact.Digest) string {
	hex := digest.Hex()
	if len(hex) != 64 {
		return ""
	}
	return filepath.Join(it.root, "objects", "sha256", hex[:2], hex[2:4], hex)
}

func (it *Filesystem) manifestPath(digest environmentartifact.Digest) string {
	hex := digest.Hex()
	if len(hex) != 64 {
		return ""
	}
	return filepath.Join(it.root, "manifests", "sha256", hex[:2], hex[2:4], hex)
}

func (it *Filesystem) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}, MaxObjectBytes: maxProviderObjectBytes, MaxManifestBytes: maxManifestBytes, MaxRequestBytes: maxProviderJSONBytes, RangeSupport: false, ResumeSupport: false, SafeRestart: true}, nil
}

func (it *Filesystem) Health(ctx context.Context) (Health, error) {
	it.requests.Add(1)
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return Health{Ready: false, Error: err.Error(), LatencyMS: time.Since(started).Milliseconds(), Process: "local", Audit: "append-only"}, err
	}
	if _, err := os.Stat(it.root); err != nil {
		return Health{Storage: "unavailable", Ready: false, Error: "storage unavailable", LatencyMS: time.Since(started).Milliseconds(), Process: "local", Audit: "append-only"}, fmt.Errorf("%w: storage: %v", ErrNotReady, err)
	}
	gc := "idle"
	if it.gcActive.Load() {
		gc = "running"
	}
	if it.repairActive.Load() {
		gc = "repairing"
	}
	return Health{Ready: true, Storage: "ok", Capability: "ok", Auth: "not-applicable", Quota: "ok", GC: gc, LatencyMS: time.Since(started).Milliseconds(), Process: "local", Audit: "append-only", Requests: it.requests.Load(), Errors: it.errors.Load(), Corruptions: it.corruptions.Load()}, nil
}

func (it *Filesystem) GetObjectByDigest(ctx context.Context, digest environmentartifact.Digest) (io.ReadCloser, int64, error) {
	it.requests.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if len(digest.Hex()) != 64 {
		return nil, 0, fmt.Errorf("invalid object digest")
	}
	path := it.objectPath(digest)
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 || !linkInfo.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("invalid provider object type")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	if info.Size() < 0 || info.Size() > maxProviderObjectBytes {
		_ = file.Close()
		return nil, 0, fmt.Errorf("invalid provider object size")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, 0, fmt.Errorf("invalid provider object type")
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest.Hex() {
		_ = file.Close()
		return nil, 0, fmt.Errorf("stored object digest mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, 0, err
	}
	return file, info.Size(), nil
}

func (it *Filesystem) MissingObjects(ctx context.Context, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	it.requests.Add(1)
	if len(descriptors) > maxProviderDescriptorFanout {
		return nil, fmt.Errorf("descriptor fanout exceeds limit")
	}
	missing := make([]environmentartifact.Digest, 0)
	for _, descriptor := range descriptors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		found, err := it.hasObject(descriptor)
		if err != nil {
			return nil, err
		}
		if !found {
			missing = append(missing, descriptor.Digest)
		}
	}
	sort.Slice(missing, func(left, right int) bool { return missing[left].String() < missing[right].String() })
	return missing, nil
}
