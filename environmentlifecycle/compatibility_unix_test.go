//go:build darwin || linux

package environmentlifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestValidateMaterializedCompatibilityChecksPythonABI(t *testing.T) {
	hostPython, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("host Python is unavailable")
	}
	root := t.TempDir()
	wrapper := filepath.Join(root, "python")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", hostPython)), 0o750); err != nil {
		t.Fatal(err)
	}
	probe, err := probePythonCompatibility(context.Background(), wrapper, root)
	if err != nil {
		t.Fatal(err)
	}
	required := testBuildCompatibility(environmentartifact.CurrentPlatform())
	required.Python = environmentartifact.PythonRequirements{
		Implementation: probe.Implementation, Version: probe.Version, ABI: probe.ABI,
	}
	if err := validateMaterializedCompatibility(context.Background(), root, required); err != nil {
		t.Fatal(err)
	}
	required.Python.ABI += "-incompatible"
	if err := validateMaterializedCompatibility(context.Background(), root, required); err == nil {
		t.Fatal("incompatible materialized Python ABI accepted")
	}
}

func TestProbeFilesystemCapabilitiesCreatesMissingProductHome(t *testing.T) {
	previousHome := common.Product.Home()
	home := filepath.Join(t.TempDir(), "missing-home")
	common.Product.ForceHome(home)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	if _, err := probeFilesystemCapabilities(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("compatibility home = mode %o directory:%v, want 0700 directory", info.Mode().Perm(), info.IsDir())
	}
}

func TestProbeFilesystemCapabilitiesRejectsSymlinkedProductHome(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	link := filepath.Join(parent, "home")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	previousHome := common.Product.Home()
	common.Product.ForceHome(link)
	t.Cleanup(func() { common.Product.ForceHome(previousHome) })
	if _, err := probeFilesystemCapabilities(); err == nil {
		t.Fatal("symlinked compatibility home was accepted")
	}
}
