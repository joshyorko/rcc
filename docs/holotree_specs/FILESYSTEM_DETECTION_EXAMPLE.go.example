// +build linux

package htfs

// Example implementation of filesystem compression detection for RCC
// This demonstrates how to detect filesystem types and compression capabilities

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// FSCompressionInfo contains information about filesystem compression capabilities
type FSCompressionInfo struct {
	Filesystem  string // e.g., "btrfs", "xfs", "zfs", "ext4"
	Type        string // Friendly name
	Supported   bool   // Does this FS support transparent compression?
	Enabled     bool   // Is compression actually enabled?
	Algorithm   string // e.g., "zstd:3", "lz4", "none"
	Reflinks    bool   // Does this FS support reflinks?
	Recommended string // Recommendation for RCC
}

// Filesystem magic numbers from Linux kernel
const (
	BTRFS_SUPER_MAGIC   = 0x9123683e
	XFS_SUPER_MAGIC     = 0x58465342
	EXT4_SUPER_MAGIC    = 0xEF53
	ZFS_SUPER_MAGIC     = 0x2fc12fc1
	TMPFS_SUPER_MAGIC   = 0x01021994
	NFS_SUPER_MAGIC     = 0x6969
	CIFS_SUPER_MAGIC    = 0xFF534D42
	BCACHEFS_SUPER_MAGIC = 0xca451a4e
)

// DetectFilesystemCompression detects filesystem type and compression capabilities
func DetectFilesystemCompression(path string) (*FSCompressionInfo, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	if err != nil {
		return nil, fmt.Errorf("statfs failed: %w", err)
	}

	info := &FSCompressionInfo{}

	// Detect filesystem type by magic number
	switch stat.Type {
	case BTRFS_SUPER_MAGIC:
		return detectBtrfsInfo(path)

	case XFS_SUPER_MAGIC:
		info.Filesystem = "xfs"
		info.Type = "XFS"
		info.Supported = false // XFS doesn't support compression
		info.Reflinks = true   // But does support reflinks!
		info.Algorithm = "none"
		info.Recommended = "Store uncompressed, use reflinks for speed"
		return info, nil

	case EXT4_SUPER_MAGIC:
		info.Filesystem = "ext4"
		info.Type = "ext4"
		info.Supported = false
		info.Reflinks = false
		info.Algorithm = "none"
		info.Recommended = "Use application-level gzip compression"
		return info, nil

	case ZFS_SUPER_MAGIC:
		return detectZFSInfo(path)

	case TMPFS_SUPER_MAGIC:
		info.Filesystem = "tmpfs"
		info.Type = "tmpfs (RAM)"
		info.Supported = false
		info.Reflinks = false
		info.Algorithm = "none"
		info.Recommended = "No compression needed (in RAM)"
		return info, nil

	case BCACHEFS_SUPER_MAGIC:
		info.Filesystem = "bcachefs"
		info.Type = "bcachefs"
		info.Supported = true
		info.Reflinks = true
		info.Enabled = true // Usually enabled by default
		info.Algorithm = "zstd (assumed)"
		info.Recommended = "Store uncompressed, use FS compression + reflinks"
		return info, nil

	case NFS_SUPER_MAGIC:
		info.Filesystem = "nfs"
		info.Type = "NFS (network)"
		info.Supported = false
		info.Reflinks = false
		info.Algorithm = "none"
		info.Recommended = "Use application-level compression (network storage)"
		return info, nil

	case CIFS_SUPER_MAGIC:
		info.Filesystem = "cifs"
		info.Type = "CIFS/SMB (network)"
		info.Supported = false
		info.Reflinks = false
		info.Algorithm = "none"
		info.Recommended = "Use application-level compression (network storage)"
		return info, nil

	default:
		info.Filesystem = "unknown"
		info.Type = fmt.Sprintf("Unknown (0x%x)", stat.Type)
		info.Supported = false
		info.Reflinks = false
		info.Algorithm = "unknown"
		info.Recommended = "Use application-level compression (unknown filesystem)"
		return info, nil
	}
}

