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
		resolved, err := d.resolve()
		if err != nil {
			d.err = fmt.Errorf("deferred provider resolution failed")
			return
		}
		d.provider = resolved
		if d.provider == nil {
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
	for _, version := range c.RequiredVersions {
		if !contains(c.SchemaVersions, version) {
			return fmt.Errorf("provider omits required schema version %d", version)
		}
	}
	for _, feature := range c.RequiredFeatures {
		switch feature {
		case "range":
			if !c.RangeSupport {
				return fmt.Errorf("provider omits required range support")
			}
		case "resume":
			if !c.ResumeSupport {
				return fmt.Errorf("provider omits required resume support")
			}
		case "safeRestart":
			if !c.SafeRestart {
				return fmt.Errorf("provider omits required safe restart")
			}
		default:
			return fmt.Errorf("provider advertises unknown required feature %q", feature)
		}
	}
	return nil
}

// ValidateCapabilityIntersection fails closed when a provider cannot satisfy
// the caller's required protocol, formats, limits, or safety guarantees.
func ValidateCapabilityIntersection(server, required Capabilities) error {
	if err := ValidateCapabilities(server); err != nil {
		return err
	}
	for _, v := range required.SchemaVersions {
		if !contains(server.SchemaVersions, v) {
			return fmt.Errorf("required schema version %d is unavailable", v)
		}
	}
	for _, v := range required.DigestAlgorithms {
		if !contains(server.DigestAlgorithms, v) {
			return fmt.Errorf("required digest algorithm %q is unavailable", v)
		}
	}
	for _, v := range required.Encodings {
		if !contains(server.Encodings, v) {
			return fmt.Errorf("required encoding %q is unavailable", v)
		}
	}
	if required.MaxObjectBytes > 0 && server.MaxObjectBytes < required.MaxObjectBytes {
		return fmt.Errorf("provider object limit is below required limit")
	}
	if required.MaxManifestBytes > 0 && server.MaxManifestBytes < required.MaxManifestBytes {
		return fmt.Errorf("provider manifest limit is below required limit")
	}
	if required.RangeSupport && !server.RangeSupport {
		return fmt.Errorf("required range support is unavailable")
	}
	if required.ResumeSupport && !server.ResumeSupport {
		return fmt.Errorf("required resume support is unavailable")
	}
	if required.SafeRestart && !server.SafeRestart {
		return fmt.Errorf("required safe restart is unavailable")
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
