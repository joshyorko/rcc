# BLAZINGLY FAST: The Ultimate Holotree Performance Solution

**Revised approach: Keep compression, replace the algorithm**

**Version:** 2.2
**Date:** 2025-12-13
**Core Principle:** Compression is non-negotiable. Speed gains must work for ALL robots.

---

## ⚠️ Implementer Responsibility (Read First)

> **From vjmp (original author):**
> *"Important question: do you understand all those proposed improvements, or is it AI that only understands those improvements? Like all three OS optimizations and their filesystem variations?*
>
> *There are easy to understand trade-offs (compression: space vs. time; but with rcc, people easily run out of diskspace). Making OS specific FS optimizations on syscall levels brings in multiple trade-offs (like how antivirus affects those; maintenance burden; testing responsibility on different OS/FS combos; enough hands to do testing/profiling before/after changes).*
>
> *Important thing to remember: If you break it, you own the pieces, AI does not."*

**Before implementing ANY part of this spec, you MUST:**

1. **Understand the code you're changing** - Read `htfs/functions.go`, `htfs/delegates.go`, `htfs/directory.go` thoroughly
2. **Profile before changes** - Use `--pprof` to establish baselines
3. **Test on YOUR systems** - You cannot rely on AI to test edge cases
4. **Own the maintenance** - You will fix bugs, not the AI
5. **Start small** - Phase 1 (zstd) only. Do NOT implement Phases 2-3 until Phase 1 is proven.

**Risk assessment by phase:**

| Phase | Risk | Recommendation |
|-------|------|----------------|
| **Phase 1: zstd** | LOW | ✅ Implement - well-understood, proven library |
| **Phase 2: Hash optimization** | UNKNOWN | ⏸️ Profile first - may not be needed after Phase 1 |
| **Phase 3: Reflinks** | HIGH | ❌ Future - OS/FS complexity |

---

## Executive Summary

After comprehensive analysis of:
- vjmp's insights (original developer) - see [STARTING_POINT.md](STARTING_POINT.md)
- The PDF performance document - see [Accelerating RCC Holotree Environment Performance.pdf](Accelerating%20RCC%20Holotree%20Environment%20Performance.pdf)
- OSTree/Nix/Docker architectures
- uv/pixi speed techniques (uv caches both compressed AND uncompressed)
- The actual RCC codebase (`htfs/functions.go`, `htfs/delegates.go`)
- Compression algorithm benchmarks

**The REAL solution: Replace gzip with zstd - 3x faster decompression while KEEPING compression.**

**Key Insight:** ~60-70% of files in typical Python environments have NO relocations and can benefit from all optimizations.

---

## Critical: Profile Before and After (--pprof)

> **From vjmp:** *"Note on all optimizations. Always profile, before and after. Have you noticed --pprof option in rcc? It is there for a reason ..."*

### How --pprof Works

RCC uses Go's standard `runtime/pprof` for CPU profiling (see `cmd/root.go:145-152`):

```go
// When --pprof is passed:
sink, err := pathlib.Create(profilefile)
err = pprof.StartCPUProfile(sink)  // Start recording
// ... command executes ...
pprof.StopCPUProfile()  // Stop and write to file
```

### Step-by-Step Profiling Guide

**1. Create a reproducible test environment:**

```bash
# Clean hololib for fresh baseline
rm -rf ~/.robocorp/hololib

# Create test robot with significant dependencies
cat > conda.yaml << 'EOF'
channels:
  - conda-forge
dependencies:
  - python=3.11
  - numpy
  - pandas
EOF
```

**2. Profile environment CREATION (LiftFile path):**

```bash
rcc task run -r robot.yaml --pprof profile_create.pprof
```

**3. Profile environment RESTORE (DropFile path):**

```bash
# Force restore by clearing holotree
rm -rf ~/.robocorp/holotree/*
rcc task run -r robot.yaml --pprof profile_restore.pprof
```

**4. Analyze the profiles:**

```bash
# Interactive web view with flame graphs (BEST)
go tool pprof -http=:8080 profile_restore.pprof

# Text summary
go tool pprof profile_restore.pprof
(pprof) top20           # Top 20 functions by CPU
(pprof) list DropFile   # Line-by-line for DropFile

# Compare before/after
go tool pprof -base profile_before.pprof profile_after.pprof
```

**5. What to look for in the profile:**

