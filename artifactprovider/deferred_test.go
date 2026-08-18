package artifactprovider

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestValidateV1Capabilities(t *testing.T) {
	if err := ValidateV1Capabilities(Capabilities{[]int{1}, []string{"sha256"}, []string{"gzip"}}); err != nil {
		t.Fatal(err)
	}
	for _, capabilities := range []Capabilities{{}, {[]int{2}, []string{"sha256"}, []string{"gzip"}}, {[]int{1}, []string{"sha512"}, []string{"gzip"}}, {[]int{1}, []string{"sha256"}, []string{"zstd"}}} {
		if err := ValidateV1Capabilities(capabilities); err == nil {
			t.Fatalf("accepted %+v", capabilities)
		}
	}
}

func TestDeferredProviderResolvesOnce(t *testing.T) {
	var calls atomic.Int32
	deferred := NewDeferred(func() (Provider, error) { calls.Add(1); return &Filesystem{}, nil })
	_, _ = deferred.Capabilities(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d", calls.Load())
	}
}

func TestDeferredProviderStableError(t *testing.T) {
	want := errors.New("resolver failed")
	deferred := NewDeferred(func() (Provider, error) { return nil, want })
	if _, err := deferred.Capabilities(context.Background()); !errors.Is(err, want) {
		t.Fatal(err)
	}
}
