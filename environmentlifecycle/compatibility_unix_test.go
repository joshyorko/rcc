//go:build darwin || linux

package environmentlifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
