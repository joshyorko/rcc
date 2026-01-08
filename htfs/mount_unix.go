//go:build !windows

package htfs

import (
	"os"
	"syscall"
)

// sameMountPoint checks if two paths are on the same filesystem/mount point
// Returns true if they're on the same device, false otherwise
func sameMountPoint(path1, path2 string) bool {
	// Get file info for both paths
	stat1, err1 := os.Stat(path1)
	stat2, err2 := os.Stat(path2)

	if err1 != nil || err2 != nil {
		// If we can't stat either file, assume different filesystems (safer)
		return false
	}

	// Extract system-specific stat info
	sys1, ok1 := stat1.Sys().(*syscall.Stat_t)
	sys2, ok2 := stat2.Sys().(*syscall.Stat_t)

	if !ok1 || !ok2 {
		// Can't get system info, assume different filesystems
		return false
	}

	// Compare device IDs - same device means same filesystem
	return sys1.Dev == sys2.Dev
}

// getDeviceID returns the device ID for a path, or -1 if error
func getDeviceID(path string) int64 {
	stat, err := os.Stat(path)
	if err != nil {
		return -1
	}

	if sys, ok := stat.Sys().(*syscall.Stat_t); ok {
		return int64(sys.Dev)
	}

	return -1
}