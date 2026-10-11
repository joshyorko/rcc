# Implementation Plan: Smart Filesystem Compression Strategy

**Date:** 2025-12-12
**Status:** Planning Phase
**Target:** RCC v18.13.0 or later

---

## Executive Summary

This document outlines the implementation plan for adding smart filesystem compression detection to RCC, enabling optimal performance across different filesystems.

**Goal:** Enable RCC to automatically choose the best compression strategy based on filesystem capabilities, achieving both disk savings AND reflink speed on supported filesystems (Btrfs, ZFS).

---

## Current State

### How Compression Works Today

**Location:** `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/htfs/library.go:291-292`

```go
func Compress() bool {
    return !pathlib.IsFile(common.HololibCompressMarker())
}
```

**Current behavior:**
- If `~/.robocorp/hololib/catalog/compress.no` exists → store files **uncompressed**
- Otherwise → store files **gzip compressed**
- No filesystem detection
- No smart decisions
- User must manually create marker file

### Where Compression is Used

#### 1. Lifting Files to Hololib

**Location:** `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/htfs/functions.go:223-256`

```go
func LiftFile(sourcename, sinkname string, compress bool) anywork.Work {
    return func() {
        source, err := os.Open(sourcename)
        // ...

        var writer io.WriteCloser
        writer = sink
        if compress {
            writer, err = gzip.NewWriterLevel(sink, gzip.BestSpeed)
            // ...
        }

        _, err = io.Copy(writer, source)
        // ...
    }
}
```

**Called from:** `ScheduleLifters()` in `htfs/functions.go:182-221`

#### 2. Dropping Files from Hololib

**Location:** `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/htfs/functions.go:259-303`

```go
func DropFile(library Library, digest, sinkname string, details *File, rewrite []byte) anywork.Work {
    return func() {
        // ...
        reader, closer, err := library.Open(digest)
        // This will decompress if file is gzipped
        // ...
    }
}
```

**Decompression happens in:** `/var/home/kdlocpanda/second_brain/Projects/yorko-io/rcc/htfs/delegates.go:11-33`

```go
func gzDelegateOpen(filename string, ungzip bool) (readable io.Reader, closer Closer, err error) {
    // ...
    source, err := os.Open(filename)

    reader, err = gzip.NewReader(source)
    if err != nil || !ungzip {
        // Not gzipped or don't decompress
        reader = source
    }
    // ...
}
```

### Current Limitations

1. **No filesystem detection** - doesn't know if filesystem has compression
2. **Binary decision** - compress or don't compress, no smart mode
3. **No reflink awareness** - doesn't consider if reflinks would benefit
4. **Manual configuration** - user must create marker file
5. **No reporting** - user doesn't know what strategy is being used

---

## Proposed Changes

### Phase 1: Filesystem Detection (Week 1)

**New file:** `htfs/filesystem.go`

```go
package htfs

import (
    "golang.org/x/sys/unix"
)

// FilesystemInfo contains detected filesystem capabilities
type FilesystemInfo struct {
    Type              string // "btrfs", "xfs", "zfs", "ext4", etc.
    SupportsReflinks  bool
    HasCompression    bool
    CompressionAlgo   string
    Recommended       CompressionStrategy
}

// CompressionStrategy determines how to handle compression
type CompressionStrategy int

const (
    StrategyAppLevel CompressionStrategy = iota  // Use gzip in RCC
    StrategyDisabled                             // No compression
    StrategyFilesystem                          // Rely on FS compression
)

// DetectFilesystem detects filesystem capabilities for given path
func DetectFilesystem(path string) (*FilesystemInfo, error) {
    // Implementation from FILESYSTEM_DETECTION_EXAMPLE.go
}

// RecommendStrategy recommends compression strategy based on filesystem
func RecommendStrategy(info *FilesystemInfo) CompressionStrategy {
    if info.HasCompression && info.SupportsReflinks {
        // Best case: Btrfs/ZFS with compression
        return StrategyFilesystem
    }

    if info.SupportsReflinks {
        // XFS/APFS: trade-off, default to speed
        return StrategyDisabled
    }

    // ext4/NTFS: use app compression
    return StrategyAppLevel
}
```

