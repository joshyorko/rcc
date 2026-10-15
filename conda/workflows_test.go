package conda

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
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
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable recorder")
	}
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
	uvScript := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'uv 0.9.22'; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$UV_RECORD\"\nprintf 'UV_PYTHON=%s\\nVIRTUAL_ENV=%s\\nCONDA_PREFIX=%s\\nPATH_PYTHON=%s\\n' \"$UV_PYTHON\" \"$VIRTUAL_ENV\" \"$CONDA_PREFIX\" \"$(command -v python)\" >> \"$UV_RECORD\"\n"
	if err := os.WriteFile(uv, []byte(uvScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UV_PYTHON", "/decoy/bin/python")
	t.Setenv("VIRTUAL_ENV", "/decoy/venv")
	t.Setenv("CONDA_PREFIX", "/decoy/conda")
	t.Setenv("PATH", "/decoy/bin")
	t.Setenv("UV_RECORD", uvRecord)

	requirements := filepath.Join(home, "requirements.txt")
	if err := os.WriteFile(requirements, []byte("example-package==1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var plan bytes.Buffer
	success, fatal, pipUsed, selectedPython := uvLayer("test", requirements, prefix, nil, &plan)
	if !success || fatal || !pipUsed {
		t.Fatalf("uvLayer returned success=%v fatal=%v pipUsed=%v; plan: %s", success, fatal, pipUsed, plan.String())
	}
	recorded, err := os.ReadFile(uvRecord)
	if err != nil {
		t.Fatal(err)
	}
	text := string(recorded)
	for _, want := range []string{
		"--system\n--python\n" + selectedPython + "\n",
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

func TestLegacyUvLayerFailsWhenTargetPythonIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX executable recorder")
	}
	home := t.TempDir()
	previousHome := common.Product.Home()
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	target := filepath.Join(home, "holotree", "h456t")
	bin := filepath.Join(target, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	uv := filepath.Join(bin, "uv")
	if err := os.WriteFile(uv, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	requirements := filepath.Join(home, "requirements.txt")
	if err := os.WriteFile(requirements, []byte("example-package==1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var plan bytes.Buffer
	success, fatal, pipUsed, _ := uvLayer("test", requirements, target, nil, &plan)
	if success || fatal || pipUsed || !strings.Contains(plan.String(), "no python in target folder") {
		t.Fatalf("uvLayer did not fail closed for missing Python: success=%v fatal=%v pipUsed=%v plan=%s", success, fatal, pipUsed, plan.String())
	}
}