```
Current (gzip):
├── compress/gzip.(*Reader).Read    ← Decompression (target!)
├── crypto/sha256.block             ← Hash computation
├── syscall.write                   ← File I/O
└── runtime.memmove                 ← Memory copying

After zstd:
├── zstd.(*Decoder).DecodeAll       ← Should be faster
├── crypto/sha256.block             ← Same
├── syscall.write                   ← Same
└── runtime.memmove                 ← Same
```

**DO NOT assume improvements - MEASURE them.**

---

## Required Dependency: github.com/klauspost/compress

### Why This Library?

| Criteria | klauspost/compress | CGO Alternatives (DataDog/zstd, valyala/gozstd) |
|----------|-------------------|------------------------------------------------|
| **Pure Go** | ✅ Yes | ❌ No (requires C compiler) |
| **Cross-compile** | ✅ `go build` just works | ❌ Complex toolchain setup |
| **License** | ✅ BSD/MIT/Apache (all permissive) | ✅ BSD/MIT |
| **Adoption** | ✅ 2,131+ importers | ~500 importers |
| **Used by** | Docker, MinIO, Prometheus, CockroachDB, etcd, Grafana | VictoriaMetrics |
| **Maintained** | ✅ Klaus Post (Go stdlib contributor) | ✅ Active |

### Why NOT CGO?

RCC cross-compiles to Linux, macOS, and Windows across multiple architectures. CGO would require:
- Platform-specific C compilers for each target
- Complex CI/CD matrix
- Static linking challenges
- Larger binary sizes

**klauspost/compress is the de-facto standard for high-performance compression in Go.**

### Installation

```bash
go get github.com/klauspost/compress@latest
```

### Add to go.mod

```go
require (
    github.com/klauspost/compress v1.17.0  // or latest
)
```

---

## The Real Bottleneck Discovery

### What We Assumed
Files are copied slowly → need reflinks/hardlinks → must disable compression

### What's Actually Happening

```
DropFile() bottlenecks (in order of impact):

1. GZIP DECOMPRESSION     - Files stored compressed in hololib (~300 MB/s)
2. HASH VERIFICATION      - Re-computing full file hash on EVERY restore
3. PATH REWRITING         - Seeking and writing relocations
4. FILE COPY              - Actually the SMALLEST bottleneck!
```

### The Breakthrough Insight

**We don't need to disable compression - we need FASTER compression!**

| Algorithm | Decompression Speed | Compression Ratio |
|-----------|---------------------|-------------------|
| gzip (current) | ~300 MB/s | Good |
| **zstd** | **786-1024 MB/s** | Similar or better |
| lz4 | ~3000 MB/s | Lower |

**zstd gives us 3x faster decompression with NO loss in compression ratio.**

---

## Critical Constraint: Relocations

