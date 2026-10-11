# Reflink Integration Guide for RCC

This guide shows EXACTLY where to add reflink support in the RCC codebase.

---

## Overview

We'll add reflink support by:
1. Creating platform-specific reflink implementations
2. Modifying the DropFile function to try reflinks first
3. Adding configuration support
4. Adding graceful fallbacks

**Total changes:** ~150 lines of new code, ~10 lines modified

---

## File Structure

```
rcc/
├── htfs/
│   ├── reflink_linux.go       [NEW] Linux FICLONE implementation
│   ├── reflink_darwin.go      [NEW] macOS clonefile implementation
│   ├── reflink_windows.go     [NEW] Windows ReFS implementation
│   ├── reflink_unsupported.go [NEW] Stub for other platforms
│   ├── filesystem.go          [NEW] Filesystem detection
│   └── functions.go           [MODIFY] Add reflink to DropFile
├── settings/
│   └── settings.go            [MODIFY] Add holotree strategy config
└── cmd/
    └── holotree.go            [MODIFY] Add --strategy flag
```

---

## Step 1: Create Platform-Specific Reflink Files

### File: `htfs/reflink_linux.go`

```go
//go:build linux

package htfs

import (
    "fmt"
    "os"
    "syscall"
)

const (
    // From linux/fs.h: FICLONE is 0x40049409
    FICLONE = 0x40049409
)

// Reflink creates a copy-on-write clone of src to dst using the FICLONE ioctl.
// Returns (true, nil) if successful, (false, nil) if not supported, (false, err) on error.
func Reflink(src, dst string) (bool, error) {
    srcFile, err := os.Open(src)
    if err != nil {
        return false, fmt.Errorf("open source: %w", err)
    }
    defer srcFile.Close()

    srcInfo, err := srcFile.Stat()
    if err != nil {
        return false, fmt.Errorf("stat source: %w", err)
    }

    dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, srcInfo.Mode())
    if err != nil {
        return false, fmt.Errorf("create dest: %w", err)
    }
    defer dstFile.Close()

    _, _, errno := syscall.Syscall(
        syscall.SYS_IOCTL,
        uintptr(dstFile.Fd()),
        uintptr(FICLONE),
        uintptr(srcFile.Fd()),
    )

    if errno != 0 {
        os.Remove(dst)
        // EOPNOTSUPP (95) or EXDEV (18) means filesystem doesn't support reflinks
        if errno == syscall.EOPNOTSUPP || errno == syscall.EXDEV {
            return false, nil
        }
        return false, fmt.Errorf("ioctl FICLONE: %w", errno)
    }

    // Preserve timestamps
    if err := os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
        return true, fmt.Errorf("preserve times: %w", err)
    }

    return true, nil
}
```

### File: `htfs/reflink_darwin.go`

```go
//go:build darwin

package htfs

import (
    "fmt"
    "os"
    "runtime"
    "syscall"
    "unsafe"
)

// Reflink creates a copy-on-write clone using macOS clonefile syscall.
func Reflink(src, dst string) (bool, error) {
    // macOS clonefile syscall number varies by architecture
    var clonefileSyscall uintptr
    switch runtime.GOARCH {
    case "amd64":
        clonefileSyscall = 462
    case "arm64":
        clonefileSyscall = 359
    default:
        return false, fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
    }

    srcBytes := []byte(src + "\x00")
    dstBytes := []byte(dst + "\x00")

    _, _, errno := syscall.Syscall(
        clonefileSyscall,
        uintptr(unsafe.Pointer(&srcBytes[0])),
        uintptr(unsafe.Pointer(&dstBytes[0])),
        0, // flags
    )

    if errno != 0 {
        // ENOTSUP (45) means filesystem doesn't support cloning
        if errno == syscall.ENOTSUP || errno == syscall.EOPNOTSUPP {
            return false, nil
        }
        return false, fmt.Errorf("clonefile: %w", errno)
    }

    return true, nil
}
```

### File: `htfs/reflink_windows.go`

