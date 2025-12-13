# Fast File Copy - Quick Reference

## TL;DR - What to Use

| Platform | Method | Speed Improvement | When to Use |
|----------|--------|-------------------|-------------|
| **Linux** | FICLONE ioctl | 100-1000x (instant) | Same filesystem (Btrfs/XFS) |
| **Linux** | copy_file_range() | 2-3x | All regular files |
| **macOS** | clonefile() | 100-1000x (instant) | APFS (default since 10.13) |
| **macOS** | F_NOCACHE | 1.5-2x | Large files |
| **Windows** | ReFS Clone | 100-1000x (instant) | ReFS filesystem |
| **Windows** | CopyFileEx | 1.5-2x | All files |

---

## Implementation Priority

### 1. Linux (Highest Impact)
```go
//go:build linux

func platformCopy(source, target string, overwrite bool) error {
    // Try reflink (instant on Btrfs/XFS)
    if CloneFileLinux(source, target) == nil {
        return nil
    }
    // Fallback to copy_file_range (2-3x faster)
    return CopyFileLinux(source, target, overwrite)
}
```

**Key syscall**: `unix.CopyFileRange(srcFd, &offset, dstFd, nil, size, 0)`

---

### 2. macOS (Second Priority)
```go
//go:build darwin

func platformCopy(source, target string, overwrite bool) error {
    // Try clonefile (instant on APFS)
    if CloneFileMacOS(source, target) == nil {
        return nil
    }
    // Fallback to standard copy
    return copyFileStandard(source, target, overwrite)
}
```

**Key syscall**: `syscall.Syscall(SYS_CLONEFILE, srcPtr, dstPtr, 0)`

---

### 3. Windows (Third Priority)
```go
//go:build windows

func platformCopy(source, target string, overwrite bool) error {
    // Try ReFS block cloning
    if CloneFileReFS(source, target) == nil {
        return nil
    }
    // Fallback to CopyFileEx
    return CopyFileWindows(source, target, overwrite)
}
```

**Key API**: `CopyFileExW()` from kernel32.dll

---

## Performance Comparison

### Small Files (<1MB)
- **Baseline (io.Copy)**: 100%
- **Platform-optimized**: 150-200%
- **CoW (reflink/clone)**: 10000%+ (instant)

### Medium Files (10MB)
- **Baseline**: 100%
- **Platform-optimized**: 200-300%
- **CoW**: 10000%+ (instant)

### Large Files (1GB+)
- **Baseline**: 100%
- **Platform-optimized**: 300-500%
- **CoW**: 10000%+ (instant)

---

## Code Structure

```
pathlib/
├── copyfile.go           # Generic interface + fallback
├── copyfile_linux.go     # Linux: copy_file_range + FICLONE
├── copyfile_darwin.go    # macOS: clonefile + F_NOCACHE
└── copyfile_windows.go   # Windows: CopyFileEx + ReFS clone
```

---

## Testing Checklist

- [ ] Small files (1KB-1MB) - verify correctness
- [ ] Medium files (10MB-100MB) - verify performance
- [ ] Large files (1GB+) - verify memory efficiency
- [ ] Cross-filesystem copy - verify fallback works
- [ ] Same filesystem - verify CoW optimization works
- [ ] Permission preservation - verify mode/owner/time
- [ ] Symlinks - verify handling
- [ ] Error conditions - verify graceful fallback

---

## Common Pitfalls

### Linux
- FICLONE only works on same filesystem
- copy_file_range() may fall back to splice() internally
- O_DIRECT requires aligned buffers (complex)

### macOS
- clonefile() only works on APFS (not HFS+)
- Must be same volume
- Requires macOS 10.12+

### Windows
- ReFS cloning only on Windows Server 2016+ / Win 10 1709+
- CopyFileEx preserves security attributes by default
- Block cloning requires same volume

---

## Migration Strategy

1. **Add FastCopy() function** (non-breaking)
2. **Test in robotcache.go** (low-risk)
3. **Expand to other uses** (gradual rollout)
4. **Replace CopyFile()** (after validation)

---

## Key Dependencies

Already in go.mod:
```go
golang.org/x/sys v0.13.0
```

No additional dependencies needed!

---

## Minimal Working Example

```go
// pathlib/copyfile_linux.go
//go:build linux

package pathlib

import (
    "os"
    "golang.org/x/sys/unix"
)

func platformCopy(source, target string, overwrite bool) error {
    src, _ := os.Open(source)
    defer src.Close()

    srcStat, _ := src.Stat()

    dst, _ := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
    defer dst.Close()

    var off int64 = 0
    remain := srcStat.Size()

    for remain > 0 {
        n, err := unix.CopyFileRange(int(src.Fd()), &off, int(dst.Fd()), nil, int(remain), 0)
        if err != nil {
            return err
        }
        remain -= int64(n)
    }

    return nil
}
```

---

## Benchmarking Command

```bash
# Run benchmarks
go test -bench=BenchmarkCopy -benchmem -cpuprofile=cpu.prof ./pathlib

# View profile
go tool pprof cpu.prof
```

---

## Further Reading

See **FAST_FILE_COPY_RESEARCH.md** for:
- Complete implementation examples
- Detailed API documentation
- Advanced optimizations (O_DIRECT, fallocate, etc.)
- Platform-specific gotchas
- Performance tuning tips