**From vjmp (Issue #63):** *"files that have relocations in them cannot be hardlinked from one environment to next, or some code will end up running/accessing/modifying things on wrong environment/directory... we saw robots (stacktraces) jumping from one environment to next"*

**Good news:** RCC already tracks this! Each file in the catalog has a `Rewrite` field:

```go
// htfs/directory.go
type File struct {
    Name    string   `json:"name"`
    Digest  string   `json:"digest"`
    Rewrite []int64  `json:"rewrite"`  // Relocation offsets - if len > 0, MUST copy
    // ...
}
```

**The rule is simple:**
- `len(file.Rewrite) > 0` → File has relocations → MUST decompress and copy (with path rewriting)
- `len(file.Rewrite) == 0` → File has no relocations → Can use all fast-path optimizations

This distinction is already used in `htfs/functions.go:330-349` (`CalculateTreeStats`).

---

## The Three-Phase Solution

### Phase 1: Replace gzip with zstd (PRIMARY - BIGGEST WIN)

**This is the key change. It preserves compression while delivering massive speed gains.**

```go
// BEFORE: htfs/delegates.go using compress/gzip
import "compress/gzip"

reader, err = gzip.NewReader(source)

// AFTER: using github.com/klauspost/compress/zstd
import "github.com/klauspost/compress/zstd"

decoder, _ := zstd.NewReader(source)
defer decoder.Close()
```

**For writing (LiftFile):**
```go
// BEFORE: htfs/functions.go:238-240
writer, err = gzip.NewWriterLevel(sink, gzip.BestSpeed)

// AFTER:
encoder, err := zstd.NewWriter(sink, zstd.WithEncoderLevel(zstd.SpeedFastest))
defer encoder.Close()
```

**Impact:**
- 3x faster decompression (786-1024 MB/s vs ~300 MB/s)
- Similar or better compression ratios
- **Works for ALL robots** - not just dev mode
- **No disk space trade-off**
- **Compression remains a core design principle**

**Go Library:** `github.com/klauspost/compress/zstd`
- Pure Go implementation
- Drop-in streaming support
- Concurrent decompression support
- Well-maintained, widely used

### Phase 2: Hash Verification Optimization (PROFILE FIRST)

**Status: CONTINGENT ON PROFILING RESULTS**

We don't know if hash verification is a bottleneck until we profile after Phase 1 (zstd).

#### The Math: Is This Even a Problem?

| Component | Speed | Notes |
|-----------|-------|-------|
| zstd decompression | ~1000 MB/s | New bottleneck after Phase 1 |
| SHA256 (with SHA-NI) | ~1500 MB/s | Hardware acceleration on modern CPUs |
| SHA256 (software) | ~500 MB/s | Fallback on older CPUs |
| Disk I/O (SSD) | ~500-3000 MB/s | Varies widely |

**Key insight:** Hash verification happens via `io.MultiWriter` - it computes the hash WHILE copying, not as a separate pass. On CPUs with SHA-NI (Intel 2016+, AMD 2017+), SHA256 is likely **faster than zstd decompression**.

**Action required:** After implementing Phase 1, profile with `--pprof` to determine:
1. What percentage of restore time is `crypto/sha256.block`?
2. Is SHA-NI being used? (Look for `sha256block` vs `sha256blockAvx2`)

#### If Profiling Shows Hash IS a Bottleneck (>20% of time)

**Contingency A: Verify SHA-NI is Active (Zero Dependencies)**

Go's `crypto/sha256` auto-detects hardware acceleration. First verify it's working:

```go
// Add diagnostic to check SHA-NI usage
import "crypto/sha256"
import _ "crypto/sha256" // triggers init that detects SHA-NI

// In profile, look for:
// sha256blockAvx2 or sha256block_ni = hardware accelerated ✅
// sha256block (generic) = software fallback ❌
```

If SHA-NI isn't being used, investigate why before adding dependencies.

**Contingency B: BLAKE3 (Adds 2 Dependencies)**

| Pro | Con |
|-----|-----|
| 4+ GB/s - definitely faster than zstd | Adds `github.com/zeebo/blake3` |
| Well-audited algorithm | Adds `github.com/klauspost/cpuid/v2` |
| | Two more packages to audit and maintain |
| | Storage format change (must store both hashes) |

**Implementation if chosen:**
```go
type File struct {
    Digest  string   `json:"digest"`        // SHA256 - storage identity (unchanged)
    Blake3  string   `json:"blake3,omitempty"` // Fast verification (new, optional)
    // ...
}
```

Only pursue this if:
1. Profiling proves SHA256 is >20% of restore time
2. SHA-NI is confirmed active (ruling out Contingency A)
3. The 2 additional dependencies are acceptable

**Contingency C: Optional Verification Skip (⚠️ SECURITY RISK)**

> **⚠️ WARNING from vjmp (original author):**
> *"This proposal is kind of dangerous, because:*
> - *if hololib has shared access (mounted from host machine; used from shared disk location), then someone can modify file, and if integrity is not checked, attacker has access*
> - *writes to hololib could fail and corrupt file when it is coming in*
> - *disks can corrupt (for example NFS mounts)*
> - *so both accidental and intentional corruption can happen in local hololib (on rest)"*

**Why verification exists (security boundary):**

| Threat | Scenario | Without Verification |
|--------|----------|---------------------|
| **Shared access attack** | Hololib on NFS/shared mount | Attacker can inject malicious code |
| **Write corruption** | Power loss during LiftFile() | Silent data corruption |
| **Disk corruption** | Bad sectors, NFS timeouts | Broken environments |
| **Supply chain** | Tampered cached packages | Security breach |

**If still considering after ruling out A and B (isolated local environments ONLY):**

```go
// DANGER: Only for isolated, single-user, local-disk environments
// NOT for: NFS, shared mounts, CI/CD, production, containers
if common.VerifyOnRestore() {
    // Current path with verification (KEEP AS DEFAULT)
    digester := common.NewDigester(...)
    many := io.MultiWriter(sink, digester)
    io.Copy(many, reader)
} else {
    // DANGEROUS: Skip verification
    io.Copy(sink, reader)
}
```

**If implemented, require explicit opt-in with warnings:**
- `RCC_VERIFY_ON_RESTORE=true` (default, KEEP THIS)
- `RCC_VERIFY_ON_RESTORE=false` - **DANGEROUS**, only for isolated local environments

#### Phase 2 Decision Tree

```
After Phase 1 is complete:
        │
        ▼
Profile with --pprof
        │
        ▼
Is crypto/sha256 >20% of restore time?
        │
    NO ──────► STOP. Phase 2 not needed. Ship Phase 1.
        │
       YES
        │
        ▼
Is SHA-NI being used?
        │
    NO ──────► Investigate why. Fix if possible. Re-profile.
        │
       YES
        │
        ▼
Are 2 extra dependencies acceptable?
        │
    YES ─────► Contingency B: Add BLAKE3
        │
    NO
        │
        ▼
Is security trade-off acceptable for your use case?
        │
    YES ─────► Contingency C: Optional skip flag (with warnings)
        │
    NO ──────► Accept current performance. Ship Phase 1 only.
```

**Expected impact:** Unknown until profiling. Could be 0% (not a bottleneck) to 20-30% (if it is)

### Phase 3: Reflinks for Uncompressed Mode (FUTURE CONSIDERATION - HIGH COMPLEXITY)

> **⚠️ WARNING from vjmp:**
> *"Making OS specific FS optimizations on syscall levels brings in multiple trade-offs:*
> - *how antivirus affects those*
> - *maintenance burden*
> - *testing responsibility on different OS/FS combos*
> - *enough hands to do testing/profiling before/after changes*
>
> *Important thing to remember: If you break it, you own the pieces, AI does not."*

**Status: FUTURE CONSIDERATION - Not recommended for initial implementation.**

**Why this is complex:**

| OS | Syscall | Filesystems | Edge Cases |
|----|---------|-------------|------------|
| Linux | FICLONE ioctl | Btrfs, XFS (4.1+) | Antivirus hooks, SELinux, AppArmor |
| macOS | clonefile(2) | APFS only | Gatekeeper, code signing, quarantine |
| Windows | FSCTL_DUPLICATE_EXTENTS | ReFS only | Antivirus, NTFS doesn't support it |

**Questions you MUST be able to answer before implementing:**
1. What happens when antivirus intercepts FICLONE?
2. How does macOS quarantine attribute affect clonefile?
3. What's the fallback when reflink fails silently?
4. How do you test this on CI without the actual filesystems?
5. What's the maintenance burden for 3 OS × multiple FS combinations?

**If you cannot answer these confidently, DO NOT IMPLEMENT.**

**For users who absolutely want this (and understand the risks):**

```go
// FUTURE: Only for users who explicitly enable AND understand trade-offs
// Requires: uncompressed mode + CoW filesystem + no relocations
if len(details.Rewrite) == 0 && !Compress() && common.ReflinkEnabled() {
    srcPath := library.ExactLocation(digest)
    if TryReflink(srcPath, sinkname) {
        os.Chmod(sinkname, details.Mode)
        os.Chtimes(sinkname, motherTime, motherTime)
        return  // INSTANT!
    }
    // Fallback to normal copy - reflink failed silently
}
```

**Recommendation: Focus on Phase 1 (zstd) first. It provides 3x improvement with LOW risk.**

---

## Migration Strategy

### Backward Compatibility Matrix

| Component | Old RCC reads New | New RCC reads Old | Write Format |
|-----------|-------------------|-------------------|--------------|
| **Hololib files** | ❌ No | ✅ Yes (dual-format) | zstd |
| **Catalog files** | ❌ No | ✅ Yes (dual-format) | zstd |
| **ziplibrary (bundles)** | ✅ Yes | ✅ Yes | gzip (unchanged) |
| **Micromamba blob** | ✅ Yes | ✅ Yes | gzip (unchanged) |

**Key insight:** Old RCC cannot read new zstd files, but this is acceptable because:
- Users upgrade RCC, not downgrade
- Hololib/catalogs are local caches, easily regenerated
- The performance gain justifies the one-way migration

### Dual-Format Detection (Magic Bytes)

Rather than try-fail, use magic byte detection for reliability:

```go
// Magic bytes for format detection
var (
    gzipMagic = []byte{0x1f, 0x8b}           // gzip header
    zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd} // zstd frame magic
)

func detectFormat(r io.ReadSeeker) (string, error) {
    header := make([]byte, 4)
    _, err := r.Read(header)
    if err != nil {
        return "raw", err
    }
    r.Seek(0, 0) // Reset for actual reading

    if bytes.HasPrefix(header, zstdMagic) {
        return "zstd", nil
    }
    if bytes.HasPrefix(header, gzipMagic) {
        return "gzip", nil
    }
    return "raw", nil
}
```

### Hololib Files (htfs/delegates.go)

**Current code:**
```go
func gzDelegateOpen(filename string, ungzip bool) (io.Reader, Closer, error) {
    source, err := os.Open(filename)
    // ... tries gzip, falls back to raw
}
```

**New code with dual-format support:**
```go
func openCompressed(filename string) (io.Reader, Closer, error) {
    source, err := os.Open(filename)
    if err != nil {
        return nil, nil, err
    }

    format, _ := detectFormat(source)

    switch format {
    case "zstd":
        decoder, err := zstd.NewReader(source)
        if err != nil {
            source.Close()
            return nil, nil, err
        }
        return decoder, func() error {
            decoder.Close()
            return source.Close()
        }, nil

    case "gzip":
        reader, err := gzip.NewReader(source)
        if err != nil {
            source.Close()
            return nil, nil, err
        }
        return reader, func() error {
            reader.Close()
            return source.Close()
        }, nil

    default: // raw
        return source, func() error { return source.Close() }, nil
    }
}
```

### Catalog Files (htfs/directory.go)

**Challenge:** Catalogs are also gzip-compressed. Same solution applies.

**Current code (htfs/directory.go:340-354):**
```go
func (it *Root) LoadFrom(filename string) error {
    source, err := os.Open(filename)
    // ...
    reader, err := gzip.NewReader(source)  // Only handles gzip!
    // ...
}
```

**New code with dual-format support:**
```go
func (it *Root) LoadFrom(filename string) error {
    source, err := os.Open(filename)
    if err != nil {
        return err
    }
    defer source.Close()

    format, _ := detectFormat(source)

    var reader io.ReadCloser
    switch format {
    case "zstd":
        decoder, err := zstd.NewReader(source)
        if err != nil {
            return err
        }
        reader = io.NopCloser(decoder)
        defer decoder.Close()

    case "gzip":
        gzReader, err := gzip.NewReader(source)
        if err != nil {
            return err
        }
        reader = gzReader
        defer gzReader.Close()

    default:
        reader = io.NopCloser(source)
    }

    it.source = filename
    defer common.Timeline("holotree catalog %q loaded", filename)
    return it.ReadFrom(reader)
}
```

**For writing (htfs/directory.go:312-333):**
```go
func (it *Root) SaveAs(filename string) error {
    content, err := it.AsJson()
    if err != nil {
        return err
    }
    sink, err := pathlib.Create(filename)
    if err != nil {
        return err
    }
    defer sink.Close()
    defer sink.Sync()

    // NEW: Write zstd instead of gzip
    encoder, err := zstd.NewWriter(sink, zstd.WithEncoderLevel(zstd.SpeedFastest))
    if err != nil {
        return err
    }
    defer encoder.Close()

    _, err = encoder.Write(content)
    if err != nil {
        return err
    }
    return it.Info.saveAs(filename + ".info")
}
```

### Files That MUST Stay gzip (Do Not Change)

| File | Function | Reason |
|------|----------|--------|
| `htfs/ziplibrary.go:62` | `openFile()` | Pre-built zip bundles use gzip internally |
| `conda/installing.go:31` | `GunzipWrite()` | Embedded micromamba is gzip compressed |

These are external formats we don't control.

### Migration Path

1. **Deploy new RCC** with dual-format read support
2. **All new files** written as zstd (hololib + catalogs)
3. **Old gzip files** read transparently via magic byte detection
4. **Optional:** `rcc holotree migrate --to-zstd` command to recompress existing files
5. **Use existing:** `rcc holotree check` already exists to verify integrity after migration

---

## Platform-Specific Reflink Implementations

*Preserved from original spec for optional uncompressed mode.*

### Linux (Btrfs, XFS)

```go
//go:build linux

const FICLONE = 0x40049409

func TryReflink(src, dst string) bool {
    srcFile, err := os.Open(src)
    if err != nil { return false }
    defer srcFile.Close()

    dstFile, err := os.Create(dst)
    if err != nil { return false }
    defer dstFile.Close()

    _, _, errno := unix.Syscall(unix.SYS_IOCTL, dstFile.Fd(), FICLONE, srcFile.Fd())
    return errno == 0
}
```

### macOS (APFS)

```go
//go:build darwin

func TryReflink(src, dst string) bool {
    srcPtr, _ := syscall.BytePtrFromString(src)
    dstPtr, _ := syscall.BytePtrFromString(dst)

    _, _, errno := syscall.Syscall(462,
        uintptr(unsafe.Pointer(srcPtr)),
        uintptr(unsafe.Pointer(dstPtr)),
        0)
    return errno == 0
}
```

### Windows (ReFS)

```go
//go:build windows

func TryReflink(src, dst string) bool {
    // FSCTL_DUPLICATE_EXTENTS_TO_FILE for ReFS
    // Falls back to CopyFileEx otherwise
    // ... implementation ...
}
```

---

## The Complete Strategy Matrix

| Condition | Strategy | Speed vs Current |
|-----------|----------|------------------|
| Symlink | Direct symlink restore | Same (instant) |
| **zstd compressed (new default)** | **zstd decompress + copy** | **3x faster** |
| zstd + skip verification | zstd decompress + direct copy | **4x faster** |
| gzip compressed (legacy) | gzip decompress + copy | Same |
| No compression + CoW FS | REFLINK | 10-60x faster |
| No compression + !CoW | copy_file_range | 2-3x faster |
| Has relocations | Decompress + copy + rewrite | Same (required) |

---

## Configuration Options

### Environment Variables

```bash
# Verification control (default: true)
export RCC_VERIFY_ON_RESTORE=false  # Skip hash verification for trusted environments

# Optional: Force specific compression format for new files
export RCC_COMPRESSION_FORMAT=zstd  # zstd (default), gzip (legacy), none

# Reflink strategy (only applies when compression disabled)
export RCC_HOLOTREE_STRATEGY=auto  # auto, reflink, copy
```

### settings.yaml

```yaml
holotree:
  # Compression algorithm (zstd recommended)
  compression-format: zstd  # zstd, gzip, none

  # Verification on restore (disable for trusted local environments)
  verify-on-restore: true

  # Reflink strategy (only when compression-format: none)
  strategy: auto  # auto, reflink, copy

  # Files that should always be copied (not reflinked)
  copy-patterns:
    - "*.pyc"
    - "*.pyo"
    - "__pycache__/*"
```

### CLI Flags

```bash
# Normal operation (uses zstd, verifies)
rcc run

# Skip verification for trusted environment
rcc run --skip-verify

# Migration command
rcc holotree migrate --to-zstd
```

---

## Expected Performance Gains

### Environment Restoration Time

| Scenario | gzip (current) | zstd | zstd + skip verify | No compression + reflinks |
|----------|----------------|------|-------------------|---------------------------|
| Small (100 files) | 2s | 0.7s | 0.5s | 0.1s |
| Medium (1000 files) | 15s | 5s | 4s | 0.5s |
| Large (10000 files) | 120s | 40s | 30s | 2s |

**zstd alone: 3x faster**
**zstd + skip verify: 4x faster**
**No compression + reflinks: 10-60x faster (optional)**

### Disk Space (Unchanged!)

| Format | Typical Size | Speed |
|--------|--------------|-------|
| gzip (current) | 1x | Baseline |
| **zstd (new default)** | **~1x** | **3x faster** |
| None (optional) | 2-3x | 10-60x faster |

**Key insight:** zstd achieves similar compression ratios to gzip while decompressing 3x faster.

---

## Files to Modify

### Must Change (Phase 1)

| File | Changes |
|------|---------|
| `go.mod` | Add `github.com/klauspost/compress` dependency |
| `htfs/delegates.go` | Add `detectFormat()`, update `gzDelegateOpen()` for dual-format read |
| `htfs/functions.go` | Update `LiftFile()` to write zstd, update `CheckHasher()` for dual-format |
| `htfs/directory.go` | Update `LoadFrom()` for dual-format read, `SaveAs()` to write zstd |

### Should Change (Phase 2)

| File | Changes |
|------|---------|
| `htfs/functions.go` | Add verification skip option to `DropFile()` |
| `common/variables.go` | Add `RCC_VERIFY_ON_RESTORE` environment variable |

### Optional (Phase 3+)

| File | Changes |
|------|---------|
| `htfs/library.go` | Add compression format configuration |
| `pathlib/reflink_*.go` | New files based on [REFLINK_EXAMPLE.go](REFLINK_EXAMPLE.go) |
| `cmd/holotree.go` | Add `migrate --to-zstd` subcommand (`check` already exists) |

### Do NOT Change

| File | Reason |
|------|--------|
| `htfs/ziplibrary.go` | Pre-built zip bundles use gzip internally |
| `conda/installing.go` | Embedded micromamba blob is gzip compressed |

---

## Implementation Priority

### Phase 1: zstd Migration (Highest Impact, Lowest Risk)

1. Add `github.com/klauspost/compress` dependency
2. Update `htfs/delegates.go` with dual-format read support
3. Update `htfs/functions.go` LiftFile to write zstd
4. Test with existing hololib (should read gzip, write zstd)
5. Benchmark improvements

### Phase 2: Verification Skip (⚠️ NOT RECOMMENDED - Security Risk)

> **vjmp warned this is dangerous.** See Phase 2 section above for full explanation.

**If you still want to proceed (not recommended):**
1. Add `RCC_VERIFY_ON_RESTORE` environment variable
2. Modify DropFile to check verification setting
3. **REQUIRE explicit opt-in** with clear warnings
4. Document security implications thoroughly

### Phase 3: Reflink Support (❌ FUTURE - High Complexity)

> **vjmp warned about OS/FS complexity.** Only proceed if you can answer the questions in Phase 3 section above.

**Reference implementations exist but are NOT production-ready:**
- [REFLINK_EXAMPLE.go](REFLINK_EXAMPLE.go) - Example code only
- [FILESYSTEM_DETECTION_EXAMPLE.go](FILESYSTEM_DETECTION_EXAMPLE.go) - Example code only

**DO NOT implement until Phase 1 is proven and you understand:**
- Antivirus interaction with FICLONE
- macOS code signing and quarantine
- Windows ReFS limitations
- Fallback behavior on failure

### Phase 4: Migration Tooling (After Phase 1 is stable)

**Note:** `rcc holotree check` already exists with `--retries/-r` flag.

1. Add `rcc holotree migrate --to-zstd` command
2. Progress reporting for large hololibs
3. Verify existing `rcc holotree check` meets post-migration needs

---

## Addressing User Concerns

| Concern | Solution |
|---------|----------|
| **Compression must stay** | zstd keeps compression, just faster |
| **Not just dev mode** | zstd works for ALL robots |
| **No disk trade-off** | zstd has similar ratios to gzip |
| **UV-like speed** | 3-4x faster gets us much closer |
| **Migration** | Dual-format support, gradual transition |

---

## The Ultimate Fast Path

```
┌─────────────────────────────────────────────────────────────────┐
│                    DropFile() Decision Tree                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Is it a symlink? ────YES────► restoreSymlink() [INSTANT]       │
│        │                                                         │
│        NO                                                        │
│        │                                                         │
│  Has relocations? ────YES────► Decompress + copy + rewrite      │
│        │                                                         │
│        NO                                                        │
│        │                                                         │
│  Compression disabled? ──YES──► TryReflink() or copy [FASTEST]  │
│        │                                                         │
│        NO (compressed - default)                                 │
│        │                                                         │
│  Format detection ────zstd────► zstd decompress [3x FASTER]     │
│        │                                                         │
│       gzip (legacy)                                              │
│        │                                                         │
│  gzip decompress [current speed]                                 │
│                                                                  │
│  ────────────────────────────────────────────────────────────   │
│                                                                  │
│  Verify hash? ────YES────► Hash while copying (current)         │
│        │                                                         │
│        NO (RCC_VERIFY_ON_RESTORE=false)                          │
│        │                                                         │
│  Direct copy [20-30% faster]                                     │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Future Optimizations (Beyond This Spec)

These optimizations from the PDF and uv/pixi research could provide additional gains:

### Incremental Updates
Currently RCC recreates environments from scratch. Pixi achieves ~3x speedup over Micromamba by only copying files that changed between versions.

### Pre-resolved Environments
Using lockfiles (`conda.lock`, `pixi.lock`) eliminates solver time entirely. RCC already supports `conda.yaml` but could benefit from stricter lockfile support.

### Parallel Decompression
The klauspost/compress zstd library supports concurrent decompression. For very large files, this could provide additional speedup.

### UV's Dual-Cache Pattern
UV maintains BOTH compressed `.whl` files AND an uncompressed cache. Restores use reflinks from the uncompressed cache. This provides maximum speed but doubles storage. Could be an opt-in "turbo mode" for users with abundant disk space.

*These are documented here for future consideration but are out of scope for the initial implementation.*

---

## Conclusion

The path to BLAZINGLY FAST holotree **while keeping compression**:

1. **Replace gzip with zstd** - 3x faster decompression, same compression ratio
2. **Optional: Skip verification** - Additional 20-30% for trusted environments
3. **Optional: Use reflinks** - For users who choose uncompressed mode
4. **Smart fallbacks** - Transparent legacy gzip support

**Result: 3-4x faster environment creation for ALL robots, no trade-offs**

---

## Why This Approach is Better

| "Disable Compression" Approach | zstd Approach (This Spec) |
|--------------------------------|---------------------------|
| Disable compression for speed | Keep compression, use faster algorithm |
| Dev mode only | Works for ALL robots |
| 2-3x more disk space | No disk space increase |
| Requires CoW filesystem for best gains | Works on all filesystems |
| Breaking change | Backward compatible |

---

## References

### Internal Documentation
- [STARTING_POINT.md](STARTING_POINT.md) - vjmp's insights and the key `file.Rewrite` discovery
- [HOLOTREE_IMPROVEMENTS_SPEC.md](HOLOTREE_IMPROVEMENTS_SPEC.md) - Original hardlink/reflink spec
- [Accelerating RCC Holotree Environment Performance.pdf](Accelerating%20RCC%20Holotree%20Environment%20Performance.pdf) - Deep technical analysis

### External Resources
- [klauspost/compress](https://github.com/klauspost/compress) - Go compression library with zstd support
- [Zstandard](https://facebook.github.io/zstd/) - Facebook's compression algorithm
- [Zstd benchmarks](https://facebook.github.io/zstd/#benchmarks) - Performance comparison data
- [uv](https://github.com/astral-sh/uv) - Fast Python package installer (dual-cache architecture)

### RCC Source Files
- `htfs/functions.go:259-303` - `DropFile()` function (main restore path)
- `htfs/functions.go:223-257` - `LiftFile()` function (main store path)
- `htfs/delegates.go` - Compression delegates (gzip currently)
- `htfs/directory.go` - File struct with `Rewrite` field

---

## Implementation Checklist

### Phase 1: zstd Migration (✅ RECOMMENDED - Low Risk)

**Do this first. Profile before and after with `--pprof`.**

- [ ] **PROFILE FIRST:** Run `rcc run --pprof baseline.pprof` to establish baseline
- [ ] Add `github.com/klauspost/compress` to `go.mod`
- [ ] Add `detectFormat()` helper function with magic byte detection
- [ ] Update `htfs/delegates.go` - dual-format read for hololib files
- [ ] Update `htfs/functions.go` - `LiftFile()` writes zstd
- [ ] Update `htfs/functions.go` - `CheckHasher()` dual-format read
- [ ] Update `htfs/directory.go` - `LoadFrom()` dual-format read for catalogs
- [ ] Update `htfs/directory.go` - `SaveAs()` writes zstd for catalogs
- [ ] Test: New RCC reads old gzip hololib
- [ ] Test: New RCC reads old gzip catalogs
- [ ] Test: New RCC writes zstd, reads it back
- [ ] **PROFILE AFTER:** Run `rcc run --pprof after.pprof` and compare

### Phase 2: Hash Verification Optimization (⏸️ PROFILE FIRST)

**Do NOT start until Phase 1 is complete and profiled.**

- [ ] **PROFILE:** After Phase 1, run `--pprof` and analyze `crypto/sha256` time
- [ ] **DECISION POINT:** Is SHA256 >20% of restore time?
  - If NO → Phase 2 not needed. Ship Phase 1 only.
  - If YES → Continue below
- [ ] Check if SHA-NI is active (look for `sha256block_ni` or `sha256blockAvx2` in profile)
- [ ] **CONTINGENCY A:** If SHA-NI not active, investigate and fix
- [ ] **CONTINGENCY B:** If SHA-NI active but still slow, consider BLAKE3 (adds 2 dependencies)
- [ ] **CONTINGENCY C:** If dependencies unacceptable, consider optional skip flag (security risk)

### Phase 3: Reflinks (❌ FUTURE - High Complexity)

> **vjmp warning:** "If you break it, you own the pieces, AI does not."

**DO NOT IMPLEMENT until you can answer:**
1. What happens when antivirus intercepts FICLONE?
2. How does macOS quarantine attribute affect clonefile?
3. What's the fallback when reflink fails silently?
4. How do you test this on CI without the actual filesystems?

- [ ] ❌ BLOCKED: Answer all questions above first
- [ ] ❌ BLOCKED: Phase 1 must be stable in production first

### Phase 4: Tooling (After Phase 1 is stable)

- [ ] Add `rcc holotree migrate --to-zstd` command
- [x] ~~Add `rcc holotree check`~~ (ALREADY EXISTS)
- [ ] Add progress reporting for migration
