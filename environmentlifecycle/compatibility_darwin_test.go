//go:build darwin

package environmentlifecycle

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestDarwinWorkerCapabilitiesAreCompleteAndIndependent(t *testing.T) {
	required := testBuildCompatibility(environmentartifact.CurrentPlatform())
	required.OS.RequiredLibraries = []string{"rcc-impossible-system-library.dylib"}
	required.CPU.RequiredFeatures = []string{"rcc-impossible-cpu-feature"}

	osCapabilities, cpuCapabilities, err := platformWorkerCapabilities(context.Background(), required)
	if err != nil {
		t.Fatal(err)
	}
	if osCapabilities.Family != "darwin" || osCapabilities.Runtime != "darwin" || osCapabilities.NativeArchitecture != runtime.GOARCH {
		t.Fatalf("Darwin worker OS capabilities = %+v", osCapabilities)
	}
	if osCapabilities.Translation != "native" && osCapabilities.Translation != "rosetta2" {
		t.Fatalf("unexpected Darwin translation policy %q", osCapabilities.Translation)
	}
	if cpuCapabilities.Architecture != runtime.GOARCH {
		t.Fatalf("Darwin worker CPU architecture = %q", cpuCapabilities.Architecture)
	}
	worker, err := currentWorkerCapabilities(context.Background(), required)
	if err != nil {
		t.Fatal(err)
	}
	if worker.OS.NativeArchitecture != runtime.GOARCH || worker.CPU.Architecture != runtime.GOARCH || worker.Python.ArtifactProvided != true {
		t.Fatalf("Darwin worker capabilities = %+v", worker)
	}
	if slices.Contains(worker.OS.Libraries, "rcc-impossible-system-library.dylib") || slices.Contains(worker.CPU.Features, "rcc-impossible-cpu-feature") {
		t.Fatal("Darwin worker copied artifact requirements")
	}
}

func TestDarwinMachOAndSystemLibraryProbeReturnsCanonicalRequirements(t *testing.T) {
	root := t.TempDir()
	source := "/usr/bin/true"
	content, err := os.ReadFile(source)
	if err != nil {
		t.Skipf("Darwin system executable unavailable: %v", err)
	}
	path := filepath.Join(root, "true")
	if err := os.WriteFile(path, content, 0o750); err != nil {
		t.Fatal(err)
	}
	platform := environmentartifact.CurrentPlatform()
	requirements, _, err := platformCompatibilityRequirements(root, platform)
	if err != nil {
		t.Fatal(err)
	}
	if requirements.Family != "darwin" || requirements.NativeArchitecture != platform.Arch || requirements.RequiredLibraries == nil {
		t.Fatalf("Darwin requirements = %+v", requirements)
	}
	if platformMaximumPath(false) < 1024 || platformJunctions() {
		t.Fatalf("Darwin filesystem policy = maxPath:%d junctions:%v", platformMaximumPath(false), platformJunctions())
	}
	previousHome := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	if _, err := probeFilesystemCapabilities(); err != nil {
		t.Fatal(err)
	}
}
