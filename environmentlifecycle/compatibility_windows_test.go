//go:build windows

package environmentlifecycle

import (
	"context"
	"runtime"
	"slices"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestWindowsWorkerCapabilitiesAreCompleteAndIndependent(t *testing.T) {
	required := testBuildCompatibility(environmentartifact.CurrentPlatform())
	required.OS.RequiredLibraries = []string{"rcc-impossible-system-library.dll"}
	required.CPU.RequiredFeatures = []string{"rcc-impossible-cpu-feature"}
	required.Filesystem.LongPaths = false
	required.Filesystem.MinimumMaxPath = 260
	osCapabilities, cpuCapabilities, err := platformWorkerCapabilities(context.Background(), required)
	if err != nil {
		t.Fatal(err)
	}
	if osCapabilities.Family != "windows" || osCapabilities.Runtime != "win32" || osCapabilities.NativeArchitecture != runtime.GOARCH {
		t.Fatalf("Windows worker OS capabilities = %+v", osCapabilities)
	}
	if cpuCapabilities.Architecture != runtime.GOARCH {
		t.Fatalf("Windows worker CPU architecture = %q", cpuCapabilities.Architecture)
	}
	worker, err := currentWorkerCapabilities(context.Background(), required)
	if err != nil {
		t.Fatal(err)
	}
	if worker.OS.NativeArchitecture != runtime.GOARCH || worker.CPU.Architecture != runtime.GOARCH || !worker.Python.ArtifactProvided {
		t.Fatalf("Windows worker capabilities = %+v", worker)
	}
	if slices.Contains(worker.OS.Libraries, "rcc-impossible-system-library.dll") || slices.Contains(worker.CPU.Features, "rcc-impossible-cpu-feature") {
		t.Fatal("Windows worker copied artifact requirements")
	}
}

func TestWindowsFilesystemPolicyExposesJunctionAndLongPathFacts(t *testing.T) {
	if platformMaximumPath(false) != 260 || platformMaximumPath(true) != 32767 || !platformJunctions() {
		t.Fatalf("Windows filesystem policy = maxPath(false):%d maxPath(true):%d junctions:%v", platformMaximumPath(false), platformMaximumPath(true), platformJunctions())
	}
	previousHome := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	if _, err := probeFilesystemCapabilities(); err != nil {
		t.Fatal(err)
	}
}
