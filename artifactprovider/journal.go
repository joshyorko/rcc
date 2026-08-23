package artifactprovider

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/joshyorko/rcc/environmentartifact"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Journal stores bytes in separate files and keeps only bounded metadata in memory.
type Journal struct {
	path, objectDir string
	mu              sync.RWMutex
	adminMu         sync.Mutex
	restoring       bool
	objects         map[string]objectRef
	manifests       map[string][]byte
	manifestTimes   map[string]time.Time
	gcActive        atomic.Bool
	repairActive    atomic.Bool
	requests        atomic.Int64
	errors          atomic.Int64
	corruptions     atomic.Int64
}
type objectRef struct {
	path      string
	size      int64
	mediaType string
}
type journalRecord struct {
	Kind, Digest, MediaType, Content string
	Size                             int64
	At                               int64
	Txn                              string `json:"txn,omitempty"`
	Actor                            string `json:"actor,omitempty"`
	Provider                         string `json:"provider,omitempty"`
	Reference                        string `json:"reference,omitempty"`
}

var journalAppendHook func(journalRecord) error

func (j *Journal) applyJournalRecord(r journalRecord) error {
	switch r.Kind {
	case "manifest":
		b, e := base64.StdEncoding.DecodeString(r.Content)
		if e != nil {
			return e
		}
		j.manifests[r.Digest] = b
		if r.At > 0 {
			j.manifestTimes[r.Digest] = time.Unix(0, r.At)
		}
	case "delete-manifest":
		delete(j.manifests, r.Digest)
		delete(j.manifestTimes, r.Digest)
	case "delete-object":
		delete(j.objects, r.Digest)
		_ = os.Remove(filepath.Join(j.objectDir, r.Digest))
	case "object":
		p := filepath.Join(j.objectDir, r.Digest)
		if r.Content != "" {
			b, e := base64.StdEncoding.DecodeString(r.Content)
			if e != nil {
				return e
			}
			if e = os.WriteFile(p, b, 0600); e != nil {
				return e
			}
			r.Size = int64(len(b))
		}
		if r.Size >= 0 && r.Size <= maxProviderObjectBytes {
			if st, e := os.Stat(p); e == nil && st.Size() == r.Size {
				if e := verifyJournalFile(p, r.Digest, r.Size); e == nil {
					j.objects[r.Digest] = objectRef{p, r.Size, r.MediaType}
				}
			}
		}
	case "policy":
		b, e := base64.StdEncoding.DecodeString(r.Content)
		if e != nil {
			return e
		}
		tmp := j.path + ".policy.replay"
		if e = os.WriteFile(tmp, b, 0600); e != nil {
			return e
		}
		if e = os.Rename(tmp, j.path+".policy"); e != nil {
			return e
		}
	}
	return nil
}

func verifyJournalFile(path, digest string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.CopyN(h, f, size)
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return fmt.Errorf("journal object digest mismatch")
	}
	return nil
}

func readBoundedJournalIndex(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxManifestBytes {
		return nil, fmt.Errorf("object index exceeds bounded size")
	}
	return b, nil
}

