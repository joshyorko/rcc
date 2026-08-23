//go:build linux

package environmentlifecycle

import (
	"context"
	"runtime"
	"slices"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestLinuxWorkerCapabilitiesDoNotCopyArtifactRequirements(t *testing.T) {
	required := testBuildCompatibility(environmentartifact.CurrentPlatform())
	required.OS.RequiredLibraries = []string{"librcc-impossible.so"}
	required.CPU.RequiredFeatures = []string{"rcc_impossible_feature"}
	previousHome := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })

	osCapabilities, cpuCapabilities, err := platformWorkerCapabilities(context.Background(), required)
	if err != nil {
		t.Fatal(err)
	}
	if osCapabilities.NativeArchitecture != runtime.GOARCH || cpuCapabilities.Architecture != runtime.GOARCH {
		t.Fatalf("worker architecture = %q/%q, want host %q", osCapabilities.NativeArchitecture, cpuCapabilities.Architecture, runtime.GOARCH)
	}
	if slices.Contains(osCapabilities.Libraries, "librcc-impossible.so") {
		t.Fatal("worker libraries copied an unavailable artifact requirement")
	}
	if slices.Contains(cpuCapabilities.Features, "rcc_impossible_feature") {
		t.Fatal("worker CPU features copied an unavailable artifact requirement")
	}
	worker, err := currentWorkerCapabilities(context.Background(), required)
	if err != nil {
		t.Fatal(err)
	}
	if !worker.Python.ArtifactProvided || slices.Contains(worker.Python.Versions, required.Python.Version) || slices.Contains(worker.Python.ABIs, required.Python.ABI) {
		t.Fatalf("worker Python capabilities copied artifact identity: %+v", worker.Python)
	}
}