// detectBtrfsInfo detects Btrfs compression settings
func detectBtrfsInfo(path string) (*FSCompressionInfo, error) {
	info := &FSCompressionInfo{
		Filesystem: "btrfs",
		Type:       "Btrfs",
		Supported:  true,
		Reflinks:   true,
	}

	// Method 1: Try btrfs property get (most reliable)
	algo, err := getBtrfsProperty(path)
	if err == nil && algo != "" && algo != "none" {
		info.Enabled = true
		info.Algorithm = algo
		info.Recommended = fmt.Sprintf("IDEAL: Store uncompressed, FS compresses with %s + reflinks work!", algo)
		return info, nil
	}

	// Method 2: Check mount options in /proc/mounts
	algo, err = getBtrfsMountOption(path)
	if err == nil && algo != "" {
		info.Enabled = true
		info.Algorithm = algo
		info.Recommended = fmt.Sprintf("IDEAL: Store uncompressed, FS compresses with %s + reflinks work!", algo)
		return info, nil
	}

	// No compression detected
	info.Enabled = false
	info.Algorithm = "none"
	info.Recommended = "Btrfs detected but compression not enabled. Enable with: btrfs property set <path> compression zstd"
	return info, nil
}

// getBtrfsProperty gets compression property using btrfs command
func getBtrfsProperty(path string) (string, error) {
	cmd := exec.Command("btrfs", "property", "get", path, "compression")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Output format: "compression=zstd:3" or "compression=" (empty means disabled)
	line := strings.TrimSpace(string(output))
	parts := strings.SplitN(line, "=", 2)
	if len(parts) == 2 {
		algo := strings.TrimSpace(parts[1])
		if algo == "" || algo == "none" {
			return "", nil
		}
		return algo, nil
	}

	return "", nil
}

// getBtrfsMountOption checks mount options for compression
func getBtrfsMountOption(path string) (string, error) {
	// Find the mount point for this path
	mountPoint, err := findMountPoint(path)
	if err != nil {
		return "", err
	}

	// Parse /proc/mounts to find mount options
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}

		// fields[1] is mount point, fields[3] is options
		if fields[1] == mountPoint {
			options := fields[3]
			// Look for compress=zstd or compress=lzo etc.
			for _, opt := range strings.Split(options, ",") {
				if strings.HasPrefix(opt, "compress=") {
					algo := strings.TrimPrefix(opt, "compress=")
					return algo, nil
				}
				if opt == "compress" {
					return "zlib", nil // Default is zlib
				}
			}
		}
	}

	return "", scanner.Err()
}

// findMountPoint finds the mount point for a given path
func findMountPoint(path string) (string, error) {
	var stat1 unix.Stat_t
	err := unix.Stat(path, &stat1)
	if err != nil {
		return "", err
	}

	// Walk up the directory tree until device changes
	current := path
	for {
		parent := current + "/.."
		var stat2 unix.Stat_t
		err := unix.Stat(parent, &stat2)
		if err != nil {
			return current, nil
		}

		// If device changed, we found the mount point
		if stat1.Dev != stat2.Dev {
			return current, nil
		}

		current = parent
		stat1 = stat2

		// Safety: don't go above root
		if current == "/" {
			return "/", nil
		}
	}
}

// detectZFSInfo detects ZFS compression settings
func detectZFSInfo(path string) (*FSCompressionInfo, error) {
	info := &FSCompressionInfo{
		Filesystem: "zfs",
		Type:       "ZFS",
		Supported:  true,
		Reflinks:   true, // Block cloning since OpenZFS 2.2
	}

	// Try to get compression property from ZFS
	dataset, err := findZFSDataset(path)
	if err != nil {
		info.Enabled = false
		info.Algorithm = "unknown"
		info.Recommended = "ZFS detected but cannot query properties. May support compression + block cloning."
		return info, nil
	}

	// Query compression property
	cmd := exec.Command("zfs", "get", "-H", "-o", "value", "compression", dataset)
	output, err := cmd.Output()
	if err != nil {
		info.Enabled = false
		info.Algorithm = "unknown"
		info.Recommended = "ZFS detected but cannot query compression. May need elevated permissions."
		return info, nil
	}

	algo := strings.TrimSpace(string(output))
	if algo == "off" || algo == "" {
		info.Enabled = false
		info.Algorithm = "none"
		info.Recommended = "ZFS without compression. Enable with: zfs set compression=lz4 <dataset>"
	} else {
		info.Enabled = true
		info.Algorithm = algo
		info.Recommended = fmt.Sprintf("GOOD: ZFS with %s compression + block cloning! Store uncompressed.", algo)
	}

	return info, nil
}

