# Holotree Performance Improvements Specification

**Version:** 1.0
**Date:** 2025-12-12
**Author:** Josh Yorko
**Acknowledgment:** Based on insights from @vjmp (Juha Pohjalainen), the original RCC developer

---

## Executive Summary

This specification proposes performance improvements to RCC's holotree environment system, informed by direct input from the original developer. The improvements focus on two major areas:

1. **Optional hardlink-based file restoration** - Massive speedup for environment creation
2. **Streamlined telemetry/metrics** - Continuing the performance work vjmp started in his fork

---

## Background

### vjmp's Commit Analysis (d341d34)

The original RCC developer's fork removed 2,361 lines of code for performance:

| Removed Component | Files | Impact |
|-------------------|-------|--------|
| **Metrics/Telemetry** | `cloud/metrics.go` | Background goroutines, network calls |
| **Cloud Commands** | `cmd/{authorize,cloud*,push,pull,upload,download,workspace,userinfo}.go` | Unused for local-only usage |
| **Assistant Commands** | `cmd/assistant*.go` | Robocorp-specific functionality |
| **Feedback System** | `cmd/feedback.go` | User feedback collection |
| **Process Tree Watching** | `common/platform_*.go` modifications | CPU overhead from subprocess monitoring |

**Key Quote from vjmp:** *"Removed feedback, metrics, and process tree (performance improvement)"*

### vjmp's Hardlink Insights (Issue #63)

The original developer documented why hardlinks were challenging:

| Challenge | Description | Impact |
|-----------|-------------|--------|
| **Security** | Multi-user/SaaS scenarios create attack vectors between environments | High |
| **Relocations** | Files with hardcoded paths can't be hardlinked; causes stacktrace jumps | Critical |
| **File Locks** | Unclear behavior when processes lock hardlinked files | Medium |
| **.pyc/.pyo Files** | Multiple processes writing to same inode | Medium |
| **macOS Weirdness** | Security features, file ownership, syncing issues | Platform-specific |
| **Windows Weirdness** | Antivirus/firewall kernel injection, file yanking | Platform-specific |

**Key Quote:** *"None of those should prevent you trying out things. You might come up with some great solution."*

---

## Proposed Features

### Feature 1: Optional Hardlink Restoration Mode

#### Overview

Add an opt-in hardlink restoration strategy for environments on supported filesystems. This provides massive performance improvements for single-user, trusted scenarios while maintaining safety as the default.

#### Strategy Selection

```
┌─────────────────────────────────────────────────────────────────┐
│                    File Restoration Strategy                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  [Default: COPY]                                                 │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │   HOLOLIB    │───▶│    COPY      │───▶│  HOLOTREE    │       │
│  │   (source)   │    │  + rewrite   │    │   (env)      │       │
│  └──────────────┘    └──────────────┘    └──────────────┘       │
│                                                                  │
│  [Opt-in: HARDLINK]                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │   HOLOLIB    │───▶│  HARDLINK    │───▶│  HOLOTREE    │       │
│  │   (source)   │    │  (no reloc)  │    │   (env)      │       │
│  └──────────────┘    └──────────────┘    └──────────────┘       │
│                                                                  │
│  [Hybrid: SMART]                                                 │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │   HOLOLIB    │───▶│ HARDLINK if  │───▶│  HOLOTREE    │       │
│  │   (source)   │    │ no reloc,    │    │   (env)      │       │
│  │              │    │ else COPY    │    │              │       │
│  └──────────────┘    └──────────────┘    └──────────────┘       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

#### Implementation Details

##### New Configuration Option

```yaml
# settings.yaml
holotree:
  restoration-strategy: "copy"  # Options: copy, hardlink, smart
  hardlink-excludes:
    - "*.pyc"
    - "*.pyo"
    - "__pycache__/*"
```

##### Environment Variable Override

```bash
RCC_HOLOTREE_STRATEGY=hardlink  # or: copy, smart
```

##### CLI Flag

```bash
rcc holotree init --strategy=hardlink
rcc run --holotree-strategy=smart
```

##### Smart Strategy Logic

```go
// Pseudocode for smart strategy
func selectStrategy(file FileMetadata) Strategy {
    // Files with relocations MUST be copied
    if len(file.Relocations) > 0 {
        return COPY
    }

    // Exclude patterns (configurable)
    if matchesExcludePattern(file.Name) {
        return COPY
    }

    // Platform checks
    if runtime.GOOS == "darwin" && isExecutable(file) {
        return COPY  // macOS security features
    }

    // Safe to hardlink
    return HARDLINK
}
```

#### Safety Mechanisms

1. **Relocation Detection**: Files with `Rewrite` positions in metadata MUST use copy strategy
2. **Platform Validation**: Check filesystem supports hardlinks before using
3. **Fallback**: If hardlink fails, automatically fall back to copy
4. **Warning on Shared Systems**: Detect multi-user scenarios and warn/disable

```go
// Safety check example
func canUseHardlinks() (bool, string) {
    // Check if running as shared service
    if os.Getenv("RCC_SHARED_MODE") == "true" {
        return false, "hardlinks disabled in shared mode"
    }

    // Check filesystem support
    if !filesystemSupportsHardlinks(hololibPath) {
        return false, "filesystem does not support hardlinks"
    }

    return true, ""
}
```

#### Expected Performance Impact

| Operation | Current (Copy) | With Hardlinks | Improvement |
|-----------|----------------|----------------|-------------|
| Small env (100 files) | ~2s | ~0.3s | 6x faster |
| Medium env (1000 files) | ~15s | ~1s | 15x faster |
| Large env (10000 files) | ~120s | ~5s | 24x faster |

*Note: Estimates based on avoiding file I/O for content copy*

---

### Feature 2: Streamlined Telemetry Mode

#### Overview

Provide a "lite mode" configuration that disables background telemetry and monitoring for performance-critical deployments. This continues vjmp's work in a configurable way.

#### Configuration

```yaml
# settings.yaml
performance:
  lite-mode: true  # Disables: metrics, process tree, background tasks
