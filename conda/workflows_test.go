package conda

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/shell"
)

func TestLegacyUvInstallCommandBindsStagedPythonAndPreservesCondaCompatibility(t *testing.T) {
	uv := filepath.Join("/stage", "bin", "uv")
	python := filepath.Join("/stage", "bin", "python")
	got := legacyUvInstallCommand(uv, python, "/cache/uv", "/cache/wheels", "/tmp/requirements.txt")
	want := []string{
		uv, "pip", "install", "--system", "--python", python,
		"--link-mode", "copy", "--color", "never", "--cache-dir", "/cache/uv",
		"--find-links", "/cache/wheels", "--requirement", "/tmp/requirements.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected legacy uv command:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestLegacyUvExecutionEnvironmentRejectsInheritedPythonSelector(t *testing.T) {
	base := []string{
		"UV_PYTHON=/decoy/bin/python",
		"uv_python=/second-decoy/python",
		"UV_INDEX_URL=https://mirror.example/simple",
		"VIRTUAL_ENV=/decoy/venv",
		"CONDA_PREFIX=/stage",
		"PATH=/stage/bin:/usr/bin",
	}
	got := legacyUvExecutionEnvironment(base)
	for _, entry := range got {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "UV_PYTHON") {
			t.Fatalf("inherited UV_PYTHON selector survived: %#v", got)
		}
	}
	for _, preserved := range []string{
		"UV_INDEX_URL=https://mirror.example/simple",
		"VIRTUAL_ENV=/decoy/venv",
		"CONDA_PREFIX=/stage",
		"PATH=/stage/bin:/usr/bin",
	} {
		found := false
		for _, entry := range got {
			if entry == preserved {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpectedly removed environment setting %q: %#v", preserved, got)
		}
	}
}

func TestLegacyUvChildUsesStagedPrefixDespiteHostSelectors(t *testing.T) {
	home := t.TempDir()
	previousHome := common.Product.Home()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	prefix := filepath.Join(home, "holotree", "h123t")
	bin := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(bin, "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	uv := filepath.Join(bin, "uv")
	uvRecord := filepath.Join(home, "uv-record")
	uvScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$UV_RECORD\"\nprintf 'UV_PYTHON=%s\\nVIRTUAL_ENV=%s\\nCONDA_PREFIX=%s\\nPATH_PYTHON=%s\\n' \"$UV_PYTHON\" \"$VIRTUAL_ENV\" \"$CONDA_PREFIX\" \"$(command -v python)\" >> \"$UV_RECORD\"\n"
	if err := os.WriteFile(uv, []byte(uvScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UV_PYTHON", "/decoy/bin/python")
	t.Setenv("VIRTUAL_ENV", "/decoy/venv")
	t.Setenv("CONDA_PREFIX", "/decoy/conda")
	t.Setenv("PATH", "/decoy/bin")
	t.Setenv("UV_RECORD", uvRecord)

	python, err := stagedTargetPython(prefix)
	if err != nil {
		t.Fatal(err)
	}
	command := legacyUvInstallCommand(uv, python, "/uv-cache", "/wheel-cache", "/requirements.txt")
	environment := legacyUvExecutionEnvironment(CondaExecutionEnvironment(prefix, nil, true))
	if code, err := shell.New(environment, ".", command...).Tracked(io.Discard, false); err != nil || code != 0 {
		t.Fatalf("uv recorder returned code %d, error %v", code, err)
	}
	recorded, err := os.ReadFile(uvRecord)
	if err != nil {
		t.Fatal(err)
	}
	text := string(recorded)
	for _, want := range []string{
		"--system\n--python\n" + python + "\n",
		"UV_PYTHON=\n",
		"VIRTUAL_ENV=\n",
		"CONDA_PREFIX=" + prefix + "\n",
		"PATH_PYTHON=" + python + "\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("recorder output missing %q:\n%s", want, text)
		}
	}
}

func TestStagedTargetPythonFailsWhenMissing(t *testing.T) {
	if _, err := stagedTargetPython(filepath.Join(t.TempDir(), "empty-prefix")); err == nil {
		t.Fatal("expected missing staged Python to fail")
	}
}