**Platform-specific files:**

- `htfs/filesystem_linux.go` - Linux implementation (Btrfs, XFS, ZFS, ext4)
- `htfs/filesystem_darwin.go` - macOS implementation (APFS, HFS+)
- `htfs/filesystem_windows.go` - Windows implementation (NTFS, ReFS)
- `htfs/filesystem_unsupported.go` - Fallback for unknown platforms

### Phase 2: Smart Compression Selection (Week 2)

**Modify:** `htfs/library.go`

**Current:**
```go
func Compress() bool {
    return !pathlib.IsFile(common.HololibCompressMarker())
}
```

**New:**
```go
var (
    compressionStrategy     CompressionStrategy
    compressionStrategyOnce sync.Once
    compressionDetected     bool
)

// Compress returns true if files should be compressed at application level
func Compress() bool {
    compressionStrategyOnce.Do(func() {
        initCompressionStrategy()
    })

    return compressionStrategy == StrategyAppLevel
}

func initCompressionStrategy() {
    // Check for manual override first (backward compatibility)
    if pathlib.IsFile(common.HololibCompressMarker()) {
        compressionStrategy = StrategyDisabled
        compressionDetected = true
        common.Debug("Compression disabled via marker file")
        return
    }

    // Check environment variable override
    if envStrategy := os.Getenv("RCC_COMPRESSION_STRATEGY"); envStrategy != "" {
        switch strings.ToLower(envStrategy) {
        case "app", "gzip", "enable":
            compressionStrategy = StrategyAppLevel
            common.Debug("Compression strategy: app-level (env override)")
            return
        case "none", "disable", "off":
            compressionStrategy = StrategyDisabled
            common.Debug("Compression strategy: disabled (env override)")
            return
        case "smart", "auto":
            // Continue to detection
        default:
            common.Warning("Unknown RCC_COMPRESSION_STRATEGY: %s, using smart mode", envStrategy)
        }
    }

    // Smart detection
    hololibPath := common.HololibLibraryLocation()
    fsInfo, err := DetectFilesystem(hololibPath)
    if err != nil {
        common.Debug("Filesystem detection failed: %v, defaulting to app compression", err)
        compressionStrategy = StrategyAppLevel
        return
    }

    compressionStrategy = RecommendStrategy(fsInfo)
    compressionDetected = true

    // Log the decision
    switch compressionStrategy {
    case StrategyFilesystem:
        common.Log("Detected %s with %s compression - storing files uncompressed, using filesystem compression + reflinks",
            fsInfo.Type, fsInfo.CompressionAlgo)
    case StrategyDisabled:
        common.Log("Detected %s with reflink support - disabling compression for reflink speed",
            fsInfo.Type)
    case StrategyAppLevel:
        common.Log("Detected %s - using application-level gzip compression",
            fsInfo.Type)
    }
}

// GetCompressionInfo returns current compression strategy info
func GetCompressionInfo() string {
    hololibPath := common.HololibLibraryLocation()
    fsInfo, err := DetectFilesystem(hololibPath)
    if err != nil {
        return fmt.Sprintf("Cannot detect filesystem: %v", err)
    }

    var result strings.Builder
    result.WriteString(fmt.Sprintf("Hololib path: %s\n", hololibPath))
    result.WriteString(fmt.Sprintf("Filesystem: %s\n", fsInfo.Type))

    if fsInfo.HasCompression {
        result.WriteString(fmt.Sprintf("FS Compression: %s (enabled)\n", fsInfo.CompressionAlgo))
    } else {
        result.WriteString("FS Compression: Not available\n")
    }

    if fsInfo.SupportsReflinks {
        result.WriteString("Reflinks: Supported\n")
    } else {
        result.WriteString("Reflinks: Not supported\n")
    }

    result.WriteString("\nCurrent strategy: ")
    if Compress() {
        result.WriteString("Application-level gzip compression\n")
    } else if fsInfo.HasCompression {
        result.WriteString(fmt.Sprintf("Filesystem compression (%s)\n", fsInfo.CompressionAlgo))
    } else {
        result.WriteString("No compression (reflink optimization)\n")
    }

    return result.String()
}
```