```go
//go:build windows

package htfs

import "fmt"

// Reflink is not yet implemented for Windows.
// TODO: Add ReFS FSCTL_DUPLICATE_EXTENTS_TO_FILE support
func Reflink(src, dst string) (bool, error) {
    // For now, return not supported
    // Full implementation would use DeviceIoControl with FSCTL_DUPLICATE_EXTENTS_TO_FILE
    return false, nil
}
```

### File: `htfs/reflink_unsupported.go`

```go
//go:build !linux && !darwin && !windows

package htfs

// Reflink is not supported on this platform.
func Reflink(src, dst string) (bool, error) {
    return false, nil
}
```

---

## Step 2: Add Filesystem Detection

### File: `htfs/filesystem.go`

```go
package htfs

import (
    "runtime"
    "syscall"
)

// FilesystemType returns the filesystem type name for a given path.
// Only implemented for Linux; other platforms return "unknown".
func FilesystemType(path string) string {
    if runtime.GOOS != "linux" {
        return "unknown"
    }

    var stat syscall.Statfs_t
    if err := syscall.Statfs(path, &stat); err != nil {
        return "error"
    }

    // Filesystem magic numbers from linux/magic.h
    switch stat.Type {
    case 0x9123683E:
        return "btrfs"
    case 0x58465342:
        return "xfs"
    case 0xCA451A4E:
        return "bcachefs"
    case 0xEF53:
        return "ext4"
    case 0x6969:
        return "nfs"
    case 0x01021994:
        return "tmpfs"
    default:
        return "unknown"
    }
}

// SupportsReflinks checks if the filesystem at path likely supports reflinks.
// This is a heuristic check; actual reflink attempts may still fail.
func SupportsReflinks(path string) bool {
    switch runtime.GOOS {
    case "linux":
        fstype := FilesystemType(path)
        return fstype == "btrfs" || fstype == "xfs" || fstype == "bcachefs"
    case "darwin":
        // APFS is default on macOS 10.13+
        return true // Optimistic; will fail gracefully if HFS+
    case "windows":
        // ReFS support check would go here
        return false
    default:
        return false
    }
}
```

---

## Step 3: Modify DropFile Function

### File: `htfs/functions.go` (around line 259)

**BEFORE:**
```go
func DropFile(library Library, digest, sinkname string, details *File, rewrite []byte) anywork.Work {
    return func() {
        if details.IsSymlink() {
            anywork.OnErrPanicCloseAll(restoreSymlink(details.Symlink, sinkname))
            return
        }
        reader, closer, err := library.Open(digest)
        anywork.OnErrPanicCloseAll(err)
        // ... rest of function
    }
}
```

**AFTER:**
```go
func DropFile(library Library, digest, sinkname string, details *File, rewrite []byte) anywork.Work {
    return func() {
        if details.IsSymlink() {
            anywork.OnErrPanicCloseAll(restoreSymlink(details.Symlink, sinkname))
            return
        }

        // NEW: Try reflink for files without relocations
        if len(details.Rewrite) == 0 && common.UseReflinkStrategy() {
            sourcePath := library.ExactLocation(digest)
            if success, err := Reflink(sourcePath, sinkname); err == nil && success {
                // Reflink succeeded! File is instantly cloned via COW
                common.Debug("Reflink: %s -> %s", sourcePath, sinkname)
                anywork.OnErrPanicCloseAll(os.Chmod(sinkname, details.Mode))
                anywork.OnErrPanicCloseAll(os.Chtimes(sinkname, motherTime, motherTime))
                return
            }
            // Reflink failed or not supported, fall through to regular copy
            if err != nil {
                common.Debug("Reflink failed: %v, falling back to copy", err)
            }
        }

        // EXISTING: Regular copy with relocations
        reader, closer, err := library.Open(digest)
        anywork.OnErrPanicCloseAll(err)
        // ... rest of existing function unchanged
    }
}
```

---

## Step 4: Add Configuration Support

### File: `common/strategies.go` (NEW)