func NewJournal(path string) (*Journal, error) {
	j := &Journal{path: path, objectDir: path + ".objects", objects: map[string]objectRef{}, manifests: map[string][]byte{}, manifestTimes: map[string]time.Time{}}
	if err := os.MkdirAll(j.objectDir, 0700); err != nil {
		return nil, err
	}
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return j, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), int(maxManifestBytes+4096))
	var malformed error
	pending := map[string][]journalRecord{}
	for s.Scan() {
		var r journalRecord
		if e := json.Unmarshal(s.Bytes(), &r); e != nil {
			malformed = e
			break
		}
		if r.Kind == "restore-begin" {
			pending[r.Txn] = nil
			continue
		}
		if r.Kind == "restore-commit" {
			for _, item := range pending[r.Txn] {
				if e := j.applyJournalRecord(item); e != nil {
					return nil, e
				}
			}
			delete(pending, r.Txn)
			continue
		}
		if r.Txn != "" {
			pending[r.Txn] = append(pending[r.Txn], r)
			continue
		}
		if e := j.applyJournalRecord(r); e != nil {
			return nil, e
		}
	}
	if e := s.Err(); e != nil {
		return nil, e
	}
	if malformed != nil {
		data, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		if len(data) == 0 || data[len(data)-1] == '\n' {
			return nil, fmt.Errorf("corrupt journal record: %w", malformed)
		}
	}
	if ents, e := os.ReadDir(j.objectDir); e == nil {
		for _, ent := range ents {
			if len(ent.Name()) == 64 {
				if _, ok := j.objects[ent.Name()]; !ok {
					_ = os.Remove(filepath.Join(j.objectDir, ent.Name()))
				}
			}
		}
	}
	return j, nil
}
func (j *Journal) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}, MaxObjectBytes: maxProviderObjectBytes, MaxManifestBytes: maxManifestBytes, MaxRequestBytes: maxProviderJSONBytes, SafeRestart: true}, nil
}
func (j *Journal) Health(ctx context.Context) (Health, error) {
	j.requests.Add(1)
	started := time.Now()
	if e := ctx.Err(); e != nil {
		return Health{Ready: false, Error: e.Error(), LatencyMS: time.Since(started).Milliseconds(), Process: "local", Audit: "append-only"}, e
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	var n int64
	for _, r := range j.objects {
		n += r.size
	}
	gc := "idle"
	if j.gcActive.Load() {
		gc = "running"
	}
	if j.repairActive.Load() {
		gc = "repairing"
	}
	return Health{Ready: true, Storage: "ok", Capability: "ok", Auth: "not-applicable", Quota: "ok", GC: gc, Objects: int64(len(j.objects)), Manifests: int64(len(j.manifests)), Bytes: n, LatencyMS: time.Since(started).Milliseconds(), Process: "local", Audit: "append-only", Requests: j.requests.Load(), Errors: j.errors.Load(), Corruptions: j.corruptions.Load()}, nil
}
func (j *Journal) MissingObjects(ctx context.Context, ds []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	j.requests.Add(1)
	if len(ds) > maxProviderDescriptorFanout {
		return nil, fmt.Errorf("descriptor fanout exceeds limit")
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := []environmentartifact.Digest{}
	for _, d := range ds {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		r, ok := j.objects[d.Digest.Hex()]
		if !ok || r.size != d.Size {
			out = append(out, d.Digest)
		}
	}
	return out, nil
}
func (j *Journal) PutObject(ctx context.Context, b Blob) error {
	j.requests.Add(1)
	if b.Reader == nil || b.Descriptor.Size < 0 || b.Descriptor.Size > maxProviderObjectBytes {
		return fmt.Errorf("invalid object upload")
	}
	j.adminMu.Lock()
	defer j.adminMu.Unlock()
	t, e := os.CreateTemp(j.objectDir, ".upload-")
	if e != nil {
		return e
	}
	name := t.Name()
	defer os.Remove(name)
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(t, h), io.LimitReader(&contextReader{ctx: ctx, reader: b.Reader}, b.Descriptor.Size+1))
	if e != nil {
		t.Close()
		return e
	}
	if e = t.Close(); e != nil {
		return e
	}
	actual, e := environmentartifact.ParseDigest("sha256:" + hex.EncodeToString(h.Sum(nil)))
	if e != nil || n != b.Descriptor.Size || actual != b.Descriptor.Digest {
		return fmt.Errorf("object size or digest mismatch")
	}
	k := b.Descriptor.Digest.Hex()
	j.mu.Lock()
	defer j.mu.Unlock()
	if r, ok := j.objects[k]; ok {
		if r.size != n {
			return fmt.Errorf("conflicting immutable object")
		}
		return nil
	}
	final := filepath.Join(j.objectDir, k)
	if e = os.Rename(name, final); e != nil {
		return e
	}
	if e = j.append(journalRecord{Kind: "object", Digest: k, MediaType: b.Descriptor.MediaType, Size: n}); e != nil {
		return e
	}
	j.objects[k] = objectRef{final, n, b.Descriptor.MediaType}
	return nil
}
func (j *Journal) GetObject(ctx context.Context, d environmentartifact.Descriptor) (io.ReadCloser, error) {
	j.requests.Add(1)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	j.mu.RLock()
	r, ok := j.objects[d.Digest.Hex()]
	j.mu.RUnlock()
	if !ok || r.size != d.Size {
		return nil, os.ErrNotExist
	}
	if e := verifyJournalFile(r.path, d.Digest.Hex(), d.Size); e != nil {
		return nil, e
	}
	return os.Open(r.path)
}
func (j *Journal) GetObjectByDigest(ctx context.Context, d environmentartifact.Digest) (io.ReadCloser, int64, error) {
	j.requests.Add(1)
	if e := ctx.Err(); e != nil {
		return nil, 0, e
	}
	j.mu.RLock()
	r, ok := j.objects[d.Hex()]
	j.mu.RUnlock()
	if !ok {
		return nil, 0, os.ErrNotExist
	}
	if e := verifyJournalFile(r.path, d.Hex(), r.size); e != nil {
		return nil, 0, e
	}
	f, e := os.Open(r.path)
	return f, r.size, e
}
func (j *Journal) CommitManifest(ctx context.Context, c []byte) error {
	j.requests.Add(1)
	if e := ctx.Err(); e != nil {
		return e
	}
	m, e := environmentartifact.DecodeManifest(c)
	if e != nil {
		return e
	}
	j.adminMu.Lock()
	defer j.adminMu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	k := m.ArtifactDigest.Hex()
	if p, ok := j.manifests[k]; ok {
		if !bytes.Equal(p, c) {
			return fmt.Errorf("conflicting immutable manifest")
		}
		return nil
	}
	if e = j.verifyManifestClosure(m); e != nil {
		return e
	}
	now := time.Now()
	if e = j.append(journalRecord{Kind: "manifest", Digest: k, Content: base64.StdEncoding.EncodeToString(c), At: now.UnixNano()}); e != nil {
		return e
	}
	j.manifests[k] = append([]byte(nil), c...)
	j.manifestTimes[k] = now
	return nil
}
func (j *Journal) verifyManifestClosure(m environmentartifact.Manifest) error {
	if len(m.Catalogs) == 0 {
		return fmt.Errorf("manifest catalog is empty")
	}
	refs := []environmentartifact.Descriptor{m.Specification.Descriptor, m.LegacyBlueprint.Descriptor, m.ObjectIndex, m.Catalogs[0].Descriptor}
	for _, x := range refs {
		r, ok := j.objects[x.Digest.Hex()]
		if !ok || r.size != x.Size || verifyJournalFile(r.path, x.Digest.Hex(), x.Size) != nil {
			return fmt.Errorf("manifest dependency %s is not complete", x.Digest)
		}
	}
	r := j.objects[m.ObjectIndex.Digest.Hex()]
	b, e := readBoundedJournalIndex(r.path)
	if e != nil {
		return e
	}
	idx, e := environmentartifact.DecodeObjectIndex(b)
	if e != nil {
		return e
	}
	if e := validateObjectIndexBudget(idx); e != nil {
		return e
	}
	for _, x := range idx.Entries {
		r, ok := j.objects[x.StoredDigest.Hex()]
		if !ok || r.size != x.StoredSize || verifyJournalFile(r.path, x.StoredDigest.Hex(), x.StoredSize) != nil {
			return fmt.Errorf("manifest dependency %s is not complete", x.StoredDigest)
		}
	}
	return nil
}
func (j *Journal) ResolveManifest(ctx context.Context, d environmentartifact.Digest) ([]byte, error) {
	j.requests.Add(1)
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	j.mu.RLock()
	b, ok := j.manifests[d.Hex()]
	j.mu.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}
func (j *Journal) append(r journalRecord) error {
	if r.Actor == "" {
		r.Actor = "rcc"
	}
	if r.Provider == "" {
		r.Provider = "artifactprovider/journal"
	}
	if r.Reference == "" {
		r.Reference = j.path
	}
	if journalAppendHook != nil {
		if err := journalAppendHook(r); err != nil {
			return err
		}
	}
	f, e := os.OpenFile(j.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	if _, e = f.Write(append(b, '\n')); e != nil {
		return e
	}
	return f.Sync()
}

func (j *Journal) appendBatch(records []journalRecord) error {
	if len(records) == 0 {
		return nil
	}
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, record := range records {
		if record.Actor == "" {
			record.Actor = "rcc"
		}
		if record.Provider == "" {
			record.Provider = "artifactprovider/journal"
		}
		if record.Reference == "" {
			record.Reference = j.path
		}
		if journalAppendHook != nil {
			if err := journalAppendHook(record); err != nil {
				return err
			}
		}
		b, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err = f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return f.Sync()
}

func appendJournalRecord(records []journalRecord, extra journalRecord) []journalRecord {
	if extra.Kind == "" {
		return records
	}
	return append(records, extra)
}
func (j *Journal) ListObjects(ctx context.Context) ([]ObjectInfo, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]ObjectInfo, 0, len(j.objects))
	for k, r := range j.objects {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		d, e := environmentartifact.ParseDigest("sha256:" + k)
		if e != nil {
			return nil, e
		}
		st, e := os.Stat(r.path)
		if e != nil {
			return nil, e
		}
		out = append(out, ObjectInfo{Digest: d, Size: r.size, ModifiedAt: st.ModTime()})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Digest.Hex() < out[b].Digest.Hex() })
	return out, nil
}
func (j *Journal) ListManifests(ctx context.Context) ([]ManifestInfo, error) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]ManifestInfo, 0, len(j.manifests))
	for k, b := range j.manifests {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		d, e := environmentartifact.ParseDigest("sha256:" + k)
		if e != nil {
			return nil, e
		}
		out = append(out, ManifestInfo{Digest: d, Size: int64(len(b)), ModifiedAt: j.manifestTimes[k]})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Digest.Hex() < out[b].Digest.Hex() })
	return out, nil
}
func (j *Journal) Audit(ctx context.Context) ([]AuditRecord, error) {
	f, err := os.Open(j.path)
	if os.IsNotExist(err) {
		return []AuditRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []AuditRecord{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), int(maxManifestBytes+4096))
	for s.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var r journalRecord
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return nil, err
		}
		if r.Kind == "restore-begin" || r.Kind == "restore-commit" {
			continue
		}
		action := r.Kind
		if strings.HasPrefix(action, "delete-") {
			action = "gc-" + strings.TrimPrefix(action, "delete-")
		}
		at := time.Unix(0, r.At)
		if r.At == 0 {
			at = time.Time{}
		}
		out = append(out, AuditRecord{At: at, Action: action, Actor: r.Actor, Provider: r.Provider, Reference: r.Reference, Digest: r.Digest})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (j *Journal) Cleanup(ctx context.Context) (int, error) {
	n := 0
	ents, e := os.ReadDir(j.objectDir)
	if e != nil {
		return 0, e
	}
	for _, ent := range ents {
		if e := ctx.Err(); e != nil {
			return n, e
		}
		if !strings.HasPrefix(ent.Name(), ".upload-") {
			continue
		}
		if e = os.Remove(filepath.Join(j.objectDir, ent.Name())); e == nil {
			n++
		}
	}
	return n, nil
}
func (j *Journal) GarbageCollect(ctx context.Context, r Retention) (GCReport, error) {
	j.gcActive.Store(true)
	defer j.gcActive.Store(false)
	j.adminMu.Lock()
	defer j.adminMu.Unlock()
	if err := ctx.Err(); err != nil {
		return GCReport{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	report := GCReport{}
	keep := map[string]bool{}
	now := time.Now()
	kept := 0
	for k, b := range j.manifests {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		report.ManifestsScanned++
		expired := r.MaxAge > 0 && !j.manifestTimes[k].IsZero() && now.Sub(j.manifestTimes[k]) > r.MaxAge
		if expired && (r.KeepManifests <= 0 || kept >= r.KeepManifests) {
			if err := j.append(journalRecord{Kind: "delete-manifest", Digest: k}); err != nil {
				return report, err
			}
			delete(j.manifests, k)
			delete(j.manifestTimes, k)
			report.ManifestsRemoved++
			continue
		}
		kept++
		m, err := environmentartifact.DecodeManifest(b)
		if err != nil {
			return report, err
		}
		keep[m.Specification.Descriptor.Digest.Hex()] = true
		keep[m.LegacyBlueprint.Descriptor.Digest.Hex()] = true
		keep[m.ObjectIndex.Digest.Hex()] = true
		for _, c := range m.Catalogs {
			keep[c.Digest.Hex()] = true
		}
		if ref, ok := j.objects[m.ObjectIndex.Digest.Hex()]; ok {
			ib, err := readBoundedJournalIndex(ref.path)
			if err != nil {
				return report, err
			}
			idx, err := environmentartifact.DecodeObjectIndex(ib)
			if err != nil {
				return report, err
			}
			if err := validateObjectIndexBudget(idx); err != nil {
				return report, err
			}
			for _, entry := range idx.Entries {
				keep[entry.StoredDigest.Hex()] = true
			}
		}
	}
	for k, ref := range j.objects {
		report.ObjectsScanned++
		if keep[k] {
			continue
		}
		if err := j.append(journalRecord{Kind: "delete-object", Digest: k}); err != nil {
			return report, err
		}
		if err := os.Remove(ref.path); err != nil && !os.IsNotExist(err) {
			return report, err
		}
		delete(j.objects, k)
		report.ObjectsRemoved++
		report.BytesReclaimed += ref.size
	}
	return report, nil
}
func (j *Journal) Repair(ctx context.Context) (Health, error) {
	j.repairActive.Store(true)
	defer j.repairActive.Store(false)
	if _, e := j.ListObjects(ctx); e != nil {
		return Health{Ready: false, Corrupt: true, Storage: "corrupt"}, e
	}
	return j.Health(ctx)
}
func (j *Journal) ReadOnly() bool { return false }
func (j *Journal) Backup(ctx context.Context, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("nil backup writer")
	}
	tw := tar.NewWriter(w)
	defer tw.Close()
	members := 0
	var total int64
	addMember := func(size int64) error {
		members++
		if members > maxProviderArchiveMembers || size < 0 || size > maxProviderArchiveBytes-total {
			return fmt.Errorf("backup archive exceeds bounds")
		}
		total += size
		return nil
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	for _, entry := range []struct {
		name, path string
		max        int64
	}{{"journal", j.path, maxProviderJSONBytes}, {"policy", j.path + ".policy", maxProviderJSONBytes}} {
		if f, e := os.Open(entry.path); e == nil {
			st, _ := f.Stat()
			if e := addMember(st.Size()); e != nil {
				f.Close()
				return e
			}
			if st.Size() > entry.max {
				f.Close()
				return fmt.Errorf("backup member too large")
			}
			if e = tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0600, Size: st.Size()}); e != nil {
				f.Close()
				return e
			}
			if _, e = io.CopyN(tw, f, st.Size()); e != nil {
				f.Close()
				return e
			}
			f.Close()
		}
	}
	for k, b := range j.manifests {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e := tw.WriteHeader(&tar.Header{Name: filepath.Join("manifests", k), Mode: 0600, Size: int64(len(b))}); e != nil {
			return e
		}
		if e := addMember(int64(len(b))); e != nil {
			return e
		}
		if _, e := tw.Write(b); e != nil {
			return e
		}
	}
	for k, r := range j.objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, e := os.Open(r.path)
		if e != nil {
			return e
		}
		if e = tw.WriteHeader(&tar.Header{Name: filepath.Join("objects", k), Mode: 0600, Size: r.size}); e != nil {
			f.Close()
			return e
		}
		if e := addMember(r.size); e != nil {
			f.Close()
			return e
		}
		_, e = io.CopyN(tw, f, r.size)
		f.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
func (j *Journal) Restore(ctx context.Context, r io.Reader) error {
	if r == nil {
		return fmt.Errorf("nil restore reader")
	}
	stage, e := os.MkdirTemp(j.objectDir, ".restore-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	tr := tar.NewReader(r)
	seen := map[string]bool{}
	members := 0
	var total int64
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		name := filepath.Clean(h.Name)
		if h.Typeflag != tar.TypeReg || name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, "..") || strings.Contains(name, "../") || seen[name] {
			return fmt.Errorf("unsafe or duplicate backup path %q", h.Name)
		}
		seen[name] = true
		members++
		if members > maxProviderArchiveMembers {
			return fmt.Errorf("backup archive has too many members")
		}
		max := int64(maxProviderObjectBytes)
		if name == "journal" || name == "policy" || strings.HasPrefix(name, "manifests/") {
			max = maxManifestBytes
		}
		if h.Size < 0 || h.Size > max || h.Size > maxProviderArchiveBytes-total {
			return fmt.Errorf("backup member too large")
		}
		total += h.Size
		target := filepath.Join(stage, name)
		if e := os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return e
		}
		f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		n, e := io.Copy(f, io.LimitReader(tr, h.Size+1))
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if n != h.Size {
			return fmt.Errorf("backup member size mismatch")
		}
		if strings.HasPrefix(name, "objects/") {
			d, e := environmentartifact.ParseDigest("sha256:" + filepath.Base(name))
			if e != nil {
				return e
			}
			if e := verifyJournalFile(target, d.Hex(), h.Size); e != nil {
				return fmt.Errorf("backup object digest mismatch")
			}
		} else if strings.HasPrefix(name, "manifests/") {
			d, e := environmentartifact.ParseDigest("sha256:" + filepath.Base(name))
			if e != nil {
				return e
			}
			b, e := os.ReadFile(target)
			if e != nil {
				return e
			}
			m, e := environmentartifact.DecodeManifest(b)
			if e != nil || m.ArtifactDigest != d {
				return fmt.Errorf("backup manifest digest mismatch")
			}
		}
	}
	j.adminMu.Lock()
	defer j.adminMu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	objectRecords := make([]journalRecord, 0)
	manifestRecords := make([]journalRecord, 0)
	txn := fmt.Sprintf("restore-%d-%d", os.Getpid(), time.Now().UnixNano())
	newObjects := make(map[string]objectRef)
	newManifests := make(map[string][]byte)
	newTimes := make(map[string]time.Time)
	for name := range seen {
		if !strings.HasPrefix(name, "objects/") && !strings.HasPrefix(name, "manifests/") {
			continue
		}
		src := filepath.Join(stage, name)
		if strings.HasPrefix(name, "objects/") {
			d := filepath.Base(name)
			dst := filepath.Join(j.objectDir, d)
			if _, e := os.Stat(dst); e == nil {
				oldFile, oe := os.Open(dst)
				newFile, ne := os.Open(src)
				if oe != nil || ne != nil {
					if oldFile != nil {
						oldFile.Close()
					}
					if newFile != nil {
						newFile.Close()
					}
					return fmt.Errorf("backup conflicts with immutable object %q", d)
				}
				oldStat, se := oldFile.Stat()
				if se != nil || verifyJournalFile(dst, d, oldStat.Size()) != nil {
					oldFile.Close()
					newFile.Close()
					return fmt.Errorf("backup conflicts with immutable object %q", d)
				}
				same, ce := sameContent(oldFile, newFile)
				oldFile.Close()
				newFile.Close()
				if ce != nil || !same {
					return fmt.Errorf("backup conflicts with immutable object %q", d)
				}
				continue
			}
			st, e := os.Stat(src)
			if e != nil {
				return e
			}
			newObjects[d] = objectRef{dst, st.Size(), ""}
			objectRecords = append(objectRecords, journalRecord{Kind: "object", Digest: d, Size: st.Size(), Txn: txn})
		} else {
			d := filepath.Base(name)
			b, e := os.ReadFile(src)
			if e != nil {
				return e
			}
			if old, oe := j.manifests[d]; oe {
				if !bytes.Equal(old, b) {
					return fmt.Errorf("backup conflicts with immutable manifest %q", d)
				}
				continue
			}
			newManifests[d] = append([]byte(nil), b...)
			newTimes[d] = time.Now()
			manifestRecords = append(manifestRecords, journalRecord{Kind: "manifest", Digest: d, Content: base64.StdEncoding.EncodeToString(b), At: newTimes[d].UnixNano(), Txn: txn})
		}
	}
	// Validate the complete staged closure before publishing anything.
	for d, ref := range newObjects {
		if e := verifyJournalFile(filepath.Join(stage, "objects", d), d, ref.size); e != nil {
			return e
		}
	}
	for _, b := range newManifests {
		m, e := environmentartifact.DecodeManifest(b)
		if e != nil {
			return e
		}
		if len(m.Catalogs) == 0 {
			return fmt.Errorf("manifest catalog is empty")
		}
		for _, x := range []environmentartifact.Descriptor{m.Specification.Descriptor, m.LegacyBlueprint.Descriptor, m.ObjectIndex, m.Catalogs[0].Descriptor} {
			if ref, ok := j.objects[x.Digest.Hex()]; !ok {
				if n, staged := newObjects[x.Digest.Hex()]; !staged || n.size != x.Size {
					return fmt.Errorf("manifest dependency %s is not complete", x.Digest)
				}
			} else if ref.size != x.Size || verifyJournalFile(ref.path, x.Digest.Hex(), x.Size) != nil {
				return fmt.Errorf("manifest dependency %s is not complete", x.Digest)
			}
		}
		idxPath := filepath.Join(stage, "objects", m.ObjectIndex.Digest.Hex())
		if _, ok := newObjects[m.ObjectIndex.Digest.Hex()]; !ok {
			if ref, exists := j.objects[m.ObjectIndex.Digest.Hex()]; exists {
				idxPath = ref.path
			}
		}
		idxBytes, e := readBoundedJournalIndex(idxPath)
		if e != nil {
			return e
		}
		idx, e := environmentartifact.DecodeObjectIndex(idxBytes)
		if e != nil {
			return e
		}
		if e := validateObjectIndexBudget(idx); e != nil {
			return e
		}
		for _, x := range idx.Entries {
			if ref, ok := j.objects[x.StoredDigest.Hex()]; !ok {
				if n, staged := newObjects[x.StoredDigest.Hex()]; !staged || n.size != x.StoredSize {
					return fmt.Errorf("manifest dependency %s is not complete", x.StoredDigest)
				}
			} else if ref.size != x.StoredSize || verifyJournalFile(ref.path, x.StoredDigest.Hex(), x.StoredSize) != nil {
				return fmt.Errorf("manifest dependency %s is not complete", x.StoredDigest)
			}
		}
	}
	policyRecord := journalRecord{}
	if src := filepath.Join(stage, "policy"); func() bool { _, e := os.Stat(src); return e == nil }() {
		b, e := os.ReadFile(src)
		if e != nil {
			return e
		}
		policyRecord = journalRecord{Kind: "policy", Content: base64.StdEncoding.EncodeToString(b), Txn: txn}
	}
	if len(objectRecords) == 0 && len(manifestRecords) == 0 && policyRecord.Kind == "" {
		return nil
	}
	if e := j.append(journalRecord{Kind: "restore-begin", Txn: txn}); e != nil {
		return e
	}
	if e := j.appendBatch(objectRecords); e != nil {
		return e
	}
	for d, ref := range newObjects {
		if e := os.Rename(filepath.Join(stage, "objects", d), ref.path); e != nil {
			return e
		}
		j.objects[d] = ref
	}
	if e := j.appendBatch(appendJournalRecord(manifestRecords, policyRecord)); e != nil {
		for d, ref := range newObjects {
			_ = os.Remove(ref.path)
			delete(j.objects, d)
		}
		return e
	}
	if e := j.append(journalRecord{Kind: "restore-commit", Txn: txn}); e != nil {
		for d, ref := range newObjects {
			_ = os.Remove(ref.path)
			delete(j.objects, d)
		}
		return e
	}
	if policyRecord.Kind != "" {
		if e := j.applyJournalRecord(policyRecord); e != nil {
			return e
		}
	}
	for d, b := range newManifests {
		j.manifests[d] = b
		j.manifestTimes[d] = newTimes[d]
	}
	return nil
}

var _ Provider = (*Journal)(nil)
var _ ProviderV1Admin = (*Journal)(nil)
var _ ProviderV1Backup = (*Journal)(nil)
var _ ProviderV1ReadOnly = (*Journal)(nil)
var _ ProviderV1Audit = (*Journal)(nil)