**Add to:** `common/variables.go` or similar

```go
const (
    // Environment variables
    EnvCompressionStrategy = "RCC_COMPRESSION_STRATEGY"
)
```

### Phase 3: CLI Commands (Week 3)

**New command:** `rcc holotree compression`

**Add file:** `cmd/holotreeCompression.go`

```go
package cmd

import (
    "fmt"
    "github.com/spf13/cobra"
    "github.com/joshyorko/rcc/htfs"
)

var holotreeCompressionCmd = &cobra.Command{
    Use:   "compression",
    Short: "Show filesystem compression information and strategy",
    Long: `Displays information about the filesystem compression capabilities
and the current compression strategy being used by RCC.`,
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Println("=== RCC Compression Strategy ===\n")
        fmt.Println(htfs.GetCompressionInfo())

        // Add performance expectations
        fmt.Println("\nPerformance Expectations:")
        // ... show expected restore times, disk usage, etc.
    },
}

func init() {
    holotreeCmd.AddCommand(holotreeCompressionCmd)
}
```

**Usage:**
```bash
$ rcc holotree compression

=== RCC Compression Strategy ===

Hololib path: /home/user/.robocorp/hololib
Filesystem: Btrfs
FS Compression: zstd:3 (enabled)
Reflinks: Supported

Current strategy: Filesystem compression (zstd:3)

Performance Expectations:
  Disk usage: Similar to gzipped (~2-3x compression)
  Environment restore: 20-40x faster (instant reflinks)
  Decompression overhead: Minimal (~400-700 MB/s)

Recommendation: IDEAL configuration detected!
Your filesystem provides both compression and fast reflinks.
```

**Modify:** `cmd/holotree.go` - add `--compression-info` flag

```go
var holotreeCmd = &cobra.Command{
    // ...
    PersistentPreRun: func(cmd *cobra.Command, args []string) {
        if compressionInfo {
            fmt.Println(htfs.GetCompressionInfo())
            os.Exit(0)
        }
    },
}

var compressionInfo bool

func init() {
    holotreeCmd.PersistentFlags().BoolVar(&compressionInfo, "compression-info", false,
        "Show filesystem compression information and exit")
}
```

### Phase 4: Configuration & Testing (Week 4)

**Add to:** `settings.yaml` schema

```yaml
holotree:
  # Compression strategy: smart (auto-detect), app (gzip), none (disabled)
  # Default: smart
  compression-strategy: smart

  # Force specific behavior (overrides detection)
  # Useful for testing or specific requirements
  force-compression:
    enabled: false
    type: gzip  # or: none
```

**Update:** Documentation

1. `docs/holotree.md` - Add section on compression strategies
2. `docs/recipes.md` - Add recipe for optimizing performance
3. `CLAUDE.md` - Document new environment variables

**Testing:**

1. Unit tests for filesystem detection
2. Integration tests with different filesystems (via Docker)
3. Performance benchmarks comparing strategies
4. Backward compatibility tests (marker file still works)

---

## Migration Guide

### For Existing Users

**No action required!** Changes are backward compatible:

1. If you have `~/.robocorp/hololib/catalog/compress.no` → behavior unchanged
2. If you don't have the marker → smart detection applies

### Opting Out of Smart Detection

If you want to keep old behavior (always compress):

```bash
# Method 1: Environment variable
export RCC_COMPRESSION_STRATEGY=app

# Method 2: Create marker file (old method still works)
touch ~/.robocorp/hololib/catalog/compress.no

# Method 3: Configuration file
rcc config set holotree.compression-strategy app
```

### Opting In to Filesystem Compression

If you're on Btrfs but compression is not enabled:

