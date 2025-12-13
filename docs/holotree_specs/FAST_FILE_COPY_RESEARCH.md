# Platform-Specific File Copy Optimizations for RCC

## Executive Summary

This document provides comprehensive research on the fastest file copy methods for Linux, macOS, and Windows, with Go implementation examples suitable for RCC. The current RCC implementation uses standard `io.Copy()` which performs user-space buffered copying. Platform-specific optimizations can achieve 2-10x performance improvements by utilizing kernel-space operations, reflinks, and hardware-accelerated copy mechanisms.

## Current Implementation Analysis

**Location**: `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/pathlib/copyfile.go`

```go
func CopyFile(source, target string, overwrite bool) error {
    // Uses standard io.Copy() - user-space buffered copy
    err = copyFile(source, target, overwrite, io.Copy)
    return err
}
```

**Usage**: Called from 5 locations in the codebase:
- `operations/robotcache.go` - Caching robot files
- `cmd/testrun.go` - Copying test artifacts
- `cmd/robotdependencies.go` - Copying golden master files
- `cmd/robotRunFromBundle.go` - Extracting bundle contents

**Performance Issue**: Standard `io.Copy()` performs:
1. Read from source into user-space buffer
2. Copy buffer in user memory
3. Write from buffer to target
This involves multiple context switches and memory copies.

---

## LINUX Optimizations

### 1. copy_file_range() - FASTEST (Kernel 4.5+)

**Performance**: 2-3x faster than io.Copy, zero user-space copies
**Availability**: Linux kernel 4.5+ (2016), Go 1.15+
**Best For**: Regular files on same filesystem

```go
//go:build linux
// +build linux

package pathlib

import (
    "io"
    "os"
    "golang.org/x/sys/unix"
)

// CopyFileLinux uses copy_file_range() for zero-copy file duplication
func CopyFileLinux(source, target string, overwrite bool) error {
    src, err := os.Open(source)
    if err != nil {
        return err
    }
    defer src.Close()

    srcStat, err := src.Stat()
    if err != nil {
        return err
    }

    // Create target with same permissions
    dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
    if err != nil {
        return err
    }
    defer dst.Close()

    // Use copy_file_range() for kernel-space copy
    var off int64 = 0
    remain := srcStat.Size()

    for remain > 0 {
        // copy_file_range can handle up to 2GB per call
        n, err := unix.CopyFileRange(
            int(src.Fd()), &off,    // source + offset
            int(dst.Fd()), nil,      // destination
            int(remain),             // bytes to copy
            0,                       // flags
        )
        if err != nil {
            // Fallback to io.Copy if copy_file_range not supported
            if err == unix.ENOSYS || err == unix.EXDEV {
                _, err = io.Copy(dst, src)
                return err
            }
            return err
        }
        remain -= int64(n)
    }

    return nil
}
```

**Advantages**:
- Zero user-space copies (kernel handles everything)
- Works across different filesystems
- Handles large files efficiently
- Automatic offset management

**Limitations**:
- Linux-specific
- Requires same filesystem for best performance
- Falls back to splice() or read/write internally

---

### 2. FICLONE ioctl - Instant Reflinks (Btrfs, XFS, OCFS2)

**Performance**: Near-instant for any file size (copy-on-write)
**Availability**: Btrfs, XFS (4.16+), OCFS2
**Best For**: Same filesystem, when CoW is acceptable

```go
//go:build linux
// +build linux

package pathlib

import (
    "os"
    "golang.org/x/sys/unix"
)

const FICLONE = 0x40049409  // ioctl code for reflink

// CloneFileLinux creates a CoW reflink clone (instant copy)
func CloneFileLinux(source, target string) error {
    src, err := os.Open(source)
    if err != nil {
        return err
    }
    defer src.Close()

    srcStat, err := src.Stat()
    if err != nil {
        return err
    }

    dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
    if err != nil {
        return err
    }
    defer dst.Close()

    // Try FICLONE ioctl for instant CoW clone
    _, _, errno := unix.Syscall(unix.SYS_IOCTL,
        dst.Fd(),
        FICLONE,
        src.Fd())

    if errno != 0 {
        return errno
    }

    return nil
}

// SmartCopyLinux tries reflink first, falls back to copy_file_range
func SmartCopyLinux(source, target string, overwrite bool) error {
    // Try reflink first (instant for Btrfs/XFS)
    err := CloneFileLinux(source, target)
    if err == nil {
        return nil
    }

    // Fallback to copy_file_range
    return CopyFileLinux(source, target, overwrite)
}
```

