//go:build windows

package environmentlifecycle

import (
	"os"
	"path/filepath"
	"strings"
)

func hasExecutablePathSeparator(command string) bool {
	return strings.ContainsAny(command, `/\\`)
}

func executableCandidates(path string) []string {
	extension := filepath.Ext(path)
	if extension != "" {
		return []string{path}
	}
	candidates := make([]string, 0, len(windowsExecutableExtensions(os.Getenv("PATHEXT"))))
	for _, extension := range windowsExecutableExtensions(os.Getenv("PATHEXT")) {
		candidates = append(candidates, path+extension)
	}
	return candidates
}

func executableFileIsRunnable(info os.FileInfo, _ string) bool {
	return info.Mode().IsRegular()
}