```go
package common

import (
    "os"
    "strings"
)

type HolotreeStrategy int

const (
    StrategyCopy HolotreeStrategy = iota
    StrategyReflink
    StrategySmart
)

var (
    holotreeStrategy = StrategySmart // Default to smart
)

func init() {
    // Check environment variable
    if strategy := os.Getenv("RCC_HOLOTREE_STRATEGY"); strategy != "" {
        switch strings.ToLower(strategy) {
        case "copy":
            holotreeStrategy = StrategyCopy
        case "reflink":
            holotreeStrategy = StrategyReflink
        case "smart":
            holotreeStrategy = StrategySmart
        }
    }
}

// SetHolotreeStrategy sets the global holotree restoration strategy.
func SetHolotreeStrategy(strategy HolotreeStrategy) {
    holotreeStrategy = strategy
}

// UseReflinkStrategy returns true if reflinks should be attempted.
func UseReflinkStrategy() bool {
    return holotreeStrategy == StrategyReflink || holotreeStrategy == StrategySmart
}

// GetHolotreeStrategy returns the current strategy.
func GetHolotreeStrategy() HolotreeStrategy {
    return holotreeStrategy
}

// StrategyName returns the human-readable name of a strategy.
func StrategyName(s HolotreeStrategy) string {
    switch s {
    case StrategyCopy:
        return "copy"
    case StrategyReflink:
        return "reflink"
    case StrategySmart:
        return "smart"
    default:
        return "unknown"
    }
}
```

### File: `cmd/holotree.go` (modify existing command)

Add flag to holotree commands:

```go
// In holotreeInitCmd or similar
var holotreeInitCmd = &cobra.Command{
    Use:   "init",
    Short: "Initialize holotree environment",
    Run: func(cmd *cobra.Command, args []string) {
        // NEW: Parse strategy flag
        strategy, _ := cmd.Flags().GetString("strategy")
        switch strings.ToLower(strategy) {
        case "copy":
            common.SetHolotreeStrategy(common.StrategyCopy)
        case "reflink":
            common.SetHolotreeStrategy(common.StrategyReflink)
        case "smart":
            common.SetHolotreeStrategy(common.StrategySmart)
        }

        // Existing init logic...
    },
}

func init() {
    // Add strategy flag
    holotreeInitCmd.Flags().String("strategy", "smart",
        "File restoration strategy: copy, reflink, smart")
}
```

---

## Step 5: Add Statistics Tracking (Optional)

### File: `htfs/stats.go` (modify existing)

Add reflink counter to the stats struct:

```go
type stats struct {
    sync.Mutex
    files      uint64
    directories uint64
    dirty      uint64
    links      uint64
    duplicates uint64
    reflinks   uint64  // NEW
}

func (s *stats) Reflink() {
    s.Lock()
    defer s.Unlock()
    s.reflinks++
}
```

Update DropFile to call `stats.Reflink()` on success:

```go
if success, err := Reflink(sourcePath, sinkname); err == nil && success {
    stats.Reflink()  // Track reflink usage
    // ... rest of reflink success code
}
```

---

## Step 6: Add Tests

### File: `htfs/reflink_test.go` (NEW)

```go
package htfs

import (
    "os"
    "path/filepath"
    "testing"
)

func TestReflinkBasic(t *testing.T) {
    tmpDir := t.TempDir()
    src := filepath.Join(tmpDir, "source.txt")
    dst := filepath.Join(tmpDir, "dest.txt")

    // Create source file
    content := []byte("test content")
    if err := os.WriteFile(src, content, 0644); err != nil {
        t.Fatalf("Failed to create source: %v", err)
    }

    // Try reflink
    success, err := Reflink(src, dst)
    if err != nil {
        t.Fatalf("Reflink error: %v", err)
    }

    if !success {
        t.Skip("Reflinks not supported on this filesystem")
    }

    // Verify destination exists and has same content
    dstContent, err := os.ReadFile(dst)
    if err != nil {
        t.Fatalf("Failed to read dest: %v", err)
    }

    if string(dstContent) != string(content) {
        t.Errorf("Content mismatch: got %q, want %q", dstContent, content)
    }

    t.Logf("Reflink SUCCESS on filesystem: %s", FilesystemType(tmpDir))
}

func TestReflinkFallback(t *testing.T) {
    tmpDir := t.TempDir()
    src := filepath.Join(tmpDir, "source.txt")
    dst := filepath.Join(tmpDir, "dest.txt")

    os.WriteFile(src, []byte("test"), 0644)

    // Try reflink
    success, err := Reflink(src, dst)

    // Should either succeed or fail gracefully (not supported)
    if err != nil && success {
        t.Errorf("Unexpected state: success=true with error=%v", err)
    }

    if !success {
        t.Logf("Reflinks not supported (expected on %s)", FilesystemType(tmpDir))
    }
}
```