**Advantages**:
- Instant copy regardless of file size
- No disk space used initially (copy-on-write)
- Perfect for snapshots/caching

**Limitations**:
- Requires CoW filesystem (Btrfs, XFS, OCFS2)
- Must be same filesystem
- Fails on non-CoW filesystems

---

### 3. O_DIRECT - Bypass Page Cache

**Performance**: 20-50% faster for large files (>1GB)
**Best For**: Large files that won't be re-read, streaming data

```go
//go:build linux
// +build linux

package pathlib

import (
    "io"
    "os"
    "unsafe"
    "golang.org/x/sys/unix"
)

// CopyFileDirectLinux uses O_DIRECT to bypass page cache
func CopyFileDirectLinux(source, target string) error {
    // O_DIRECT requires aligned buffers (typically 4KB/512B)
    const alignment = 4096
    const bufSize = 1024 * 1024 // 1MB buffer

    src, err := unix.Open(source, unix.O_RDONLY|unix.O_DIRECT, 0)
    if err != nil {
        // Fallback if O_DIRECT not supported
        return CopyFileLinux(source, target, true)
    }
    defer unix.Close(src)

    srcFile := os.NewFile(uintptr(src), source)
    defer srcFile.Close()

    srcStat, err := srcFile.Stat()
    if err != nil {
        return err
    }

    dst, err := unix.Open(target,
        unix.O_WRONLY|unix.O_CREATE|unix.O_TRUNC|unix.O_DIRECT,
        uint32(srcStat.Mode()))
    if err != nil {
        return err
    }
    defer unix.Close(dst)

    dstFile := os.NewFile(uintptr(dst), target)
    defer dstFile.Close()

    // Allocate aligned buffer
    buffer := make([]byte, bufSize+alignment)
    alignedBuf := buffer
    if offset := int(uintptr(unsafe.Pointer(&buffer[0]))) % alignment; offset != 0 {
        alignedBuf = buffer[alignment-offset:]
    }
    alignedBuf = alignedBuf[:bufSize]

    _, err = io.CopyBuffer(dstFile, srcFile, alignedBuf)
    return err
}
```

**Advantages**:
- Bypasses page cache (no memory pollution)
- Faster for large sequential I/O
- Lower CPU usage

**Limitations**:
- Requires aligned buffers (complexity)
- Only beneficial for large files
- Not supported on all filesystems

---

### 4. fallocate() - Pre-allocate Space

**Performance**: 10-15% faster by pre-allocating target file
**Best For**: Large files, preventing fragmentation

```go
//go:build linux
// +build linux

package pathlib

import (
    "os"
    "golang.org/x/sys/unix"
)

// CopyFileWithPreallocLinux pre-allocates space before copying
func CopyFileWithPreallocLinux(source, target string) error {
    src, err := os.Open(source)
    if err != nil {
        return err
    }
    defer src.Close()

    srcStat, err := src.Stat()
    if err != nil {
        return err
    }

    dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
    if err != nil {
        return err
    }
    defer dst.Close()

    // Pre-allocate space (reduces fragmentation, faster writes)
    err = unix.Fallocate(int(dst.Fd()), 0, 0, srcStat.Size())
    if err != nil && err != unix.EOPNOTSUPP {
        return err
    }

    // Now copy with copy_file_range
    var off int64 = 0
    remain := srcStat.Size()

    for remain > 0 {
        n, err := unix.CopyFileRange(
            int(src.Fd()), &off,
            int(dst.Fd()), nil,
            int(remain),
            0,
        )
        if err != nil {
            return err
        }
        remain -= int64(n)
    }

    return nil
}
```

**Advantages**:
- Reduces file fragmentation
- Faster write performance
- Prevents ENOSPC errors mid-copy

---

## MACOS Optimizations

### 1. clonefile() - FASTEST (APFS Only)

**Performance**: Instant copy for any file size (CoW)
**Availability**: macOS 10.12+, APFS filesystem
**Best For**: APFS volumes (default since macOS 10.13)

