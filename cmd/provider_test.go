package cmd

import (
	"context"
	"testing"
)

func TestProviderReferenceResolutionIsDeferred(t *testing.T) {
	provider, err := newProviderReference("missing-profile")
	if err != nil {
		t.Fatalf("newProviderReference() error = %v", err)
	}
	if _, err := provider.Capabilities(context.Background()); err == nil {
		t.Fatal("missing profile capability lookup unexpectedly succeeded")
	}
}
