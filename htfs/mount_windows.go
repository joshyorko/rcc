//go:build windows

package htfs

import (
	"os"
	"path/filepath"
	"strings"
)

// sameMountPoint checks if two paths are on the same filesystem/mount point
// On Windows, we check if they're on the same drive volume
func sameMountPoint(path1, path2 string) bool {
	// Get absolute paths to ensure we have full paths with drive letters
	abs1, err1 := filepath.Abs(path1)
	abs2, err2 := filepath.Abs(path2)

	if err1 != nil || err2 != nil {
		// Can't resolve paths, assume different filesystems
		return false
	}

	// Extract volume names (e.g., "C:", "D:", "\\server\share")
	vol1 := filepath.VolumeName(abs1)
	vol2 := filepath.VolumeName(abs2)

	// Simple volume comparison - same volume means same filesystem
	// This handles both drive letters and UNC paths
	return strings.EqualFold(vol1, vol2)
}

// getDeviceID returns a pseudo device ID for a path on Windows
// Returns the volume name hash as a simple identifier
// Returns -1 if the path doesn't exist or can't be resolved
func getDeviceID(path string) int64 {
	// Check if path exists first - on Windows, filepath.Abs succeeds even
	// for nonexistent paths by resolving relative to current drive
	if _, err := os.Stat(path); err != nil {
		return -1
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return -1
	}

	vol := filepath.VolumeName(absPath)
	if vol == "" {
		return -1
	}

	// Simple hash of volume name for consistency
	var hash int64
	for _, r := range vol {
		hash = hash*31 + int64(r)
	}
	return hash
}