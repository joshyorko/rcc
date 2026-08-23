package artifactprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type Limits struct {
	MaxBytes          int64
	MaxObjects        int64
	MaxManifests      int64
	MaxUploads        int64
	RequestsPerSecond int64
	Retention         time.Duration
}
type Policy struct {
	Provider                                                        Provider
	Limits                                                          Limits
	mu                                                              sync.Mutex
	bytes, objects, manifests, uploads, window                      int64
	windowAt                                                        time.Time
	statePath                                                       string
	requests, failures, quotaFailures, repairs, gcRuns, auditEvents atomic.Int64
}

func (p *Policy) ListObjects(ctx context.Context) ([]ObjectInfo, error) {
	e, ok := p.Provider.(ProviderV1Enumerable)
	if !ok {
		return nil, fmt.Errorf("enumeration unavailable")
	}
	return e.ListObjects(ctx)
}
func (p *Policy) ListManifests(ctx context.Context) ([]ManifestInfo, error) {
	e, ok := p.Provider.(ProviderV1Enumerable)
	if !ok {
		return nil, fmt.Errorf("enumeration unavailable")
	}
	return e.ListManifests(ctx)
}
func (p *Policy) Audit(ctx context.Context) ([]AuditRecord, error) {
	a, ok := p.Provider.(ProviderV1Audit)
	if !ok {
		return nil, fmt.Errorf("audit unavailable")
	}
	records, err := a.Audit(ctx)
	if err == nil {
		p.auditEvents.Store(int64(len(records)))
	}
	return records, err
}
func (p *Policy) GarbageCollect(ctx context.Context, retention Retention) (GCReport, error) {
	p.gcRuns.Add(1)
	a, ok := p.Provider.(ProviderV1Admin)
	if !ok {
		return GCReport{}, fmt.Errorf("admin unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	report, err := a.GarbageCollect(ctx, retention)
	if err == nil {
		p.reconcileLocked()
		if persistErr := p.persistLocked(); persistErr != nil {
			return report, &MutationError{Operation: "garbage-collect", Committed: true, Objects: p.objects, Manifests: p.manifests, Bytes: p.bytes, Err: persistErr}
		}
	}
	return report, err
}
func (p *Policy) Cleanup(ctx context.Context) (int, error) {
	a, ok := p.Provider.(ProviderV1Admin)
	if !ok {
		return 0, fmt.Errorf("admin unavailable")
	}
	return a.Cleanup(ctx)
}
func (p *Policy) Repair(ctx context.Context) (Health, error) {
	p.repairs.Add(1)
	a, ok := p.Provider.(ProviderV1Admin)
	if !ok {
		return Health{}, fmt.Errorf("admin unavailable")
	}
	return a.Repair(ctx)
}
func (p *Policy) Backup(ctx context.Context, w io.Writer) error {
	a, ok := p.Provider.(ProviderV1Backup)
	if !ok {
		return fmt.Errorf("backup unavailable")
	}
	return a.Backup(ctx, w)
}
func (p *Policy) Restore(ctx context.Context, r io.Reader) error {
	a, ok := p.Provider.(ProviderV1Backup)
	if !ok {
		return fmt.Errorf("restore unavailable")
	}
	if err := a.Restore(ctx, r); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reconcileLocked()
	if err := p.persistLocked(); err != nil {
		return &MutationError{Operation: "restore", Committed: true, Objects: p.objects, Manifests: p.manifests, Bytes: p.bytes, Err: err}
	}
	return nil
}

type policyState struct {
	Bytes, Objects, Manifests, Uploads, Window int64
	WindowAt                                   time.Time
}

func NewPolicy(provider Provider, limits Limits) *Policy {
	p := &Policy{Provider: provider, Limits: limits}
	if j, ok := provider.(*Journal); ok {
		p.statePath = j.path + ".policy"
	} else if f, ok := provider.(*Filesystem); ok {
		p.statePath = f.root + ".policy"
	}
	if b, err := os.ReadFile(p.statePath); err == nil {
		var s policyState
		if json.Unmarshal(b, &s) == nil {
			p.bytes, p.objects, p.manifests, p.uploads, p.window, p.windowAt = s.Bytes, s.Objects, s.Manifests, s.Uploads, s.Window, s.WindowAt
		}
	}
	if enumerable, ok := provider.(ProviderV1Enumerable); ok {
		if p.objects == 0 {
			if list, err := enumerable.ListObjects(context.Background()); err == nil {
				p.objects = int64(len(list))
				for _, o := range list {
					p.bytes += o.Size
				}
			}
		}
		if p.manifests == 0 {
			if list, err := enumerable.ListManifests(context.Background()); err == nil {
				p.manifests = int64(len(list))
			}
		}
	}
	return p
}
func (p *Policy) Capabilities(ctx context.Context) (Capabilities, error) {
	return p.Provider.Capabilities(ctx)
}
func (p *Policy) MissingObjects(ctx context.Context, ds []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	p.requests.Add(1)
	if err := p.allow(); err != nil {
		return nil, err
	}
	return p.Provider.MissingObjects(ctx, ds)
}
func (p *Policy) GetObject(ctx context.Context, d environmentartifact.Descriptor) (io.ReadCloser, error) {
	p.requests.Add(1)
	if err := p.allow(); err != nil {
		return nil, err
	}
	return p.Provider.GetObject(ctx, d)
}
func (p *Policy) ResolveManifest(ctx context.Context, d environmentartifact.Digest) ([]byte, error) {
	p.requests.Add(1)
	if err := p.allow(); err != nil {
		return nil, err
	}
	return p.Provider.ResolveManifest(ctx, d)
}
func (p *Policy) PutObject(ctx context.Context, b Blob) error {
	p.requests.Add(1)
	if err := p.allow(); err != nil {
		return err
	}
	// Idempotent retries of an already committed object must not consume quota.
	if existing, err := p.Provider.GetObject(ctx, b.Descriptor); err == nil {
		_ = existing.Close()
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Limits.MaxUploads > 0 && p.uploads >= p.Limits.MaxUploads {
		p.quotaFailures.Add(1)
		return fmt.Errorf("%w: uploads", ErrQuotaExceeded)
	}
	if p.Limits.MaxObjects > 0 && p.objects >= p.Limits.MaxObjects {
		p.quotaFailures.Add(1)
		return fmt.Errorf("%w: objects", ErrQuotaExceeded)
	}
	if p.Limits.MaxBytes > 0 && p.bytes+b.Descriptor.Size > p.Limits.MaxBytes {
		p.quotaFailures.Add(1)
		return fmt.Errorf("%w: bytes", ErrQuotaExceeded)
	}
	p.uploads++
	if err := p.Provider.PutObject(ctx, b); err != nil {
		p.uploads--
		p.reconcileLocked()
		_ = p.persistLocked()
		return err
	}
	p.objects++
	p.bytes += b.Descriptor.Size
	if err := p.persistLocked(); err != nil {
		p.reconcileLocked()
		return &MutationError{Operation: "put-object", Committed: true, Objects: p.objects, Manifests: p.manifests, Bytes: p.bytes, Err: err}
	}
	return nil
}
func (p *Policy) CommitManifest(ctx context.Context, content []byte) error {
	p.requests.Add(1)
	if err := p.allow(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Limits.MaxManifests > 0 && p.manifests >= p.Limits.MaxManifests {
		p.quotaFailures.Add(1)
		return fmt.Errorf("%w: manifests", ErrQuotaExceeded)
	}
	if err := p.Provider.CommitManifest(ctx, content); err != nil {
		p.reconcileLocked()
		_ = p.persistLocked()
		return err
	}
	p.manifests++
	if err := p.persistLocked(); err != nil {
		p.reconcileLocked()
		return &MutationError{Operation: "commit-manifest", Committed: true, Objects: p.objects, Manifests: p.manifests, Bytes: p.bytes, Err: err}
	}
	return nil
}
func (p *Policy) Health(ctx context.Context) (Health, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if h, ok := p.Provider.(HealthProvider); ok {
		h, err := h.Health(ctx)
		if err != nil {
			return h, err
		}
		if (p.Limits.MaxBytes > 0 && p.bytes >= p.Limits.MaxBytes) || (p.Limits.MaxObjects > 0 && p.objects >= p.Limits.MaxObjects) || (p.Limits.MaxManifests > 0 && p.manifests >= p.Limits.MaxManifests) {
			h.Quota = "exhausted"
			h.Degraded = true
		}
		h.Requests = p.requests.Load()
		h.Errors = p.failures.Load()
		h.QuotaFailures = p.quotaFailures.Load()
		h.Repairs = p.repairs.Load()
		h.GCRuns = p.gcRuns.Load()
		h.AuditEvents = p.auditEvents.Load()
		return h, nil
	}
	return Health{Ready: true, Storage: "ok", Capability: "ok", Quota: "managed", GC: "idle", Process: "local", Audit: "append-only", Requests: p.requests.Load(), Errors: p.failures.Load(), QuotaFailures: p.quotaFailures.Load(), Repairs: p.repairs.Load(), GCRuns: p.gcRuns.Load(), AuditEvents: p.auditEvents.Load()}, nil
}
func (p *Policy) allow() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	oldWindow, oldAt := p.window, p.windowAt
	now := time.Now()
	if p.windowAt.IsZero() || now.Sub(p.windowAt) >= time.Second {
		p.windowAt = now
		p.window = 0
	}
	if p.Limits.RequestsPerSecond > 0 && p.window >= p.Limits.RequestsPerSecond {
		p.failures.Add(1)
		return ErrRateLimited
	}
	p.window++
	if err := p.persistLocked(); err != nil {
		p.window, p.windowAt = oldWindow, oldAt
		return err
	}
	return nil
}

func (p *Policy) reconcileLocked() {
	e, ok := p.Provider.(ProviderV1Enumerable)
	if !ok {
		return
	}
	objects, oe := e.ListObjects(context.Background())
	manifests, me := e.ListManifests(context.Background())
	if oe != nil || me != nil {
		return
	}
	p.objects = int64(len(objects))
	p.bytes = 0
	for _, o := range objects {
		p.bytes += o.Size
	}
	p.manifests = int64(len(manifests))
}

func (p *Policy) persistLocked() error {
	if p.statePath == "" {
		return nil
	}
	b, err := json.Marshal(policyState{p.bytes, p.objects, p.manifests, p.uploads, p.window, p.windowAt})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.statePath), ".policy-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, p.statePath); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(p.statePath))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

var _ Provider = (*Policy)(nil)
var _ ProviderV1Admin = (*Policy)(nil)
var _ ProviderV1Backup = (*Policy)(nil)
var _ ProviderV1Audit = (*Policy)(nil)