```bash
# Enable Btrfs compression
sudo btrfs property set ~/.robocorp/hololib compression zstd

# Verify it's detected
rcc holotree compression

# Rebuild hololib to take advantage
rcc holotree cleanup --all
# Then rebuild your environments
```

---

## Testing Strategy

### Unit Tests

```go
// htfs/filesystem_test.go

func TestFilesystemDetection(t *testing.T) {
    tests := []struct {
        name     string
        fsType   int64
        expected string
    }{
        {"Btrfs", BTRFS_SUPER_MAGIC, "btrfs"},
        {"XFS", XFS_SUPER_MAGIC, "xfs"},
        {"ext4", EXT4_SUPER_MAGIC, "ext4"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test detection logic
        })
    }
}

func TestCompressionStrategy(t *testing.T) {
    tests := []struct {
        name               string
        fsInfo             *FilesystemInfo
        expectedStrategy   CompressionStrategy
    }{
        {
            "Btrfs with compression",
            &FilesystemInfo{Type: "btrfs", HasCompression: true, SupportsReflinks: true},
            StrategyFilesystem,
        },
        {
            "XFS without compression",
            &FilesystemInfo{Type: "xfs", HasCompression: false, SupportsReflinks: true},
            StrategyDisabled,
        },
        {
            "ext4",
            &FilesystemInfo{Type: "ext4", HasCompression: false, SupportsReflinks: false},
            StrategyAppLevel,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            strategy := RecommendStrategy(tt.fsInfo)
            if strategy != tt.expectedStrategy {
                t.Errorf("Expected %v, got %v", tt.expectedStrategy, strategy)
            }
        })
    }
}
```

### Integration Tests

```bash
# Test on different filesystems using Docker
docker run --rm -v $(pwd):/rcc \
    --device /dev/loop0 \
    btrfs-test-image \
    /rcc/test_filesystem_detection.sh

# Test script verifies:
# 1. Correct filesystem detection
# 2. Appropriate strategy selection
# 3. Files stored correctly (compressed or not)
# 4. Reflinks work when expected
```

### Performance Benchmarks

```go
// htfs/benchmark_test.go

func BenchmarkRestoreWithCompression(b *testing.B) {
    // Benchmark environment restoration with gzip
}

func BenchmarkRestoreWithoutCompression(b *testing.B) {
    // Benchmark environment restoration without compression
}

func BenchmarkRestoreWithReflinks(b *testing.B) {
    // Benchmark environment restoration with reflinks
}
```

---

## Rollout Plan

### Stage 1: Internal Testing (Week 5)

- Deploy to test environments
- Verify detection works on various filesystems
- Gather performance metrics
- Fix any bugs

### Stage 2: Beta Release (Week 6)

- Release as `v18.13.0-beta.1`
- Announce to community
- Gather feedback
- Monitor for issues

### Stage 3: Production Release (Week 7)

- Address feedback
- Release as `v18.13.0`
- Update documentation
- Publish blog post explaining improvements

---

## Monitoring & Metrics

### What to Track

1. **Filesystem distribution** - what filesystems are users on?
2. **Strategy distribution** - what strategies are being used?
3. **Performance improvements** - actual restore time reductions
4. **Disk usage** - compression ratios achieved
5. **Error rates** - detection failures, fallback rates

### Implementation

```go
// In htfs/library.go

func initCompressionStrategy() {
    // ... detection logic ...

    // Report telemetry (if enabled)
    go reportCompressionMetrics(fsInfo, compressionStrategy)
}

func reportCompressionMetrics(fsInfo *FilesystemInfo, strategy CompressionStrategy) {
    metrics := map[string]interface{}{
        "filesystem": fsInfo.Type,
        "fs_compression": fsInfo.HasCompression,
        "fs_reflinks": fsInfo.SupportsReflinks,
        "strategy": strategyName(strategy),
    }

    cloud.InternalBackgroundMetric(
        common.ControllerIdentity(),
        "rcc.holotree.compression.strategy",
        metrics,
    )
}
```

---

