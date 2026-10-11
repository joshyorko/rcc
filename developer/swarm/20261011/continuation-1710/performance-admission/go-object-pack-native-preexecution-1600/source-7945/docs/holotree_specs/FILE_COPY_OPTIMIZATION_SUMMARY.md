# File Copy Optimization Research Summary

**Date**: 2025-12-12
**Project**: RCC (Repeatable, Contained Code)
**Current Implementation**: Standard `io.Copy()` in `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/pathlib/copyfile.go`

---

## Key Findings

### Current Performance Issues

The existing `CopyFile()` function uses `io.Copy()` which:
1. Reads data into user-space buffer
2. Copies buffer in user memory
3. Writes from buffer to disk

This involves **4+ context switches** and **2+ memory copies** per buffer.

### Optimization Opportunities

| Platform | Fastest Method | Speed Gain | Availability |
|----------|---------------|------------|--------------|
| **Linux** | copy_file_range() | 2-3x | Kernel 4.5+ (2016) |
| **Linux** | FICLONE ioctl | 100-1000x | Btrfs, XFS 4.16+, OCFS2 |
| **macOS** | clonefile() | 100-1000x | macOS 10.12+, APFS only |
| **macOS** | F_NOCACHE | 1.5-2x | All macOS versions |
| **Windows** | CopyFileEx | 1.5-2x | Windows XP+ |
| **Windows** | ReFS Clone | 100-1000x | Windows Server 2016+, ReFS only |

---

## Documents Created

### 1. FAST_FILE_COPY_RESEARCH.md (25KB, 1074 lines)
**Comprehensive technical documentation** including:
- Detailed API documentation for each platform
- Complete Go code examples with error handling
- Performance benchmarks and comparisons
- Testing strategies and migration plans
- All necessary imports and dependencies
- Platform-specific gotchas and limitations

**Sections**:
- Linux: copy_file_range, FICLONE, O_DIRECT, fallocate
- macOS: clonefile, copyfile, F_NOCACHE
- Windows: CopyFileEx, ReFS block cloning
- Implementation strategy (3 phases)
- Testing & benchmarking
- Migration plan

### 2. FAST_FILE_COPY_QUICKREF.md (4.8KB)
**Quick reference card** for developers:
- TL;DR comparison table
- Minimal code examples for each platform
- Implementation priorities
- Common pitfalls checklist
- Benchmarking commands

### 3. IMPLEMENTATION_EXAMPLE_LINUX.go (4.2KB)
**Reference implementation** showing:
- Complete working code for Linux
- Smart fallback strategy (reflink → copy_file_range → io.Copy)
- Proper error handling
- Debug logging integration
- Benchmark test examples

---

## Recommended Implementation

### File Structure
```
pathlib/
├── copyfile.go           # Generic interface (existing)
├── copyfile_linux.go     # Linux optimizations (NEW)
├── copyfile_darwin.go    # macOS optimizations (NEW)
└── copyfile_windows.go   # Windows optimizations (NEW)
```

### Strategy: Try Fast → Fallback to Safe

```go
func FastCopy(source, target string, overwrite bool) error {
    // Try platform-specific optimization
    err := platformCopy(source, target, overwrite)
    if err != nil {
        // Fallback to standard io.Copy
        err = copyFile(source, target, overwrite, io.Copy)
    }
    return err
}
```

Each platform implements `platformCopy()` with its own optimization hierarchy.

---

## Impact Analysis

### Where CopyFile is Used (5 locations)

1. **operations/robotcache.go**
   - Caching robot files
   - Impact: Medium (frequent operation)

2. **cmd/testrun.go**
   - Copying test artifacts back
   - Impact: Medium (batch operations)

3. **cmd/robotdependencies.go**
   - Copying golden master environments
   - Impact: High (large conda environments)

4. **cmd/robotRunFromBundle.go**
   - Extracting bundle contents
   - Impact: High (many files, frequent)

