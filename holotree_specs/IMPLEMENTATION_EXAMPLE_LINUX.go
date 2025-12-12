// Example implementation for pathlib/copyfile_linux.go
// This is a REFERENCE implementation - not production ready
//
//go:build linux
// +build linux

package pathlib

import (
	"io"
	"os"
	"golang.org/x/sys/unix"

	"github.com/joshyorko/rcc/common"
)

const FICLONE = 0x40049409 // ioctl code for reflink

// platformCopy is called by FastCopy() and uses Linux-specific optimizations
func platformCopy(source, target string, overwrite bool) error {
	if overwrite && Exists(target) {
		if err := os.Remove(target); err != nil {
			return err
		}
	}

	// Strategy 1: Try FICLONE first (instant CoW on Btrfs/XFS)
	err := cloneFileLinux(source, target)
	if err == nil {
		common.Debug("Fast copy: used FICLONE reflink for %q -> %q", source, target)
		return nil
	}

	// Strategy 2: Try copy_file_range (kernel-space copy, 2-3x faster)
	err = copyFileRangeLinux(source, target)
	if err == nil {
		common.Debug("Fast copy: used copy_file_range for %q -> %q", source, target)
		return nil
	}

	// Strategy 3: Fallback to standard io.Copy
	common.Debug("Fast copy: falling back to io.Copy for %q -> %q (reason: %v)", source, target, err)
	return nil // Return nil to trigger fallback in caller
}

// cloneFileLinux creates a CoW reflink clone (instant copy)
// Only works on Btrfs, XFS (4.16+), and OCFS2 filesystems
func cloneFileLinux(source, target string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	srcStat, err := src.Stat()
	if err != nil {
		return err
	}

	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, srcStat.Mode())
	if err != nil {
		return err
	}

	// Try FICLONE ioctl - this will be instant if successful
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		dst.Fd(),
		FICLONE,
		src.Fd(),
	)

	if errno != 0 {
		dst.Close()
		os.Remove(target)
		return errno
	}

	return dst.Close()
}

// copyFileRangeLinux uses copy_file_range() syscall for fast kernel-space copy
// Available since Linux 4.5 (2016)
func copyFileRangeLinux(source, target string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	srcStat, err := src.Stat()
	if err != nil {
		return err
	}

	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, srcStat.Mode())
	if err != nil {
		return err
	}

	var success bool
	defer func() {
		dst.Close()
		if !success {
			os.Remove(target)
		}
	}()

	// copy_file_range can copy entire file in kernel space
	var offset int64 = 0
	remaining := srcStat.Size()

	for remaining > 0 {
		// copy_file_range handles up to 2GB per call
		n, err := unix.CopyFileRange(
			int(src.Fd()), &offset, // source + offset
			int(dst.Fd()), nil,      // destination (auto-managed offset)
			int(remaining),          // bytes to copy
			0,                       // flags
		)
		if err != nil {
			// If copy_file_range not supported, fall back
			if err == unix.ENOSYS || err == unix.EXDEV {
				return err
			}
			return err
		}
		remaining -= int64(n)
	}

	success = true
	return nil
}

// FastCopy uses platform-specific optimizations with automatic fallback
func FastCopy(source, target string, overwrite bool) error {
	// Preserve modification time
	mark, err := Modtime(source)
	if err != nil {
		return err
	}

	// Try platform-specific fast copy
	err = platformCopy(source, target, overwrite)
	if err != nil {
		// Fallback to standard io.Copy
		common.Debug("Platform copy failed, using fallback: %v", err)
		err = copyFile(source, target, overwrite, io.Copy)
	}

	// Restore modification time
	if err == nil {
		TouchWhen(target, mark)
	}

	return err
}

// Benchmark test helper
func BenchmarkCopySmall(b *testing.B) {
	// Create 1MB test file
	src := createTestFile(1024 * 1024)
	defer os.Remove(src)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := src + ".bench"
		FastCopy(src, dst, false)
		os.Remove(dst)
	}
}

func BenchmarkCopyLarge(b *testing.B) {
	// Create 100MB test file
	src := createTestFile(100 * 1024 * 1024)
	defer os.Remove(src)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := src + ".bench"
		FastCopy(src, dst, false)
		os.Remove(dst)
	}
}

func createTestFile(size int64) string {
	f, _ := os.CreateTemp("", "copytest-*.bin")
	defer f.Close()
	f.Truncate(size)
	return f.Name()
}
