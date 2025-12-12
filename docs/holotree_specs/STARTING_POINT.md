# RCC Holotree Performance: The Starting Point

**Cross-Referenced Analysis from:**
- vjmp's Issue #61 & #63 comments (original developer insights)
- vjmp's commit d341d34 (what he wanted for private use)
- Accelerating RCC Holotree Performance PDF (technical deep-dive)
- docs/holotree.md (current architecture documentation)
- docs/roadmap.md (project direction)
- htfs/functions.go (actual implementation)

---

## The Critical Insight from vjmp

In Issue #63, vjmp (the father of holotree) gave us the key constraint:

> **"files that have relocations in them cannot be hardlinked from one environment to next, or some code will end up running/accessing/modifying things on wrong environment/directory (because they are referring other files based on 'hardcoded' location information inside them); we saw robots (stacktraces) jumping from one environment to next"**

This is the dragon. But he also said:

> **"None of those should prevent you trying out things. You might come up with some great solution. Or solution that has multiple strategies to cover all corners."**

---

## The Key Discovery: Relocations Are Already Tracked

Looking at `htfs/functions.go:330-349`, the `CalculateTreeStats` function already tracks relocations:

```go
func CalculateTreeStats() (Dirtask, *TreeStats) {
    result := &TreeStats{}
    return func(path string, it *Dir) anywork.Work {
        return func() {
            // ...
            for _, file := range it.Files {
                // ...
                if len(file.Rewrite) > 0 {
                    result.Relocations += 1  // <-- ALREADY COUNTED!
                }
            }
        }
    }, result
}
```

And in the catalog format (`htfs/directory.go`):

```go
type File struct {
    Name    string      `json:"name"`
    Digest  string      `json:"digest"`
    Rewrite []int64     `json:"rewrite"`  // <-- Relocation offsets stored per file!
    // ...
}
```

**This means we can distinguish files that need copying (have relocations) from files that could use faster methods (no relocations).**

---

## The Bottleneck: `DropFile` Function

The performance-critical function is `DropFile` (`htfs/functions.go:259-303`):

```go
func DropFile(library Library, digest, sinkname string, details *File, rewrite []byte) anywork.Work {
    return func() {
        // 1. Handle symlinks (fast path - already exists)
        if details.IsSymlink() {
            anywork.OnErrPanicCloseAll(restoreSymlink(details.Symlink, sinkname))
            return
        }

        // 2. Open from library, decompress
        reader, closer, err := library.Open(digest)

        // 3. Copy to temp file (THE BOTTLENECK)
        _, err = io.Copy(many, reader)

        // 4. Apply relocations (path rewriting)
        for _, position := range details.Rewrite {
            sink.Seek(position, 0)
            sink.Write(rewrite)
        }

        // 5. Rename, chmod, chtimes
    }
}
```

**Every file goes through full copy + decompress, even files with no relocations.**

---

## What the PDF Says

From "Accelerating RCC Holotree Environment Performance":

> **"Hardlinks: A hardlink makes a directory entry to the same file data on disk, so creating a hardlink is extremely fast (no file content is duplicated). If RCC were to hardlink files from hololib into the environment directory, environment creation would be very fast."**

> **"Copy-on-write (CoW) Filesystems: Using advanced filesystems or overlays can combine the benefits of linking with isolation. For example... Btrfs has reflink copy which allows cheap CoW copies of files (e.g. `cp --reflink`)."**

> **"If RCC detects the filesystem supports it, it could choose to clone files rather than copy, achieving the same effect as hardlink (distinct files that share data until written)."**

---

## The Solution: Tiered Restoration Strategy

Based on all sources, here's the correct approach:

### Tier 1: Files WITH Relocations (Must Copy)
- These files have `len(details.Rewrite) > 0`
- They contain hardcoded paths that must be rewritten
- **vjmp's warning applies: ALWAYS copy these**
- Current `DropFile` behavior is correct for these

### Tier 2: Files WITHOUT Relocations (Can Optimize)
- These files have `len(details.Rewrite) == 0`
- They have no embedded paths
- **Can use faster restoration methods:**

```
Priority Order (try in sequence until one works):
1. Reflink (cp --reflink) - CoW copy, instant, isolated
2. Hardlink (os.Link) - Instant but shares inode
3. Regular Copy - Fallback (current behavior)
```

### Why Reflinks > Hardlinks

From the PDF:
> **"Btrfs has reflink copy which allows cheap CoW copies of files... achieving the same effect as hardlink (distinct files that share data until written)."**

Reflinks address vjmp's concerns:
- **Security**: Different inodes, no cross-env modification
- **Isolation**: Changes are copy-on-write
- **.pyc files**: If Python writes .pyc, it gets its own copy

