//go:build !windows

package operations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
)

func TestExecutionEnvironmentListingPipFallback(t *testing.T) {
	previous := common.NoOutputCapture
	t.Cleanup(func() { common.NoOutputCapture = previous })
	for _, capture := range []bool{false, true} {
		for _, test := range []struct {
			name string
			exit string
			want bool
		}{{"successful", "0", true}, {"failed", "42", false}} {
			t.Run(test.name+map[bool]string{false: "/direct", true: "/captured"}[capture], func(t *testing.T) {
				root := t.TempDir()
				pip := filepath.Join(root, "pip")
				if err := os.WriteFile(pip, []byte("#!/bin/sh\nprintf 'fixture-package==1.0\\n'\nexit "+test.exit+"\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				common.NoOutputCapture = !capture
				got := ExecutionEnvironmentListing(filepath.Join(root, "missing-dependencies.yaml"), root, pathlib.PathParts{root}, root, filepath.Join(root, "output"), os.Environ())
				if got != test.want {
					t.Fatalf("pip fallback result = %v, want %v", got, test.want)
				}
				if capture {
					output, err := os.ReadFile(filepath.Join(root, "output", "stdout.log"))
					if err != nil || string(output) != "fixture-package==1.0\n" {
						t.Fatalf("captured pip output = %q, error %v", output, err)
					}
				}
			})
		}
	}
}