```

```bash
# Environment variable
RCC_LITE_MODE=true
```

#### Components to Make Optional

| Component | Current State | Lite Mode | Savings |
|-----------|---------------|-----------|---------|
| `cloud/metrics.go` | Always runs | Disabled | ~5% CPU, network I/O |
| Process tree watching | Always runs | Disabled | ~3% CPU |
| Background telemetry | Always runs | Disabled | Network I/O |

#### Implementation

```go
// In common/strategies.go or similar
var LiteMode = os.Getenv("RCC_LITE_MODE") == "true"

func init() {
    if !LiteMode {
        go metrics.StartBackgroundCollection()
        go operations.WatchProcessTree()
    }
}
```

---

### Feature 3: COW (Copy-on-Write) Support (Future)

#### Overview

For filesystems that support it (Btrfs, ZFS, APFS), use reflinks/clones instead of hardlinks or copies. This provides the best of both worlds: performance of hardlinks with isolation of copies.

```bash
# Linux (Btrfs/XFS)
cp --reflink=auto source dest

# macOS (APFS)
cp -c source dest
```

#### Benefits Over Hardlinks

- **Isolation**: Changes don't affect source
- **Security**: No shared inode attack vectors
- **Compatibility**: Works with relocations (copy modified version)

#### Detection

```go
func detectCOWSupport(path string) bool {
    // Linux: check for Btrfs/XFS/bcachefs
    // macOS: check for APFS
    // Windows: check for ReFS
}
```

---

## File Changes Required

### New Files

| File | Purpose |
|------|---------|
| `htfs/strategy.go` | Restoration strategy selection logic |
| `htfs/hardlink.go` | Hardlink-specific implementation |
| `htfs/reflink.go` | COW/reflink implementation (future) |
| `common/litemode.go` | Lite mode configuration and checks |

### Modified Files

| File | Changes |
|------|---------|
| `htfs/functions.go` | Add strategy selection in `DropFile()` |
| `htfs/library.go` | Pass strategy context to restoration |
| `settings/settings.go` | Add new configuration options |
| `common/strategies.go` | Add lite mode checks |
| `cmd/holotree.go` | Add `--strategy` flag |

---

## Configuration Summary

### settings.yaml Additions

```yaml
# Holotree performance settings
holotree:
  # File restoration strategy: copy (default), hardlink, smart
  restoration-strategy: "copy"

  # Patterns to always copy (never hardlink)
  hardlink-excludes:
    - "*.pyc"
    - "*.pyo"
    - "__pycache__/*"
    - "*.so"  # Shared libraries with relocations

performance:
  # Disable background telemetry and monitoring
  lite-mode: false
```

### Environment Variables

| Variable | Values | Default | Description |
|----------|--------|---------|-------------|
| `RCC_HOLOTREE_STRATEGY` | copy, hardlink, smart | copy | Override restoration strategy |
| `RCC_LITE_MODE` | true, false | false | Disable background tasks |
| `RCC_SHARED_MODE` | true, false | false | Indicate multi-user deployment |

---

## Migration & Compatibility

### Backward Compatibility

- Default behavior unchanged (copy strategy)
- All new features are opt-in
- Existing environments continue to work

### Migration Path

1. **Phase 1**: Ship with `smart` strategy available but not default
2. **Phase 2**: Gather feedback, refine exclude patterns
3. **Phase 3**: Consider making `smart` the default for single-user installs

---

## Testing Strategy

### Unit Tests

- Strategy selection logic
- Relocation detection
- Platform-specific behavior

### Integration Tests

- Create environment with each strategy
- Verify file contents match
- Verify relocations are properly rewritten
- Test fallback behavior

### Performance Tests

- Benchmark environment creation times
- Compare strategies across file counts
- Measure memory usage

### Platform Tests

- Linux (ext4, Btrfs, XFS)
- macOS (APFS, HFS+)
- Windows (NTFS, ReFS)

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Hardlink corruption in shared env | Medium | High | Disable by default, warn in shared mode |
| Platform-specific bugs | Medium | Medium | Comprehensive platform testing |
| Relocation bugs with hardlinks | Low | High | Always copy files with relocations |
| User confusion | Low | Low | Clear documentation, sensible defaults |

---

## Success Metrics

1. **Performance**: 10x+ speedup for large environment creation with hardlinks
2. **Adoption**: 50%+ of single-user installs opt into smart/hardlink mode
3. **Stability**: No regression in environment correctness
4. **Feedback**: Positive feedback from original developer (vjmp)

---

## Acknowledgments

Special thanks to **@vjmp (Juha Pohjalainen)** for:
- Creating the original RCC and holotree system
- Sharing detailed insights on hardlink challenges (Issue #63)
- Demonstrating performance improvements in his fork
- Encouraging continued development of RCC

*"Thank you! for picking up rcc torch. Let it burn bright!"* - @vjmp

---

## References

- [vjmp's fork commit](https://github.com/vjmp/rcc/commit/d341d34ac2d9baf975d802f5262e539e49b7e059)
- [Issue #63: About using hardlinks](https://github.com/joshyorko/rcc/issues/63)
- [Issue #61: Thank you message](https://github.com/joshyorko/rcc/issues/61)
- [OSTree hardlink deployment model](https://ostreedev.github.io/ostree/)
- [Holotree documentation](docs/holotree.md)