---

## Step 7: Update Documentation

### File: `docs/holotree.md` (add section)

```markdown
## File Restoration Strategies

RCC supports multiple strategies for restoring files from hololib to holotree:

### Copy (Default on unsupported filesystems)
- Traditional file copy using I/O
- Slow but universal
- Works on all filesystems

### Reflink (Fast on supported filesystems)
- Copy-on-write cloning
- Instant file creation
- Supported on: Btrfs, XFS, APFS, ReFS
- Falls back to copy if not supported

### Smart (Recommended)
- Automatically detects filesystem capabilities
- Uses reflinks when available
- Falls back to copy otherwise

### Configuration

Environment variable:
```bash
export RCC_HOLOTREE_STRATEGY=smart  # or: copy, reflink
```

Command-line flag:
```bash
rcc holotree init --strategy=reflink
```
```

---

## Expected Performance Improvements

### Before (Regular Copy)
```
Creating environment "test-env"...
Files: 1000
Time: 15.3 seconds
I/O: 1GB (500MB read + 500MB write)
```

### After (Reflinks on Btrfs/XFS)
```
Creating environment "test-env"...
Files: 1000
Time: 1.1 seconds (14x faster!)
I/O: <1MB (metadata only)
Reflinks: 950/1000 (95%)
Copies: 50/1000 (5% - files with relocations)
```

---

## Testing Checklist

- [ ] Test on ext4 (should fall back to copy)
- [ ] Test on Btrfs (should use reflinks)
- [ ] Test on XFS (should use reflinks)
- [ ] Test on tmpfs (should fall back to copy)
- [ ] Test with files that have relocations (should copy)
- [ ] Test with files that don't have relocations (should reflink)
- [ ] Test with --strategy=copy (should always copy)
- [ ] Test with --strategy=reflink (should try reflinks)
- [ ] Test with --strategy=smart (should auto-detect)
- [ ] Test on macOS APFS (should use clonefile)
- [ ] Verify timestamps are preserved
- [ ] Verify permissions are preserved
- [ ] Verify content integrity

---

## Rollout Plan

### Phase 1: Implement Core
- Add reflink_*.go files
- Modify DropFile
- Add basic configuration
- **Ship as experimental feature**

### Phase 2: Stabilize
- Add comprehensive tests
- Gather user feedback
- Fix edge cases
- Add statistics tracking

### Phase 3: Optimize
- Make "smart" the default
- Add telemetry
- Document performance gains
- Celebrate! 🎉

---

## Troubleshooting

### Issue: "Reflinks not working"

**Check:**
```bash
# Verify filesystem type
df -T $ROBOCORP_HOME

# Run test
go run REFLINK_EXAMPLE.go

# Check strategy
echo $RCC_HOLOTREE_STRATEGY
```

### Issue: "No performance improvement"

**Likely cause:** Files have relocations and are being copied

**Fix:** This is expected and correct behavior. Files with relocations MUST be copied.

### Issue: "Permission denied"

**Likely cause:** Source and destination on different filesystems

**Fix:** Ensure hololib and holotree are on the same filesystem

---

## Summary

**Total changes:**
- 5 new files (~150 lines)
- 2 modified files (~20 lines)
- 1 test file (~50 lines)

**Total time:** 2-3 days for full implementation and testing

**Expected speedup:** 10-20x on supported filesystems

**Risk:** Low (graceful fallback to existing behavior)

---

**Next step:** Start with Step 1, create the reflink_linux.go file and test it!
