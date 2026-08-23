package artifactprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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
	Provider                                   Provider
	Limits                                     Limits
	mu                                         sync.Mutex
	bytes, objects, manifests, uploads, window int64
	windowAt                                   time.Time
	statePath                                  string
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
	if err := p.allow(); err != nil {
		return nil, err
	}
	return p.Provider.MissingObjects(ctx, ds)
}
func (p *Policy) GetObject(ctx context.Context, d environmentartifact.Descriptor) (io.ReadCloser, error) {
	if err := p.allow(); err != nil {
		return nil, err
	}
	return p.Provider.GetObject(ctx, d)
}
func (p *Policy) ResolveManifest(ctx context.Context, d environmentartifact.Digest) ([]byte, error) {
	if err := p.allow(); err != nil {
		return nil, err
	}
	return p.Provider.ResolveManifest(ctx, d)
}
func (p *Policy) PutObject(ctx context.Context, b Blob) error {
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
		return fmt.Errorf("%w: uploads", ErrQuotaExceeded)
	}
	if p.Limits.MaxObjects > 0 && p.objects >= p.Limits.MaxObjects {
		return fmt.Errorf("%w: objects", ErrQuotaExceeded)
	}
	if p.Limits.MaxBytes > 0 && p.bytes+b.Descriptor.Size > p.Limits.MaxBytes {
		return fmt.Errorf("%w: bytes", ErrQuotaExceeded)
	}
	p.uploads++
	if err := p.Provider.PutObject(ctx, b); err != nil {
		p.uploads--
		return err
	}
	p.objects++
	p.bytes += b.Descriptor.Size
	if err := p.persistLocked(); err != nil {
		p.reconcileLocked()
		return fmt.Errorf("persist quota state: %w", err)
	}
	return nil
}
func (p *Policy) CommitManifest(ctx context.Context, content []byte) error {
	if err := p.allow(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Limits.MaxManifests > 0 && p.manifests >= p.Limits.MaxManifests {
		return fmt.Errorf("%w: manifests", ErrQuotaExceeded)
	}
	if err := p.Provider.CommitManifest(ctx, content); err != nil {
		return err
	}
	p.manifests++
	if err := p.persistLocked(); err != nil {
		p.reconcileLocked()
		return fmt.Errorf("persist quota state: %w", err)
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
		return h, nil
	}
	return Health{Ready: true, Storage: "ok", Capability: "ok", Quota: "managed", GC: "idle"}, nil
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
