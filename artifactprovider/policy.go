package artifactprovider

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/joshyorko/rcc/environmentartifact"
)

type Limits struct { MaxBytes int64; MaxObjects int64; MaxManifests int64; MaxUploads int64; RequestsPerSecond int64; Retention time.Duration }
type Policy struct { Provider Provider; Limits Limits; mu sync.Mutex; bytes, objects, manifests, uploads, window int64; windowAt time.Time }
func NewPolicy(provider Provider, limits Limits) *Policy { return &Policy{Provider: provider, Limits: limits} }
func (p *Policy) Capabilities(ctx context.Context) (Capabilities, error) { return p.Provider.Capabilities(ctx) }
func (p *Policy) MissingObjects(ctx context.Context, ds []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) { if err:=p.allow(); err!=nil{return nil,err}; return p.Provider.MissingObjects(ctx, ds) }
func (p *Policy) GetObject(ctx context.Context, d environmentartifact.Descriptor) (io.ReadCloser,error) { if err:=p.allow(); err!=nil{return nil,err}; return p.Provider.GetObject(ctx,d) }
func (p *Policy) ResolveManifest(ctx context.Context, d environmentartifact.Digest) ([]byte,error) { if err:=p.allow(); err!=nil{return nil,err}; return p.Provider.ResolveManifest(ctx,d) }
func (p *Policy) PutObject(ctx context.Context, b Blob) error { if err:=p.allow(); err!=nil{return err}; p.mu.Lock(); if p.Limits.MaxUploads>0 && p.uploads>=p.Limits.MaxUploads { p.mu.Unlock(); return fmt.Errorf("%w: uploads",ErrQuotaExceeded) }; if p.Limits.MaxObjects>0 && p.objects>=p.Limits.MaxObjects { p.mu.Unlock(); return fmt.Errorf("%w: objects",ErrQuotaExceeded) }; if p.Limits.MaxBytes>0 && p.bytes+b.Descriptor.Size>p.Limits.MaxBytes { p.mu.Unlock(); return fmt.Errorf("%w: bytes",ErrQuotaExceeded) }; p.uploads++; p.mu.Unlock(); if err:=p.Provider.PutObject(ctx,b); err!=nil{return err}; p.mu.Lock(); p.objects++; p.bytes+=b.Descriptor.Size; p.mu.Unlock(); return nil }
func (p *Policy) CommitManifest(ctx context.Context, content []byte) error { if err:=p.allow(); err!=nil{return err}; p.mu.Lock(); defer p.mu.Unlock(); if p.Limits.MaxManifests>0 && p.manifests>=p.Limits.MaxManifests{return fmt.Errorf("%w: manifests",ErrQuotaExceeded)}; if err:=p.Provider.CommitManifest(ctx,content); err!=nil{return err}; p.manifests++; return nil }
func (p *Policy) Health(ctx context.Context) (Health,error) { if h,ok:=p.Provider.(HealthProvider); ok{return h.Health(ctx)}; return Health{Ready:true,Storage:"ok",Capability:"ok",Quota:"managed",GC:"idle"},nil }
func (p *Policy) allow() error { p.mu.Lock(); defer p.mu.Unlock(); now:=time.Now(); if p.windowAt.IsZero()||now.Sub(p.windowAt)>=time.Second {p.windowAt=now;p.window=0}; if p.Limits.RequestsPerSecond>0&&p.window>=p.Limits.RequestsPerSecond{return ErrRateLimited}; p.window++; return nil }
var _ Provider = (*Policy)(nil)