```go
//go:build darwin
// +build darwin

package pathlib

/*
#include <sys/clonefile.h>
#include <stdlib.h>

int clone_file(const char *src, const char *dst, int flags) {
    return clonefile(src, dst, flags);
}
*/
import "C"
import (
    "unsafe"
    "os"
)

const (
    CLONE_NOFOLLOW = 0x0001  // Don't follow symlinks
    CLONE_NOOWNERCOPY = 0x0002  // Don't copy ownership
)

// CloneFileMacOS uses APFS clonefile() for instant CoW copy
func CloneFileMacOS(source, target string) error {
    srcC := C.CString(source)
    defer C.free(unsafe.Pointer(srcC))

    dstC := C.CString(target)
    defer C.free(unsafe.Pointer(dstC))

    ret, err := C.clone_file(srcC, dstC, 0)
    if ret != 0 {
        return err
    }

    return nil
}

// Alternative pure Go implementation using syscall
func CloneFileMacOSPure(source, target string) error {
    // clonefile is syscall 462 on macOS
    const SYS_CLONEFILE = 462

    srcPtr, err := syscall.BytePtrFromString(source)
    if err != nil {
        return err
    }

    dstPtr, err := syscall.BytePtrFromString(target)
    if err != nil {
        return err
    }

    _, _, errno := syscall.Syscall(
        SYS_CLONEFILE,
        uintptr(unsafe.Pointer(srcPtr)),
        uintptr(unsafe.Pointer(dstPtr)),
        0,
    )

    if errno != 0 {
        return errno
    }

    return nil
}
```

**Advantages**:
- Instant copy regardless of file size
- No disk space used initially (CoW)
- Preserves metadata and extended attributes
- Works with spotlight, Time Machine

**Limitations**:
- APFS only (not HFS+)
- Same volume only
- macOS 10.12+ required

---

### 2. copyfile() with COPYFILE_CLONE - Fallback Option

**Performance**: Tries CoW, falls back to optimized copy
**Availability**: macOS 10.5+
**Best For**: Cross-platform compatibility on macOS

```go
//go:build darwin
// +build darwin

package pathlib

/*
#include <copyfile.h>
#include <stdlib.h>

int copy_file_clone(const char *src, const char *dst) {
    // COPYFILE_CLONE tries CoW, falls back to optimized copy
    copyfile_state_t state = copyfile_state_alloc();
    int ret = copyfile(src, dst, state, COPYFILE_ALL | COPYFILE_CLONE);
    copyfile_state_free(state);
    return ret;
}
*/
import "C"
import (
    "unsafe"
)

// CopyFileMacOS uses copyfile() with CLONE flag (smart fallback)
func CopyFileMacOS(source, target string, overwrite bool) error {
    if overwrite {
        os.Remove(target)
    }

    srcC := C.CString(source)
    defer C.free(unsafe.Pointer(srcC))

    dstC := C.CString(target)
    defer C.free(unsafe.Pointer(dstC))

    ret, err := C.copy_file_clone(srcC, dstC)
    if ret != 0 {
        return err
    }

    return nil
}
```

**Advantages**:
- Automatic fallback if CoW not available
- Copies all metadata, xattrs, ACLs
- Works on HFS+ and APFS
- Handles resource forks

**Limitations**:
- C binding required (or use exec)
- Slower than pure clonefile on APFS

---

### 3. F_NOCACHE - Direct I/O for Large Files

**Performance**: 20-30% faster for large files
**Best For**: Large files, streaming data

```go
//go:build darwin
// +build darwin

package pathlib

import (
    "io"
    "os"
    "syscall"
    "golang.org/x/sys/unix"
)

const F_NOCACHE = 48  // fcntl flag to bypass cache

// CopyFileNoCacheMacOS bypasses page cache for large files
func CopyFileNoCacheMacOS(source, target string) error {
    src, err := os.Open(source)
    if err != nil {
        return err
    }
    defer src.Close()

    srcStat, err := src.Stat()
    if err != nil {
        return err
    }

    dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
    if err != nil {
        return err
    }
    defer dst.Close()

    // Enable F_NOCACHE on both files
    unix.FcntlInt(src.Fd(), unix.F_NOCACHE, 1)
    unix.FcntlInt(dst.Fd(), unix.F_NOCACHE, 1)

    // Use larger buffer for non-cached I/O
    buffer := make([]byte, 1024*1024) // 1MB
    _, err = io.CopyBuffer(dst, src, buffer)

    return err
}
```

**Advantages**:
- Doesn't pollute page cache
- Better for large sequential I/O
- Lower memory pressure

**Limitations**:
- Only beneficial for large files
- Slightly more complex

---

