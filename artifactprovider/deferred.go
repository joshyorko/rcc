package artifactprovider

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/joshyorko/rcc/environmentartifact"
)

type deferredProvider struct {
	once     sync.Once
	resolve  func() (Provider, error)
	provider Provider
	err      error
}

func NewDeferred(resolve func() (Provider, error)) Provider {
	return &deferredProvider{resolve: resolve}
}

func (d *deferredProvider) get() (Provider, error) {
	d.once.Do(func() {
		if d.resolve == nil {
			d.err = fmt.Errorf("nil provider resolver")
			return
		}
		d.provider, d.err = d.resolve()
		if d.err == nil && d.provider == nil {
			d.err = fmt.Errorf("provider resolver returned nil provider")
		}
	})
	return d.provider, d.err
}
func (d *deferredProvider) Capabilities(ctx context.Context) (Capabilities, error) {
	p, e := d.get()
	if e != nil {
		return Capabilities{}, e
	}
	return p.Capabilities(ctx)
}
func (d *deferredProvider) ResolveManifest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	p, e := d.get()
	if e != nil {
		return nil, e
	}
	return p.ResolveManifest(ctx, digest)
}
func (d *deferredProvider) MissingObjects(ctx context.Context, ds []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	p, e := d.get()
	if e != nil {
		return nil, e
	}
	return p.MissingObjects(ctx, ds)
}
func (d *deferredProvider) PutObject(ctx context.Context, b Blob) error {
	p, e := d.get()
	if e != nil {
		return e
	}
	return p.PutObject(ctx, b)
}
func (d *deferredProvider) GetObject(ctx context.Context, dsc environmentartifact.Descriptor) (io.ReadCloser, error) {
	p, e := d.get()
	if e != nil {
		return nil, e
	}
	return p.GetObject(ctx, dsc)
}
func (d *deferredProvider) CommitManifest(ctx context.Context, b []byte) error {
	p, e := d.get()
	if e != nil {
		return e
	}
	return p.CommitManifest(ctx, b)
}

func ValidateV1Capabilities(c Capabilities) error {
	if !contains(c.SchemaVersions, 1) || !contains(c.DigestAlgorithms, "sha256") || !contains(c.Encodings, "gzip") {
		return fmt.Errorf("provider does not support environment artifact v1 gzip/sha256")
	}
	return nil
}

func contains[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
