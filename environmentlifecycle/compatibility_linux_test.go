//go:build linux

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

func TestBundledLibrarySymlinkIsNotReportedAsSystemRequirement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "libfixture.so.1.0.8")
	if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "libfixture.so.1.0")
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var entry os.DirEntry
	for _, candidate := range entries {
		if candidate.Name() == filepath.Base(link) {
			entry = candidate
		}
	}
	name, bundled := bundledEntryName(root, link, entry)
	if !bundled || name != filepath.Base(link) {
		t.Fatalf("internal library symlink = %q/%v", name, bundled)
	}
}

func TestLinuxRequirementsUsePortableOSIdentityAndKernelFloor(t *testing.T) {
	platform := environmentartifact.CurrentPlatform()
	requirements, _, err := platformCompatibilityRequirements(t.TempDir(), platform)
	if err != nil {
		t.Fatal(err)
	}
	if requirements.Family != "linux" || requirements.MinimumVersion != "1" {
		t.Fatalf("Linux OS identity = %q/%q, want linux/1", requirements.Family, requirements.MinimumVersion)
	}
	if requirements.KernelMinimum != "3.15" {
		t.Fatalf("Linux kernel floor = %q, want 3.15", requirements.KernelMinimum)
	}
}

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