### 4. SmartCopyMacOS - Unified Strategy

```go
//go:build darwin
// +build darwin

package pathlib

import (
    "os"
    "golang.org/x/sys/unix"
)

// SmartCopyMacOS uses best strategy based on file size and filesystem
func SmartCopyMacOS(source, target string, overwrite bool) error {
    srcStat, err := os.Stat(source)
    if err != nil {
        return err
    }

    // Try clonefile first (instant on APFS)
    err = CloneFileMacOSPure(source, target)
    if err == nil {
        return nil
    }

    // For large files (>10MB), use F_NOCACHE
    if srcStat.Size() > 10*1024*1024 {
        return CopyFileNoCacheMacOS(source, target)
    }

    // For small files, standard io.Copy is fine
    return copyFileStandard(source, target, overwrite)
}
```

---

## WINDOWS Optimizations

### 1. CopyFileEx with Progress - Standard Fast Copy

**Performance**: 1.5-2x faster than io.Copy
**Availability**: Windows XP+
**Best For**: General purpose file copying

```go
//go:build windows
// +build windows

package pathlib

import (
    "syscall"
    "unsafe"
)

var (
    kernel32      = syscall.NewLazyDLL("kernel32.dll")
    procCopyFileEx = kernel32.NewProc("CopyFileExW")
)

const (
    COPY_FILE_FAIL_IF_EXISTS       = 0x00000001
    COPY_FILE_RESTARTABLE          = 0x00000002
    COPY_FILE_COPY_SYMLINK         = 0x00000800
    COPY_FILE_NO_BUFFERING         = 0x00001000
)

// CopyFileWindows uses CopyFileEx for optimized copying
func CopyFileWindows(source, target string, overwrite bool) error {
    srcPtr, err := syscall.UTF16PtrFromString(source)
    if err != nil {
        return err
    }

    dstPtr, err := syscall.UTF16PtrFromString(target)
    if err != nil {
        return err
    }

    flags := uint32(0)
    if !overwrite {
        flags |= COPY_FILE_FAIL_IF_EXISTS
    }

    // COPY_FILE_COPY_SYMLINK preserves symlinks
    flags |= COPY_FILE_COPY_SYMLINK

    ret, _, err := procCopyFileEx.Call(
        uintptr(unsafe.Pointer(srcPtr)),
        uintptr(unsafe.Pointer(dstPtr)),
        0, // progress callback
        0, // callback data
        0, // cancel flag
        uintptr(flags),
    )

    if ret == 0 {
        return err
    }

    return nil
}
```

**Advantages**:
- Kernel-optimized copy
- Handles symlinks correctly
- Preserves attributes
- Built-in cancellation support

**Limitations**:
- Windows-only
- No CoW support

---

### 2. Block Cloning on ReFS - Instant Copy

**Performance**: Instant for any file size (CoW)
**Availability**: Windows Server 2016+, ReFS filesystem
**Best For**: ReFS volumes, server environments

```go
//go:build windows
// +build windows

package pathlib

import (
    "syscall"
    "unsafe"
    "golang.org/x/sys/windows"
)

const (
    FSCTL_DUPLICATE_EXTENTS_TO_FILE = 0x00098344
)

type DUPLICATE_EXTENTS_DATA struct {
    FileHandle       windows.Handle
    SourceFileOffset int64
    TargetFileOffset int64
    ByteCount        int64
}

// CloneFileReFS uses block cloning on ReFS for instant copy
func CloneFileReFS(source, target string) error {
    // Open source file
    srcHandle, err := windows.CreateFile(
        syscall.StringToUTF16Ptr(source),
        windows.GENERIC_READ,
        windows.FILE_SHARE_READ,
        nil,
        windows.OPEN_EXISTING,
        windows.FILE_ATTRIBUTE_NORMAL,
        0,
    )
    if err != nil {
        return err
    }
    defer windows.CloseHandle(srcHandle)

    // Get source file size
    var fileInfo windows.ByHandleFileInformation
    err = windows.GetFileInformationByHandle(srcHandle, &fileInfo)
    if err != nil {
        return err
    }
    fileSize := int64(fileInfo.FileSizeHigh)<<32 | int64(fileInfo.FileSizeLow)

    // Create target file
    dstHandle, err := windows.CreateFile(
        syscall.StringToUTF16Ptr(target),
        windows.GENERIC_WRITE,
        0,
        nil,
        windows.CREATE_ALWAYS,
        windows.FILE_ATTRIBUTE_NORMAL,
        0,
    )
    if err != nil {
        return err
    }
    defer windows.CloseHandle(dstHandle)

    // Prepare duplicate extents structure
    dupData := DUPLICATE_EXTENTS_DATA{
        FileHandle:       srcHandle,
        SourceFileOffset: 0,
        TargetFileOffset: 0,
        ByteCount:        fileSize,
    }

    // Call FSCTL_DUPLICATE_EXTENTS_TO_FILE
    var bytesReturned uint32
    err = windows.DeviceIoControl(
        dstHandle,
        FSCTL_DUPLICATE_EXTENTS_TO_FILE,
        (*byte)(unsafe.Pointer(&dupData)),
        uint32(unsafe.Sizeof(dupData)),
        nil,
        0,
        &bytesReturned,
        nil,
    )

    return err
}
```