// findZFSDataset finds the ZFS dataset for a given path
func findZFSDataset(path string) (string, error) {
	// Use df to find the ZFS dataset
	cmd := exec.Command("df", "-T", path)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("unexpected df output")
	}

	// Parse df output: Filesystem Type Size Used Avail Use% Mounted
	fields := strings.Fields(lines[1])
	if len(fields) < 1 {
		return "", fmt.Errorf("cannot parse df output")
	}

	return fields[0], nil
}

// CompressionStrategyRecommendation recommends a compression strategy
func CompressionStrategyRecommendation(info *FSCompressionInfo) string {
	if !info.Supported {
		return "app-level-gzip"
	}

	if info.Enabled && info.Reflinks {
		return "none" // Perfect case: FS compression + reflinks
	}

	if info.Reflinks {
		return "none-or-app" // Trade-off: speed vs disk space
	}

	return "app-level-gzip"
}

// Example usage and testing
func ExampleDetection() {
	fmt.Println("=== RCC Filesystem Compression Detection ===\n")

	paths := []string{
		"/home/user/.robocorp",     // Typical hololib location
		"/tmp",                      // Often tmpfs
		"/",                         // Root filesystem
	}

	for _, path := range paths {
		fmt.Printf("Analyzing: %s\n", path)
		info, err := DetectFilesystemCompression(path)
		if err != nil {
			fmt.Printf("  ERROR: %v\n\n", err)
			continue
		}

		fmt.Printf("  Filesystem: %s (%s)\n", info.Type, info.Filesystem)
		fmt.Printf("  Compression supported: %v\n", info.Supported)
		if info.Supported {
			fmt.Printf("  Compression enabled: %v\n", info.Enabled)
			fmt.Printf("  Algorithm: %s\n", info.Algorithm)
		}
		fmt.Printf("  Reflink support: %v\n", info.Reflinks)
		fmt.Printf("  Recommendation: %s\n", info.Recommended)
		fmt.Printf("  Strategy: %s\n\n", CompressionStrategyRecommendation(info))
	}
}

// Integration point for RCC
func ShouldCompressInHololib(hololibPath string) bool {
	info, err := DetectFilesystemCompression(hololibPath)
	if err != nil {
		// On error, be conservative and compress
		return true
	}

	// If FS supports compression + reflinks, don't compress at app level
	if info.Enabled && info.Reflinks {
		return false
	}

	// If FS supports reflinks but not compression, it's a trade-off
	// User can override with RCC_COMPRESSION_STRATEGY env var
	if info.Reflinks {
		// Check for override
		strategy := os.Getenv("RCC_COMPRESSION_STRATEGY")
		switch strategy {
		case "none", "disable", "off":
			return false
		case "app", "gzip", "enable":
			return true
		default:
			// Default for reflink filesystems: prioritize speed
			return false
		}
	}

	// Default: compress at app level
	return true
}

// GetCompressionInfo returns human-readable compression info
func GetCompressionInfo(hololibPath string) string {
	info, err := DetectFilesystemCompression(hololibPath)
	if err != nil {
		return fmt.Sprintf("Cannot detect filesystem: %v", err)
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Filesystem: %s\n", info.Type))

	if info.Supported {
		result.WriteString(fmt.Sprintf("Compression: %s ", info.Algorithm))
		if info.Enabled {
			result.WriteString("(enabled)\n")
		} else {
			result.WriteString("(available but not enabled)\n")
		}
	} else {
		result.WriteString("Compression: Not supported by filesystem\n")
	}

	if info.Reflinks {
		result.WriteString("Reflinks: Supported\n")
	} else {
		result.WriteString("Reflinks: Not supported\n")
	}

	result.WriteString(fmt.Sprintf("\nRecommendation: %s\n", info.Recommended))

	return result.String()
}

// Could be added to rcc CLI:
//
// $ rcc holotree info --compression
// Filesystem: Btrfs
// Compression: zstd:3 (enabled)
// Reflinks: Supported
//
// Recommendation: IDEAL - Store uncompressed, FS compresses with zstd:3 + reflinks work!
// Current setting: Application-level compression disabled (using filesystem compression)
//
// Performance expectations:
//   - Disk usage: Similar to gzipped (~2-3x compression)
//   - Environment restore: 20-40x faster (instant reflinks)
//   - Decompression overhead: Minimal (~400-700 MB/s zstd)