---

## The Implementation Plan

### Step 1: Add Filesystem Detection
```go
// pathlib/filesystem.go (new file)
func SupportsReflink(path string) bool
func SupportsHardlink(path string) bool
```

### Step 2: Add Fast Copy Functions
```go
// pathlib/copyfile.go (exists, enhance it)
func Reflink(src, dst string) error   // cp --reflink=always
func TryReflink(src, dst string) bool // Returns false if unsupported
```

### Step 3: Modify DropFile to Use Tiers
```go
func DropFile(...) anywork.Work {
    return func() {
        if details.IsSymlink() {
            // existing symlink handling
            return
        }

        // NEW: Fast path for files without relocations
        if len(details.Rewrite) == 0 {
            srcPath := library.ExactLocation(digest)

            // Try reflink first (fastest, safest)
            if pathlib.TryReflink(srcPath, sinkname) {
                os.Chmod(sinkname, details.Mode)
                os.Chtimes(sinkname, motherTime, motherTime)
                return
            }

            // Optionally try hardlink (if enabled and safe)
            // NOTE: Only for single-user, read-only scenarios
        }

        // Existing copy path (for files with relocations)
        // ... current implementation ...
    }
}
```

### Step 4: Add Configuration
```yaml
# settings.yaml
holotree:
  restoration-strategy: "auto"  # auto, copy-only, reflink, hardlink
```

---

## File-by-File Analysis

Based on typical Python environments:

| File Type | Has Relocations | Percentage | Can Optimize |
|-----------|-----------------|------------|--------------|
| `.py` source | Usually no | ~20% | Yes (reflink) |
| `.pyc` bytecode | Usually no | ~15% | Yes (reflink) |
| `.so/.dll` libs | Sometimes | ~10% | Partial |
| Data files | No | ~30% | Yes (reflink) |
| Scripts with shebang | Yes (path in #!) | ~5% | No |
| Site-packages metadata | Sometimes | ~10% | Partial |
| Executables | Usually yes | ~10% | No |

**Estimated impact: 60-70% of files could use fast path**

---

## The Starting Point: What to Build First

### Phase 1: Reflink Support (Safest Win)
1. Add `pathlib.TryReflink()` function
2. Detect filesystem support at startup
3. Modify `DropFile` to use reflink for no-relocation files
4. Benchmark: measure before/after on Btrfs/XFS/APFS

### Phase 2: Metrics & Validation
1. Add counters: `stats.Reflinked()`, `stats.Hardlinked()`, `stats.Copied()`
2. Log strategy usage per restore
3. Validate environments work correctly

### Phase 3: Configuration
1. Add `RCC_HOLOTREE_STRATEGY` env var
2. Add settings.yaml option
3. Add `--strategy` CLI flag

### Phase 4: Optional Hardlinks (Advanced)
Only after reflinks are proven:
1. Add read-only hardlink option
2. Document security implications
3. Default to disabled

---

## Platform Support Matrix

| Platform | Filesystem | Reflink | Hardlink | Notes |
|----------|------------|---------|----------|-------|
| Linux | Btrfs | Yes | Yes | Best support |
| Linux | XFS | Yes | Yes | Good support |
| Linux | ext4 | No | Yes | Hardlink only |
| macOS | APFS | Yes (`cp -c`) | Yes | Good support |
| macOS | HFS+ | No | Yes | Legacy |
| Windows | ReFS | Yes | Yes | Rare |
| Windows | NTFS | No | Yes | Most common |

---

## Key Code Locations

| File | What to Modify |
|------|----------------|
| `htfs/functions.go:259` | `DropFile` - add fast path |
| `pathlib/copyfile.go` | Add reflink functions |
| `common/strategies.go` | Add strategy configuration |
| `htfs/stats.go` | Add new counters |

---

## Summary: The Path Forward

1. **vjmp is right**: Files with relocations MUST be copied
2. **The opportunity**: Files WITHOUT relocations (~60-70%) can be optimized
3. **Reflinks first**: Safer than hardlinks, addresses all vjmp's concerns
4. **Already have the data**: `file.Rewrite` tells us which files can be optimized
5. **Minimal code change**: Modify `DropFile` to check `len(details.Rewrite)`

**The starting point is `htfs/functions.go:259` - the `DropFile` function.**

---

## Acknowledgment

This analysis was made possible by @vjmp's willingness to share his hard-won knowledge. The dragons he warned about are real, but so is the path around them.

> *"You might come up with some great solution. Or solution that has multiple strategies to cover all corners."* - @vjmp