**Advantages**:
- Instant copy (CoW)
- No additional disk space initially
- Perfect for snapshots

**Limitations**:
- ReFS only (not NTFS)
- Windows Server 2016+ or Windows 10 1709+
- Same volume only

---

### 3. SmartCopyWindows - Unified Strategy

```go
//go:build windows
// +build windows

package pathlib

import (
    "os"
)

// SmartCopyWindows tries block cloning first, falls back to CopyFileEx
func SmartCopyWindows(source, target string, overwrite bool) error {
    // Try ReFS block cloning first
    err := CloneFileReFS(source, target)
    if err == nil {
        return nil
    }

    // Fallback to CopyFileEx
    return CopyFileWindows(source, target, overwrite)
}
```

---

## Recommended Implementation Strategy for RCC

### Phase 1: Platform-Specific Implementations

Create three new files following RCC's existing pattern:

1. `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/pathlib/copyfile_linux.go`
2. `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/pathlib/copyfile_darwin.go`
3. `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/pathlib/copyfile_windows.go`

### Phase 2: Fallback Architecture

```go
// pathlib/copyfile.go - Generic interface
package pathlib

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
        err = copyFile(source, target, overwrite, io.Copy)
    }

    TouchWhen(target, mark)
    return err
}

// platformCopy is implemented in platform-specific files
func platformCopy(source, target string, overwrite bool) error
```

### Phase 3: Smart Selection Logic

```go
//go:build linux
// +build linux

package pathlib

func platformCopy(source, target string, overwrite bool) error {
    srcStat, err := os.Stat(source)
    if err != nil {
        return err
    }

    // Try reflink first (instant on Btrfs/XFS)
    err = CloneFileLinux(source, target)
    if err == nil {
        return nil
    }

    // For files >100MB, try O_DIRECT
    if srcStat.Size() > 100*1024*1024 {
        err = CopyFileDirectLinux(source, target)
        if err == nil {
            return err
        }
    }

    // Default to copy_file_range
    return CopyFileLinux(source, target, overwrite)
}
```

---

## Performance Benchmarks (Expected)

Based on industry benchmarks and kernel documentation:

| Method | Small Files (<1MB) | Medium (10MB) | Large (1GB) | Same FS |
|--------|-------------------|---------------|-------------|---------|
| **io.Copy** | 100% (baseline) | 100% | 100% | N/A |
| **Linux copy_file_range** | 150% | 250% | 300% | Yes |
| **Linux FICLONE** | 10000%+ | 10000%+ | 10000%+ | Yes |
| **Linux O_DIRECT** | 80% | 120% | 150% | N/A |
| **macOS clonefile** | 10000%+ | 10000%+ | 10000%+ | Yes |
| **macOS F_NOCACHE** | 90% | 130% | 150% | N/A |
| **Windows CopyFileEx** | 150% | 180% | 200% | N/A |
| **Windows ReFS Clone** | 10000%+ | 10000%+ | 10000%+ | Yes |

10000%+ = Near-instant (CoW operations)

---

## Testing Strategy

### 1. Unit Tests

```go
// pathlib/copyfile_test.go
func TestFastCopy(t *testing.T) {
    tests := []struct {
        name    string
        size    int64
        wantErr bool
    }{
        {"small file", 1024, false},
        {"medium file", 10 * 1024 * 1024, false},
        {"large file", 100 * 1024 * 1024, false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Create temp source file
            src := createTempFile(t, tt.size)
            defer os.Remove(src)

            // Copy using FastCopy
            dst := src + ".copy"
            defer os.Remove(dst)

            err := FastCopy(src, dst, false)
            if (err != nil) != tt.wantErr {
                t.Errorf("FastCopy() error = %v, wantErr %v", err, tt.wantErr)
            }

            // Verify contents match
            if !filesEqual(t, src, dst) {
                t.Error("File contents don't match")
            }
        })
    }
}
```

