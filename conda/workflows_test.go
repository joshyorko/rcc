package conda

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
