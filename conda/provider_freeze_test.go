package conda

import (
	"reflect"
	"testing"
)

func TestProviderFreezeSerializesResolvedOrderedLayers(t *testing.T) {
	original := &Environment{ProviderPlan: &ProviderPlan{Layers: []ProviderLayer{
		{Provider: "rattler", Version: "0.1.0", Channels: []string{"conda-forge"}, Packages: []string{"python>=3.10", "shared"}},
		{Provider: "uv", Version: "0.9.26", Packages: []string{"shared>=1", "demo"}},
		{Provider: "pip", Version: "25.0.1", Packages: []string{"other"}},
	}}}
	fixed := dependencies{{Name: "python", Version: "3.12.4", Origin: "conda-forge"}, {Name: "shared", Version: "2", Origin: "conda-forge"}, {Name: "shared", Version: "3", Origin: "pypi"}, {Name: "demo", Version: "4", Origin: "pypi"}, {Name: "other", Version: "5", Origin: "pypi"}}
	want := [][]string{{"python=3.12.4", "shared=2"}, {"shared==3", "demo==4"}, {"other==5"}}
	check := func(t *testing.T, frozen *Environment) {
		t.Helper()
		serialized, err := frozen.AsYaml()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := CondaYamlFrom([]byte(serialized))
		if err != nil {
			t.Fatal(err)
		}
		for i, layer := range decoded.ProviderPlan.Layers {
			if !reflect.DeepEqual(layer.Packages, want[i]) {
				t.Fatalf("layer %d: %v", i, layer.Packages)
			}
		}
	}
	t.Run("listing", func(t *testing.T) { check(t, original.FreezeDependencies(fixed)) })
	t.Run("wanted", func(t *testing.T) {
		frozen, ok := original.FromDependencies(fixed)
		if !ok {
			t.Fatal("resolved packages reported missing")
		}
		check(t, frozen)
	})
	if original.ProviderPlan.Layers[0].Packages[0] != "python>=3.10" {
		t.Fatal("original plan mutated")
	}
	frozen, ok := original.FromDependencies(fixed[:1])
	if ok || frozen.ProviderPlan.Layers[1].Packages[0] != "shared>=1" {
		t.Fatal("missing packages must remain and be reported")
	}
}