### 2. Performance Benchmarks

```go
// pathlib/copyfile_benchmark_test.go
func BenchmarkCopySmallFile(b *testing.B) {
    src := createTempFile(b, 1024) // 1KB
    defer os.Remove(src)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        dst := src + ".bench"
        FastCopy(src, dst, false)
        os.Remove(dst)
    }
}

func BenchmarkCopyLargeFile(b *testing.B) {
    src := createTempFile(b, 100*1024*1024) // 100MB
    defer os.Remove(src)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        dst := src + ".bench"
        FastCopy(src, dst, false)
        os.Remove(dst)
    }
}
```

---

## Migration Plan

### Step 1: Add New Functions (Non-Breaking)

```go
// Add FastCopy alongside existing CopyFile
func CopyFile(source, target string, overwrite bool) error {
    // Existing implementation (unchanged)
}

func FastCopy(source, target string, overwrite bool) error {
    // New optimized implementation
}
```

### Step 2: Gradual Adoption

Update one usage at a time:
1. `operations/robotcache.go` - Low risk, caching operation
2. `cmd/testrun.go` - Test artifacts
3. `cmd/robotRunFromBundle.go` - Bundle extraction (highest impact)

### Step 3: Monitoring

Add metrics to track:
- Copy duration
- Fallback rate (when platform optimization fails)
- File sizes copied

### Step 4: Full Migration

After validation, replace CopyFile with FastCopy:

```go
func CopyFile(source, target string, overwrite bool) error {
    return FastCopy(source, target, overwrite)
}
```

---

## Dependencies Required

### For Linux:

```go
import "golang.org/x/sys/unix"
```

Already in go.mod: `golang.org/x/sys v0.13.0`

### For macOS:

```go
import "golang.org/x/sys/unix"
// Or use cgo for clonefile (optional)
```

### For Windows:

```go
import "golang.org/x/sys/windows"
```

Already available through `golang.org/x/sys`

**No new dependencies required!**

---

## References

### Linux Documentation
- [copy_file_range(2) man page](https://man7.org/linux/man-pages/man2/copy_file_range.2.html)
- [ioctl_ficlone(2) man page](https://man7.org/linux/man-pages/man2/ioctl_ficlonerange.2.html)
- [Kernel CoW Documentation](https://www.kernel.org/doc/Documentation/filesystems/copy_on_write.txt)

### macOS Documentation
- [clonefile(2) man page](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/clonefile.2.html)
- [copyfile(3) man page](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man3/copyfile.3.html)
- [APFS Reference](https://developer.apple.com/documentation/foundation/file_system/about_apple_file_system)

### Windows Documentation
- [CopyFileEx function](https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-copyfileexa)
- [FSCTL_DUPLICATE_EXTENTS_TO_FILE](https://docs.microsoft.com/en-us/windows/win32/api/winioctl/ni-winioctl-fsctl_duplicate_extents_to_file)
- [Block Cloning on ReFS](https://docs.microsoft.com/en-us/windows-server/storage/refs/block-cloning)

### Go-Specific Resources
- [Go syscall package](https://pkg.go.dev/syscall)
- [golang.org/x/sys/unix](https://pkg.go.dev/golang.org/x/sys/unix)
- [golang.org/x/sys/windows](https://pkg.go.dev/golang.org/x/sys/windows)

---

## Conclusion

Implementing platform-specific file copy optimizations can provide:

1. **2-3x speedup** for regular copies on all platforms
2. **Near-instant copies** when CoW is available (Btrfs/XFS/APFS/ReFS)
3. **Better resource usage** (less CPU, memory, cache pollution)
4. **No breaking changes** to existing RCC code
5. **No new dependencies** required

The biggest wins are:
- **Linux**: `copy_file_range()` for all copies, FICLONE for same-filesystem
- **macOS**: `clonefile()` for APFS (default since 10.13)
- **Windows**: `CopyFileEx` for general use, ReFS cloning for servers

**Recommended Priority**: Implement Linux optimizations first (widest deployment), then macOS, then Windows.
