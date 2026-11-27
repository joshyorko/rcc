package conda

import (
	"bytes"
	"testing"
)

func TestProviderPlanLayersRejectsUnknownProvider(t *testing.T) {
	env := &Environment{ProviderPlan: &ProviderPlan{Layers: []ProviderLayer{{Provider: "unknown", Version: "1"}}}}
	var output bytes.Buffer
	ok, fatal, _, _ := providerPlanLayers(env, t.TempDir(), &output, SkipNoLayers, nil, false)
	if ok || !fatal {
		t.Fatalf("expected explicit rejection, got ok=%v fatal=%v", ok, fatal)
	}
	if output.Len() == 0 {
		t.Fatal("expected a diagnostic")
	}
}

func TestProviderPlanLayersRejectsUnverifiedPartialRestore(t *testing.T) {
	env := &Environment{ProviderPlan: &ProviderPlan{Layers: []ProviderLayer{{Provider: "rattler", Version: "1"}}}}
	var output bytes.Buffer
	ok, fatal, _, _ := providerPlanLayers(env, t.TempDir(), &output, SkipMicromambaLayer, nil, false)
	if ok || !fatal {
		t.Fatalf("expected explicit rejection, got ok=%v fatal=%v", ok, fatal)
	}
}