## Documentation Updates

### User-Facing Documentation

1. **docs/holotree.md** - Add "Compression Strategies" section
2. **docs/recipes.md** - Add "Optimizing Holotree Performance" recipe
3. **docs/troubleshooting.md** - Add compression-related troubleshooting
4. **README.md** - Mention performance improvements

### Developer Documentation

1. **CLAUDE.md** - Document new environment variables
2. **docs/architecture.md** - Explain compression detection system
3. **CHANGELOG.md** - Document changes in v18.13.0

---

## Success Criteria

1. ✅ Filesystem detection works on Linux, macOS, Windows
2. ✅ Btrfs users automatically get FS compression + reflinks
3. ✅ XFS/APFS users get reflinks without app compression
4. ✅ ext4/NTFS users continue with app compression
5. ✅ Backward compatibility maintained
6. ✅ No regressions in correctness
7. ✅ Measurable performance improvements
8. ✅ Positive user feedback

---

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| Detection fails on some filesystems | Medium | Medium | Fallback to app compression |
| False positive (thinks FS has compression but doesn't) | High | Low | Thorough testing, conservative detection |
| Performance regression | High | Low | Benchmarks, beta testing |
| User confusion | Low | Medium | Clear documentation, good logging |
| Incompatibility with existing setups | Medium | Low | Backward compatibility, marker file still works |

---

## Future Enhancements

### Post-v18.13.0

1. **Application-level zstd** - Use zstd instead of gzip when FS doesn't compress
2. **Per-file-type strategies** - Different strategies for .py vs .so files
3. **Automatic optimization** - Detect FS changes and suggest rebuild
4. **Compression ratio reporting** - Show actual space savings
5. **Advanced reflink detection** - Detect if reflinks are actually working

---

## Questions to Resolve

### Before Implementation

- [ ] Should we use `sync.Once` or check filesystem on every call?
  - **Answer:** Use `sync.Once` for performance

- [ ] How to handle filesystem detection failures?
  - **Answer:** Fallback to app compression (conservative)

- [ ] Should we add telemetry for strategy selection?
  - **Answer:** Yes, but make it optional (respect existing telemetry settings)

- [ ] What about remote filesystems (NFS, CIFS)?
  - **Answer:** Always use app compression for network filesystems

### During Implementation

- [ ] Best way to detect ZFS on Linux?
- [ ] How to handle Docker overlay filesystems?
- [ ] Should we cache detection results?

---

## Timeline Summary

| Week | Phase | Deliverable |
|------|-------|-------------|
| 1 | Filesystem Detection | `htfs/filesystem*.go` files, unit tests |
| 2 | Smart Strategy | Modified `Compress()`, integration tests |
| 3 | CLI Commands | `rcc holotree compression`, documentation |
| 4 | Testing & Polish | Benchmarks, migration guide |
| 5 | Internal Testing | Bug fixes, performance tuning |
| 6 | Beta Release | Community feedback |
| 7 | Production Release | v18.13.0 launch |

**Total time:** ~7 weeks from start to production release

---

## Conclusion

This implementation plan provides a clear path to adding smart filesystem compression detection to RCC. The changes are:

1. **Backward compatible** - existing setups continue to work
2. **Performance-focused** - optimal strategy for each filesystem
3. **User-friendly** - automatic detection, clear reporting
4. **Well-tested** - comprehensive test coverage
5. **Documented** - clear documentation for users and developers

**Expected impact:**
- **Btrfs users:** Same disk usage, 20-40x faster environment restores
- **XFS/APFS users:** 2-3x more disk usage, 20-40x faster restores
- **ext4/NTFS users:** No change (continues to work as before)

---

## Approval & Sign-off

- [ ] Technical review by maintainers
- [ ] Performance benchmarks completed
- [ ] Documentation reviewed
- [ ] Testing plan approved
- [ ] Timeline confirmed
- [ ] Ready to implement

---

**Document Status:** Planning Complete, Ready for Review
**Next Step:** Technical review and approval to proceed with implementation
