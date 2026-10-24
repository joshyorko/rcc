package conda

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/hamlet"
)

type recordingLayerRecorder struct {
	records int
	err     error
}

func (it *recordingLayerRecorder) Record(_ []byte) error {
	it.records++
	return it.err
}

func TestRemoveExternallyManaged(t *testing.T) {
	tests := []struct {
		name   string
		marker string
	}{
		{"linux style stdlib", filepath.Join("lib", "python3.12", EXTERNALLY_MANAGED)},
		{"windows versioned stdlib", filepath.Join("Lib", "python3.12", EXTERNALLY_MANAGED)},
		{"windows stdlib root", filepath.Join("Lib", EXTERNALLY_MANAGED)},
		{"lowercase stdlib root", filepath.Join("lib", EXTERNALLY_MANAGED)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			must_be, _ := hamlet.Specifications(t)

			targetFolder := t.TempDir()
			marker := filepath.Join(targetFolder, tt.marker)
			must_be.Nil(os.MkdirAll(filepath.Dir(marker), 0o755))
			must_be.Nil(os.WriteFile(marker, []byte("marker"), 0o644))

			removeExternallyManaged(targetFolder)

			_, err := os.Stat(marker)
			must_be.True(os.IsNotExist(err))
		})
	}
}

func TestRemoveExternallyManagedWithoutMarker(t *testing.T) {
	removeExternallyManaged(t.TempDir())
}

func TestUvPythonTarget(t *testing.T) {
	tests := []struct {
		name         string
		python       string
		targetFolder string
		expected     string
	}{
		{
			name:         "prefer resolved python path",
			python:       filepath.Join("C:", "env", "python.exe"),
			targetFolder: filepath.Join("C:", "env"),
			expected:     filepath.Join("C:", "env", "python.exe"),
		},
		{
			name:         "fallback to target folder when python missing",
			python:       "",
			targetFolder: filepath.Join("C:", "env"),
			expected:     filepath.Join("C:", "env"),
		},
		{
			name:         "fallback on whitespace-only python",
			python:       "   ",
			targetFolder: "/tmp/env",
			expected:     "/tmp/env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			must_be, _ := hamlet.Specifications(t)
			must_be.Equal(tt.expected, uvPythonTarget(tt.python, tt.targetFolder))
		})
	}
}

func TestUvCommandEnvironmentRejectsHostUvConfiguration(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"UV_INDEX_URL=https://host.example/simple",
		"uv_cache_dir=/tmp/host-cache",
		"HTTPS_PROXY=https://proxy.example",
	}

	actual := uvCommandEnvironment(base, "UV_PYTHON_INSTALL_DIR=/rcc/uvpython")
	joined := strings.Join(actual, "\n")

	if strings.Contains(strings.ToUpper(joined), "UV_INDEX_URL=") {
		t.Fatalf("host UV_INDEX_URL leaked into uv environment: %q", actual)
	}
	if strings.Contains(strings.ToUpper(joined), "UV_CACHE_DIR=/TMP/HOST-CACHE") {
		t.Fatalf("host UV_CACHE_DIR leaked into uv environment: %q", actual)
	}
	for _, expected := range []string{
		"PATH=/usr/bin",
		"HTTPS_PROXY=https://proxy.example",
		"UV_NO_CONFIG=1",
		"UV_PYTHON_INSTALL_DIR=/rcc/uvpython",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in uv environment: %q", expected, actual)
		}
	}
}

func TestUvPipCommandsTargetStagedPython(t *testing.T) {
	uvBinary := filepath.Join("/rcc", "uv", "0.11.33", "uv")
	python := filepath.Join("/stage", "bin", "python3")

	listCommand := uvPipListCommand(uvBinary, python, "/stage")
	expectedList := []string{
		uvBinary,
		"pip",
		"list",
		"--python",
		python,
		"--format",
		"json",
		"--color",
		"never",
	}
	if !reflect.DeepEqual(expectedList, listCommand) {
		t.Fatalf("unexpected uv pip list command:\nwant: %#v\ngot:  %#v", expectedList, listCommand)
	}

	checkCommand := uvPipCheckCommand(uvBinary, python, "/stage")
	expectedCheck := []string{uvBinary, "pip", "check", "--python", python, "--color", "never"}
	if !reflect.DeepEqual(expectedCheck, checkCommand) {
		t.Fatalf("unexpected uv pip check command:\nwant: %#v\ngot:  %#v", expectedCheck, checkCommand)
	}
}

