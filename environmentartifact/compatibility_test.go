package environmentartifact

import (
	"errors"
	"testing"
)

func testCompatibilityRequirements() CompatibilityRequirements {
	return CompatibilityRequirements{
		SchemaVersion:     1,
		RelocationVersion: "holotree-v12-path-rewrite-v1",
		Python: PythonRequirements{
			Implementation: "cpython", Version: "3.11.9", ABI: "cp311",
		},
		OS: OSRequirements{
			Family: "linux", MinimumVersion: "6.6", KernelMinimum: "6.6", LibC: "glibc", LibCMinimum: "2.34",
			NativeArchitecture: "amd64", TranslationPolicy: "native-only", RequiredLibraries: []string{"libc.so.6"},
		},
		CPU: CPURequirements{Architecture: "amd64", RequiredFeatures: []string{"sse2"}},
		Filesystem: FilesystemRequirements{
			CaseSensitive: true, Symlinks: true, Junctions: false, LongPaths: true, MinimumMaxPath: 260,
		},
		SystemRequirementsOverridden: false,
	}
}

func testWorkerCapabilities() WorkerCapabilities {
	return WorkerCapabilities{
		SchemaVersion:      1,
		RelocationVersions: []string{"holotree-v12-path-rewrite-v1"},
		Python: PythonCapabilities{
			Implementations: []string{"cpython"}, Versions: []string{"3.11.9"}, ABIs: []string{"cp311"},
		},
		OS: OSCapabilities{
			Family: "linux", Version: "6.8", KernelVersion: "6.8", LibC: "glibc", LibCVersion: "2.39",
			NativeArchitecture: "amd64", Translation: "native", Libraries: []string{"libc.so.6", "libm.so.6"},
		},
		CPU: CPUCapabilities{Architecture: "amd64", Features: []string{"sse2", "sse4_2"}},
		Filesystem: FilesystemCapabilities{
			CaseSensitive: true, Symlinks: true, Junctions: false, LongPaths: true, MaxPath: 4096,
		},
	}
}

func TestCompatibilityRequirementsValidateCanonicalContract(t *testing.T) {
	requirements := testCompatibilityRequirements()
	if err := requirements.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*CompatibilityRequirements){
		"schema":       func(value *CompatibilityRequirements) { value.SchemaVersion = 2 },
		"python abi":   func(value *CompatibilityRequirements) { value.Python.ABI = "" },
		"libc":         func(value *CompatibilityRequirements) { value.OS.LibCMinimum = "" },
		"cpu ordering": func(value *CompatibilityRequirements) { value.CPU.RequiredFeatures = []string{"sse4_2", "sse2"} },
		"library dup": func(value *CompatibilityRequirements) {
			value.OS.RequiredLibraries = []string{"libc.so.6", "libc.so.6"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := requirements
			mutate(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatal("invalid compatibility requirements accepted")
			}
		})
	}
}

func TestEvaluateCompatibilityReturnsStableMismatchBeforeUse(t *testing.T) {
	requirements := testCompatibilityRequirements()
	worker := testWorkerCapabilities()
	if err := EvaluateCompatibility(requirements, worker); err != nil {
		t.Fatal(err)
	}

	worker.OS.LibCVersion = "2.31"
	err := EvaluateCompatibility(requirements, worker)
	var mismatch *CompatibilityError
	if !errors.As(err, &mismatch) {
		t.Fatalf("mismatch error = %T %v", err, err)
	}
	if mismatch.Code != "libc-version" || mismatch.Field != "os.libcMinimum" || mismatch.Required != "2.34" || mismatch.Observed != "2.31" {
		t.Fatalf("mismatch = %+v", mismatch)
	}
}

func TestEvaluateCompatibilityRejectsPythonCPUFilesystemAndOverrideMismatch(t *testing.T) {
	requirements := testCompatibilityRequirements()
	for name, test := range map[string]struct {
		mutate func(*WorkerCapabilities)
		code   string
	}{
		"python": {func(worker *WorkerCapabilities) { worker.Python.ABIs = []string{"cp312"} }, "python-abi"},
		"cpu":    {func(worker *WorkerCapabilities) { worker.CPU.Features = []string{} }, "cpu-feature"},
		"case":   {func(worker *WorkerCapabilities) { worker.Filesystem.CaseSensitive = false }, "filesystem-case"},
		"path":   {func(worker *WorkerCapabilities) { worker.Filesystem.MaxPath = 128 }, "filesystem-max-path"},
	} {
		t.Run(name, func(t *testing.T) {
			worker := testWorkerCapabilities()
			test.mutate(&worker)
			var mismatch *CompatibilityError
			if err := EvaluateCompatibility(requirements, worker); !errors.As(err, &mismatch) || mismatch.Code != test.code {
				t.Fatalf("error = %T %v", err, err)
			}
		})
	}
}

func TestEvaluateCompatibilityDefersArtifactProvidedPythonVersionAndABI(t *testing.T) {
	requirements := testCompatibilityRequirements()
	worker := testWorkerCapabilities()
	worker.Python.ArtifactProvided = true
	worker.Python.Versions = []string{}
	worker.Python.ABIs = []string{}
	if err := EvaluateCompatibility(requirements, worker); err != nil {
		t.Fatalf("artifact-provided Python was rejected before materialization: %v", err)
	}
	worker.Python.Implementations = []string{"pypy"}
	var mismatch *CompatibilityError
	if err := EvaluateCompatibility(requirements, worker); !errors.As(err, &mismatch) || mismatch.Code != "python-implementation" {
		t.Fatalf("unsupported Python implementation error = %T %v", err, err)
	}
}

func TestCompatibilityRequirementsAreManifestIdentityBearing(t *testing.T) {
	input := testManifestInput(t)
	input.Requirements.Compatibility = testCompatibilityRequirements()
	first, _, err := NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Requirements.Compatibility.Python.ABI = "cp311d"
	second, _, err := NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ArtifactDigest == second.ArtifactDigest {
		t.Fatal("compatibility change did not change Artifact identity")
	}
}
