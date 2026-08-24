package conda_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/blobs"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/hamlet"
)

func TestCondaExecutionEnvironmentRejectsProducerOwnedActivationPaths(t *testing.T) {
	home := t.TempDir()
	previousHome := common.Product.Home()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	location := filepath.Join(home, "holotree", "consumer")
	if err := os.MkdirAll(location, 0o755); err != nil {
		t.Fatal(err)
	}
	activation := `{"PATH":"/producer/bin","CONDA_DEFAULT_ENV":"/producer","CONDA_PREFIX":"/producer","CONDA_PREFIX_1":"/producer","CONDA_PROMPT_MODIFIER":"(/producer) ","CONDA_SHLVL":"2","MAMBA_ROOT_PREFIX":"/producer","CUSTOM_ACTIVATION":"preserved"}`
	if err := os.WriteFile(filepath.Join(location, "rcc_activate.json"), []byte(activation), 0o644); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{}
	for _, entry := range conda.CondaExecutionEnvironment(location, nil, false) {
		name, value, found := strings.Cut(entry, "=")
		if found {
			environment[strings.ToUpper(name)] = value
		}
	}
	for _, name := range []string{"PATH", "CONDA_DEFAULT_ENV", "CONDA_PREFIX", "CONDA_PROMPT_MODIFIER", "MAMBA_ROOT_PREFIX"} {
		if strings.Contains(environment[name], "/producer") {
			t.Fatalf("%s retained producer activation path: %q", name, environment[name])
		}
	}
	if environment["CONDA_PREFIX"] != location || environment["CONDA_SHLVL"] != "1" {
		t.Fatalf("consumer conda identity was overridden: %#v", environment)
	}
	if _, found := environment["CONDA_PREFIX_1"]; found {
		t.Fatalf("producer conda stack entry was retained: %#v", environment)
	}
	if environment["CUSTOM_ACTIVATION"] != "preserved" {
		t.Fatalf("unowned activation variable was removed: %#v", environment)
	}
}

func second(_ interface{}, version string) string {
	return version
}

func TestCanParseMicromambaVersion(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal("0", second(conda.AsVersion("python")))
	must_be.Equal("0.19.1", second(conda.AsVersion("0.19.1")))
	must_be.Equal("0.19.0", second(conda.AsVersion("micromamba: 0.19.0")))
	must_be.Equal("0.19.0", second(conda.AsVersion("\n\n\tmicromamba: 0.19.0 \nlibmamba: 0.18.7\n\n\t")))
	must_be.Equal("0.20", second(conda.AsVersion("microrumba: 0.20")))
}

func TestCanParsePipVersion(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal("20.3.4", second(conda.AsVersion("pip 20.3.4 from /outer/space/python/blah (python 3.9)")))
	must_be.Equal("22.2.2", second(conda.AsVersion("pip 22.2.2 from /outer/space/python/blah (python 3.9)")))
}

func TestCanParseUvVersion(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal("0.9.22", second(conda.AsVersion("uv 0.9.22 (be460de64 2025-06-09)")))
	must_be.Equal("0.4.24", second(conda.AsVersion("uv 0.4.24")))
}

func TestInternalMicromambaVersionConsistency(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	needs, _ := conda.AsVersion(blobs.MicromambaVersion())
	must_be.Equal(uint64(blobs.MicromambaVersionLimit), needs)
}
