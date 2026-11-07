//go:build !windows

package environmentlifecycle

import (
	"os"
	"path/filepath"
)

func hasExecutablePathSeparator(command string) bool {
	return filepath.Base(command) != command
}

func executableCandidates(path string) []string {
	return []string{path}
}

func executableFileIsRunnable(info os.FileInfo, _ string) bool {
	return info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}
