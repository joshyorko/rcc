//go:build !windows

package htfs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestImportProvisioningPreservesConfiguredHomeMode(t *testing.T) {
	if os.Getenv("RCC_HTFS_BOOTSTRAP_TEST_CHILD") == "1" {
		home := os.Getenv("ROBOCORP_HOME")
		info, err := os.Stat(home)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("configured home mode = %#o, want %#o", got, 0o700)
		}

		for _, relative := range []string{
			filepath.Join("hololib", "catalog"),
			filepath.Join("hololib", "library"),
			filepath.Join("hololib", "used"),
			filepath.Join("hololib", "pids"),
		} {
			path := filepath.Join(home, relative)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o750 {
				t.Errorf("provisioned directory %q mode = %#o, want %#o", relative, got, 0o750)
			}
		}
		if info, err := os.Stat(filepath.Join(home, "hololib")); err != nil {
			t.Fatal(err)
		} else if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("pre-existing hololib mode = %#o, want %#o", got, 0o700)
		}
		return
	}

	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	hololib := filepath.Join(home, "hololib")
	if err := os.Mkdir(hololib, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestImportProvisioningPreservesConfiguredHomeMode$")
	command.Env = append(os.Environ(),
		"RCC_HTFS_BOOTSTRAP_TEST_CHILD=1",
		"RCC_HOLOTREE_MODE=private",
		fmt.Sprintf("ROBOCORP_HOME=%s", home),
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("child test failed: %v\n%s", err, output)
	}
}
