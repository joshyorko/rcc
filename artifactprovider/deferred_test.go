package artifactprovider

import (
	"context"
	"errors"
	"github.com/joshyorko/rcc/environmentartifact"
	"strings"
	"sync/atomic"
	"testing"
)

func TestValidateV1Capabilities(t *testing.T) {
	if err := ValidateV1Capabilities(Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}}); err != nil {
		t.Fatal(err)
	}
	for _, capabilities := range []Capabilities{{}, {SchemaVersions: []int{2}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}}, {SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha512"}, Encodings: []string{"gzip"}}, {SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"zstd"}}} {
		if err := ValidateV1Capabilities(capabilities); err == nil {
			t.Fatalf("accepted %+v", capabilities)
		}
	}
}

func TestValidateCapabilityIntersectionFailsClosed(t *testing.T) {
	server := Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}, SafeRestart: true}
	if err := ValidateCapabilityIntersection(server, Capabilities{SchemaVersions: []int{1}, SafeRestart: true}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCapabilityIntersection(server, Capabilities{RangeSupport: true}); err == nil {
		t.Fatal("accepted unavailable range support")
	}
	if err := ValidateCapabilityIntersection(Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}, RangeSupport: true}, Capabilities{}); err == nil {
		t.Fatal("accepted range without safe restart")
	}
}

func TestObjectIndexExpansionBudgetFailsClosed(t *testing.T) {
	index := environmentartifact.ObjectIndex{Count: 1, TotalStoredBytes: 1, TotalLogicalBytes: maxProviderArchiveBytes, Entries: []environmentartifact.ObjectEntry{{StoredSize: 1, LogicalSize: maxProviderArchiveBytes}}}
	if err := validateObjectIndexBudget(index); err == nil {
		t.Fatal("accepted decompression expansion bomb")
	}
}

func TestObjectIndexAcceptsRepresentativeActionsClosure(t *testing.T) {
	const actionsObjectCount = 5694
	index := environmentartifact.ObjectIndex{
		Count:   actionsObjectCount,
		Entries: make([]environmentartifact.ObjectEntry, actionsObjectCount),
	}
	if err := validateObjectIndexBudget(index); err != nil {
		t.Fatalf("representative Actions closure rejected: %v", err)
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
	_, first := deferred.Capabilities(context.Background())
	_, second := deferred.Capabilities(context.Background())
	if first == nil || second == nil || first.Error() != second.Error() {
		t.Fatalf("unstable resolver error: %v / %v", first, second)
	}
	if strings.Contains(first.Error(), want.Error()) {
		t.Fatalf("resolver error leaked: %v", first)
	}
}

func TestDeferredProviderDoesNotExposeResolverError(t *testing.T) {
	const secret = "deferred-provider-secret-sentinel-7f4c"
	deferred := NewDeferred(func() (Provider, error) { return nil, errors.New("credential=" + secret) })
	_, err := deferred.Capabilities(context.Background())
	if err == nil {
		t.Fatal("expected resolver error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("deferred resolver error exposed secret: %v", err)
	}
}
