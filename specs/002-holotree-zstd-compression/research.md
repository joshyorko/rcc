# Research: Holotree Zstd Compression

**Feature**: `002-holotree-zstd-compression`
**Date**: 2025-12-15

## Research Tasks

### 1. Zstd Library Selection for Go

**Question**: Which zstd library should be used for pure Go implementation?

**Decision**: `github.com/klauspost/compress/zstd`

**Rationale**:
- Pure Go implementation - no CGO required
- Cross-compiles cleanly to Linux, Windows, macOS (all targets RCC supports)
- Widely adopted: Docker, MinIO, Prometheus, CockroachDB, etcd, Grafana
- Maintained by Klaus Post (Go stdlib contributor)
- BSD/MIT/Apache licenses (permissive, compatible with RCC)
- Supports streaming API (required for memory efficiency)
- Version v1.17.0+ recommended

**Alternatives Considered**:
- `github.com/DataDog/zstd` - Rejected: Requires CGO
- `github.com/valyala/gozstd` - Rejected: Requires CGO
- `lz4` - Rejected: Lower compression ratio, not the right trade-off

### 2. Compression Level Selection

**Question**: Which zstd encoder level optimizes for decompression speed?

**Decision**: `zstd.SpeedFastest` (level 1)

**Rationale**:
- Decompression speed is identical across all zstd compression levels
- `SpeedFastest` minimizes compression time during `LiftFile()` (environment creation)
- Compression ratio at level 1 is within 10% of higher levels for typical files
- This aligns with the core goal: faster restoration (decompression)

**Source**: BLAZINGLY_FAST_SPEC.md, zstd benchmarks

### 3. Magic Byte Detection Pattern

**Question**: How to reliably detect compression format?

**Decision**: Use magic byte inspection at file open time

**Details**:
```
zstd magic: 0x28 0xb5 0x2f 0xfd (4 bytes)
gzip magic: 0x1f 0x8b (2 bytes)
raw: neither pattern matches
```

**Implementation Pattern**:
1. Open file
2. Read first 4 bytes
3. Check for zstd magic (4 bytes)
4. If not zstd, check for gzip magic (2 bytes)
5. If neither, treat as raw/uncompressed
6. Seek back to start
7. Create appropriate reader

**Rationale**:
- Deterministic detection (no try-fail approach)
- Works for both hololib files and catalog files
- Handles backward compatibility with existing gzip files
- Handles raw files (edge case)

### 4. Files to NOT Change

**Question**: Which compression paths must remain gzip?

**Decision**: Do NOT change compression in these locations:

| File | Function | Reason |
|------|----------|--------|
| `htfs/ziplibrary.go` | `openFile()` | Pre-built zip bundles use gzip internally |
| `conda/installing.go` | `GunzipWrite()` | Embedded micromamba blob is gzip compressed |

**Rationale**: These are external formats we don't control.

### 5. Streaming vs Buffered Decompression

**Question**: Should decompression load entire file into memory?

**Decision**: Use streaming decompression

**Rationale**:
- Current gzip implementation uses streaming
- Memory usage patterns must remain similar (SC-007)
- Large environment files could exhaust memory if fully buffered
- klauspost/compress supports streaming API via `zstd.NewReader()`

### 6. Error Handling for Malformed Data

**Question**: What happens when compressed data is corrupted?

**Decision**: Return explicit errors, do not silently fail

**Implementation**:
- zstd decoder returns errors for malformed frames
- Errors propagate to caller via existing error handling patterns
- No silent data corruption
- Aligns with existing gzip error handling behavior

## Summary

All NEEDS CLARIFICATION items resolved:

| Item | Resolution |
|------|------------|
| Library selection | `github.com/klauspost/compress/zstd` |
| Compression level | `SpeedFastest` (level 1) |
| Format detection | Magic byte inspection |
| Unchanged paths | ziplibrary, micromamba blob |
| Memory model | Streaming decompression |
| Error handling | Explicit errors, no silent failures |