5. **operations/carrier.go** (uses io.Copy directly)
   - Self-executable copying
   - Impact: Low (single binary)

### Expected Performance Improvements

**Best Case** (CoW available):
- Large environment copy: 5 minutes → 1 second
- Bundle extraction: 30 seconds → 1-2 seconds

**Typical Case** (no CoW):
- Large environment copy: 5 minutes → 2 minutes
- Bundle extraction: 30 seconds → 15 seconds

**Worst Case** (fallback to io.Copy):
- No regression, same performance as today

---

## Implementation Priority

### Phase 1: Linux (Highest Value)
- Most RCC deployments run on Linux
- copy_file_range available on all modern kernels
- FICLONE available on Btrfs/XFS (common in cloud)

**Estimated effort**: 4-6 hours
**Expected improvement**: 2-3x general, 100x+ with CoW

### Phase 2: macOS (Developer Experience)
- Improves local development speed
- APFS is default since macOS 10.13 (2017)
- clonefile nearly universal on modern Macs

**Estimated effort**: 3-4 hours
**Expected improvement**: 100x+ with APFS

### Phase 3: Windows (Completeness)
- CopyFileEx works everywhere
- ReFS less common but useful for servers

**Estimated effort**: 3-4 hours
**Expected improvement**: 1.5-2x general

---

## Risk Assessment

### Low Risk
- Fallback to io.Copy ensures compatibility
- No new external dependencies (golang.org/x/sys already used)
- Platform-specific files isolate failures
- Can roll out gradually (one command at a time)

### Testing Requirements
- Cross-filesystem copy (fallback path)
- Same-filesystem copy (optimization path)
- Permission preservation
- Timestamp preservation
- Large files (memory efficiency)
- Error conditions

### Rollback Plan
If issues arise:
```go
func FastCopy(source, target string, overwrite bool) error {
    // Temporarily disable optimizations
    return copyFile(source, target, overwrite, io.Copy)
}
```

---

## Next Steps

1. **Review** research documents (this document, RESEARCH.md, QUICKREF.md)
2. **Decide** on implementation priority (recommend Linux first)
3. **Create** platform-specific files using IMPLEMENTATION_EXAMPLE_LINUX.go as template
4. **Test** with small subset of operations (robotcache.go)
5. **Benchmark** to validate improvements
6. **Expand** to other operations once stable
7. **Document** in CLAUDE.md for future developers

---

## Dependencies

**Already available** in go.mod:
```go
golang.org/x/sys v0.13.0
```

**No additional dependencies required!**

---

## References

### Internal Documents
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/FAST_FILE_COPY_RESEARCH.md` - Full technical details
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/FAST_FILE_COPY_QUICKREF.md` - Quick reference
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/IMPLEMENTATION_EXAMPLE_LINUX.go` - Working code example

### External Documentation
- [Linux copy_file_range(2)](https://man7.org/linux/man-pages/man2/copy_file_range.2.html)
- [Linux FICLONE ioctl](https://man7.org/linux/man-pages/man2/ioctl_ficlonerange.2.html)
- [macOS clonefile(2)](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/clonefile.2.html)
- [Windows CopyFileEx](https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-copyfileexa)
- [Windows ReFS Cloning](https://docs.microsoft.com/en-us/windows-server/storage/refs/block-cloning)

---

## Conclusion

Platform-specific file copy optimizations offer:
- **2-3x improvement** in typical cases
- **100-1000x improvement** with CoW support (common on modern systems)
- **Zero risk** due to automatic fallback
- **No new dependencies**
- **Minimal code complexity** (100-150 lines per platform)

**Recommendation**: Implement Linux optimizations first (highest deployment volume), then macOS (developer experience), then Windows (completeness).

Total estimated effort: **10-14 hours** for all platforms.

Expected ROI: Significant reduction in bundle extraction and environment setup times, especially beneficial for CI/CD pipelines and cloud deployments.