func TestRecordUvNativeLayerStopsWhenInventoryFails(t *testing.T) {
	recorder := &recordingLayerRecorder{}
	inventoryFailure := errors.New("inventory failed")

	err := recordUvNativeLayer(recorder, []byte("layer"), func() error {
		return inventoryFailure
	})

	if !errors.Is(err, inventoryFailure) {
		t.Fatalf("expected inventory failure, got: %v", err)
	}
	if recorder.records != 0 {
		t.Fatalf("recorded layer after inventory failure: %d", recorder.records)
	}
}

func TestRecordUvNativeLayerPropagatesRecorderFailure(t *testing.T) {
	recordFailure := errors.New("record failed")
	recorder := &recordingLayerRecorder{err: recordFailure}

	err := recordUvNativeLayer(recorder, []byte("layer"), func() error {
		return nil
	})

	if !errors.Is(err, recordFailure) {
		t.Fatalf("expected recorder failure, got: %v", err)
	}
	if recorder.records != 1 {
		t.Fatalf("expected one record attempt, got: %d", recorder.records)
	}
}

func TestCopyPythonPrefixRejectsAmbiguousCacheMatches(t *testing.T) {
	cache := t.TempDir()
	target := t.TempDir()
	for _, name := range []string{
		"cpython-3.12.8-linux-x86_64-gnu",
		"cpython-3.12.8-linux-x86_64-gnu-old",
	} {
		prefix := filepath.Join(cache, name)
		if err := os.MkdirAll(prefix, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(prefix, "python"), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	err := copyPythonPrefix(cache, "3.12.8", target, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("expected ambiguous Python cache error, got: %v", err)
	}
}

func TestCopyPythonPrefixRejectsEscapingSymlink(t *testing.T) {
	cache := t.TempDir()
	target := t.TempDir()
	prefix := filepath.Join(cache, "cpython-3.12.8-linux-x86_64-gnu")
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "outside.txt"), filepath.Join(prefix, "escaped.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := copyPythonPrefix(cache, "3.12.8", target, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "outside Python prefix") {
		t.Fatalf("expected escaping symlink error, got: %v", err)
	}
}

func TestCopyPythonPrefixAllowsInternalRelativeLinkThroughAliasedParent(t *testing.T) {
	parent, err := os.MkdirTemp(".", "copy-python-prefix-alias-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(parent); err != nil {
			t.Errorf("removing temporary prefix tree: %v", err)
		}
	})
	cacheRoot, err := filepath.Abs(filepath.Join(parent, "external-cache"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheAlias := filepath.Join(parent, "cache-alias")
	if err := os.Symlink(cacheRoot, cacheAlias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	prefix := filepath.Join(cacheAlias, "cpython-3.12.8-linux-x86_64-gnu")
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "2to3-3.12"), []byte("script contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("2to3-3.12", filepath.Join(binDir, "2to3")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	target := t.TempDir()
	if err := copyPythonPrefix(cacheAlias, "3.12.8", target, io.Discard); err != nil {
		t.Fatalf("copying prefix through a parent alias should succeed: %v", err)
	}

	copied := filepath.Join(target, "bin", "2to3")
	info, err := os.Lstat(copied)
	if err != nil {
		t.Fatalf("copied relative symlink target is missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copied relative symlink remained a symlink: mode %v", info.Mode())
	}
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("copied relative symlink target is missing: %v", err)
	}
	if string(got) != "script contents" {
		t.Fatalf("copied relative symlink contents = %q, want %q", got, "script contents")
	}
}

func TestCopyPythonPrefixRejectsAbsoluteLinksEvenWhenInternal(t *testing.T) {
	for _, test := range []struct {
		name       string
		linkTarget func(prefix, cache string) string
	}{
		{
			name: "absolute internal target",
			linkTarget: func(prefix, _ string) string {
				return filepath.Join(prefix, "bin", "target")
			},
		},
		{
			name: "absolute outside target",
			linkTarget: func(_, cache string) string {
				return filepath.Join(cache, "outside")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := t.TempDir()
			prefix := filepath.Join(cache, "cpython-3.12.8-linux-x86_64-gnu")
			binDir := filepath.Join(prefix, "bin")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(binDir, "target"), []byte("internal"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cache, "outside"), []byte("outside"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(test.linkTarget(prefix, cache), filepath.Join(binDir, "script")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			err := copyPythonPrefix(cache, "3.12.8", t.TempDir(), io.Discard)
			if err == nil || !strings.Contains(err.Error(), "outside Python prefix") {
				t.Fatalf("expected absolute symlink rejection, got: %v", err)
			}
		})
	}
}

func TestCopyPythonPrefixRejectsEscapingSymlinkChain(t *testing.T) {
	for _, aliased := range []bool{false, true} {
		name := "physical cache"
		if aliased {
			name = "aliased cache parent"
		}
		t.Run(name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			cache := cacheRoot
			if aliased {
				aliasParent := t.TempDir()
				cache = filepath.Join(aliasParent, "cache-alias")
				if err := os.Symlink(cacheRoot, cache); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			prefix := filepath.Join(cache, "cpython-3.12.8-linux-x86_64-gnu")
			binDir := filepath.Join(prefix, "bin")
			linksDir := filepath.Join(prefix, "links")
			outsideDir := filepath.Join(cacheRoot, "outside")
			for _, dir := range []string{binDir, linksDir, outsideDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(outsideDir, "target"), []byte("outside"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join("..", "links", "first"), filepath.Join(binDir, "script")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if err := os.Symlink(filepath.Join("..", "..", "outside", "target"), filepath.Join(linksDir, "first")); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			err := copyPythonPrefix(cache, "3.12.8", t.TempDir(), io.Discard)
			if err == nil || !strings.Contains(err.Error(), "outside Python prefix") {
				t.Fatalf("expected chained relative escape rejection, got: %v", err)
			}
		})
	}
}

func TestCopyPythonPrefixRejectsSymlinkCycle(t *testing.T) {
	cache := t.TempDir()
	prefix := filepath.Join(cache, "cpython-3.12.8-linux-x86_64-gnu")
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ name, target string }{{"first", "second"}, {"second", "first"}} {
		if err := os.Symlink(link.target, filepath.Join(binDir, link.name)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}

	err := copyPythonPrefix(cache, "3.12.8", t.TempDir(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "failed to resolve symlink") {
		t.Fatalf("expected symlink cycle rejection, got: %v", err)
	}
}

func TestCopyPythonPrefixCopiesUnaliasedRelativeLinkAsFile(t *testing.T) {
	cache := t.TempDir()
	prefix := filepath.Join(cache, "cpython-3.12.8-linux-x86_64-gnu")
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "python3.12"), []byte("python contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("python3.12", filepath.Join(binDir, "python")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	target := t.TempDir()
	if err := copyPythonPrefix(cache, "3.12.8", target, io.Discard); err != nil {
		t.Fatalf("copying unaliased prefix failed: %v", err)
	}
	copied := filepath.Join(target, "bin", "python")
	info, err := os.Lstat(copied)
	if err != nil {
		t.Fatalf("copied link is missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copied link remained a symlink: mode %v", info.Mode())
	}
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("reading copied link target failed: %v", err)
	}
	if string(got) != "python contents" {
		t.Fatalf("copied link contents = %q, want %q", got, "python contents")
	}
}
