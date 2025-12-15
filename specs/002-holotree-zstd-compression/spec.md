# Feature Specification: Holotree Zstd Compression

**Feature Branch**: `002-holotree-zstd-compression`  
**Created**: 2025-12-15  
**Status**: Draft → Clarified  
**Input**: User description: "Replace gzip with zstd compression in RCC holotree for 3x faster environment restoration while keeping compression"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Faster Environment Restoration (Priority: P1)

As a developer running RCC robots, I want holotree environment restoration to be significantly faster so that my development cycle is more efficient and I spend less time waiting for environments to restore.

**Why this priority**: Environment restoration time is the primary bottleneck experienced by all RCC users. Every robot run that needs an environment restore incurs this delay. This is the core value proposition of the entire feature.

**Independent Test**: Can be fully tested by measuring environment restoration time before and after the change using `--pprof` profiling. Success is demonstrated when restoration completes 2.5x-3.5x faster with similar disk usage.

**Acceptance Scenarios**:

1. **Given** an existing gzip-compressed hololib cache, **When** the user runs a robot that requires environment restoration, **Then** the new RCC reads and decompresses the gzip files transparently (backward compatibility)
2. **Given** a fresh RCC installation with no cache, **When** the user runs a robot for the first time, **Then** environment files are stored using zstd compression in hololib
3. **Given** a large environment with 10,000+ files, **When** the environment is restored from zstd-compressed cache, **Then** restoration completes 2.5x-3.5x faster than with gzip compression
4. **Given** profiling is enabled with `--pprof`, **When** comparing before/after profiles, **Then** `compress/gzip.(*Reader).Read` time is replaced by faster `zstd.(*Decoder)` operations

---

### User Story 2 - Backward Compatible Migration (Priority: P2)

As a team with existing RCC deployments, I want the new RCC to read my existing gzip-compressed hololib files so that I don't have to rebuild all cached environments when upgrading RCC.

**Why this priority**: Without backward compatibility, upgrading RCC would invalidate all existing cached environments, causing massive one-time rebuilds across all users. This would create a poor upgrade experience and discourage adoption.

**Independent Test**: Can be fully tested by installing new RCC in an environment with existing gzip hololib and verifying all operations succeed without cache rebuilds.

**Acceptance Scenarios**:

1. **Given** an existing hololib with gzip-compressed files, **When** new RCC attempts to read a cached file, **Then** RCC detects gzip format via magic bytes and decompresses correctly
2. **Given** an existing catalog file compressed with gzip, **When** new RCC loads the catalog, **Then** catalog is read successfully without errors
3. **Given** a mixed environment with both gzip and zstd files in hololib, **When** RCC performs operations, **Then** both formats are read transparently based on format detection

---

### User Story 3 - Disk Space Maintained (Priority: P2)

As a system administrator managing RCC deployments, I want the new compression algorithm to maintain similar disk space usage so that I don't need to provision additional storage.

**Why this priority**: Users "easily run out of diskspace" (per original author). Maintaining compression ratios is essential—trading disk space for speed would create new problems and conflict with the core design principle that "compression is non-negotiable."

**Independent Test**: Can be fully tested by comparing total hololib directory sizes before and after migration to zstd, verifying no significant increase.

**Acceptance Scenarios**:

1. **Given** a typical Python environment with numpy/pandas dependencies, **When** cached using zstd compression, **Then** total storage size is within 10% of gzip-compressed equivalent
2. **Given** diverse file types in an environment, **When** compressed with zstd, **Then** compression ratios remain competitive with gzip across file types

---

### User Story 4 - Catalog File Performance (Priority: P3)

As a developer, I want catalog files to also benefit from faster compression so that catalog loading operations are faster.

**Why this priority**: Catalogs are read on every environment operation. While smaller than environment files, faster catalog loading contributes to overall performance improvement.

**Independent Test**: Can be fully tested by measuring catalog load times in isolation.

**Acceptance Scenarios**:

1. **Given** an existing gzip-compressed catalog, **When** new RCC loads the catalog, **Then** catalog loads successfully via format detection
2. **Given** a new catalog being saved, **When** RCC writes the catalog, **Then** catalog is written using zstd compression
3. **Given** a catalog with many entries, **When** loaded from zstd format, **Then** load time is noticeably faster than gzip equivalent

---

### Edge Cases

- What happens when a file has zero bytes? → Format detection treats as raw/uncompressed (no magic bytes to detect)
- What happens when magic byte detection encounters a corrupted file header? → Return error, do not attempt decompression with wrong format
- How does the system handle files that are neither gzip nor zstd? → Treated as raw/uncompressed
- What happens during concurrent reads of the same hololib file? → No change to current behavior—file-level safety remains unchanged
- How does this interact with files that have relocations (Rewrite field)? → Relocation handling unchanged—files still decompressed, path-rewritten, and copied regardless of compression algorithm
- What happens when zstd decoder encounters malformed data? → Return error from decoder, propagate to caller; do not silently fail or corrupt output
- What happens if memory is exhausted during decompression? → Let Go runtime handle OOM; no special memory limits imposed (consistent with current gzip behavior)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST read zstd-compressed files from hololib using magic byte detection (0x28 0xb5 0x2f 0xfd)
- **FR-002**: System MUST read gzip-compressed files from hololib using magic byte detection (0x1f 0x8b) for backward compatibility
- **FR-003**: System MUST read raw/uncompressed files when neither zstd nor gzip magic bytes are detected
- **FR-004**: System MUST write new hololib files using zstd compression with `SpeedFastest` encoder level
- **FR-005**: System MUST read zstd-compressed catalog files using magic byte detection
- **FR-006**: System MUST read gzip-compressed catalog files for backward compatibility
- **FR-007**: System MUST write new catalog files using zstd compression with `SpeedFastest` encoder level
- **FR-008**: System MUST NOT change compression handling for ziplibrary (bundles) which uses gzip internally
- **FR-009**: System MUST NOT change compression handling for embedded micromamba blob which uses gzip
- **FR-010**: System MUST continue to verify file integrity via hash comparison during restoration (existing behavior unchanged)
- **FR-011**: System MUST handle files with relocations (Rewrite field > 0) identically to current behavior—decompress, copy, and rewrite paths
- **FR-012**: System MUST return errors (not silent failures) when zstd/gzip decompression fails due to malformed data
- **FR-013**: System MUST use streaming decompression to avoid loading entire files into memory

### Key Entities

- **Hololib File**: Individual cached file in the holotree library, identified by content hash (digest), now stored with zstd compression instead of gzip
- **Catalog**: JSON manifest file listing all files in an environment with their metadata (name, digest, rewrite offsets, mode), also stored compressed
- **Format Detection**: Magic byte inspection at file read time to determine compression format (zstd, gzip, or raw)
- **Compression Level**: `SpeedFastest` (zstd level 1) chosen because decompression speed is identical across all levels, while compression speed during `LiftFile()` is maximized

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Environment restoration (DropFile path) completes 2.5x-3.5x faster as measured by `--pprof` CPU profiling comparing decompression time before and after
- **SC-002**: Total hololib storage size remains within 10% of previous gzip-compressed size for equivalent environments (using `SpeedFastest` level)
- **SC-003**: All existing gzip-compressed hololib files are read successfully without errors or cache invalidation
- **SC-004**: All existing gzip-compressed catalog files are read successfully without errors
- **SC-005**: No changes to external bundle format (ziplibrary) or micromamba blob handling—these remain gzip
- **SC-006**: File integrity verification behavior is unchanged—all files verified via hash on restore
- **SC-007**: No new memory allocation patterns that cause OOM on systems where gzip currently works

## Assumptions

- Users upgrade RCC versions forward, not backward (old RCC cannot read new zstd files, which is acceptable)
- The `github.com/klauspost/compress` library provides pure-Go zstd implementation suitable for cross-compilation
- Compression ratio with `SpeedFastest` is within 10% of gzip for typical Python environment files
- Hash verification (SHA256) during restoration is not the primary bottleneck after switching to zstd (to be validated by profiling)
- Files with relocations (Rewrite field) represent a minority (~30-40%) of total files in typical environments
- Memory usage patterns of zstd streaming decompression are comparable to gzip

## Dependencies

- **External Library**: `github.com/klauspost/compress/zstd` - Pure Go zstd implementation
  - License: BSD/MIT/Apache (permissive, compatible with RCC licensing)
  - Widely adopted: Docker, MinIO, Prometheus, CockroachDB, etcd, Grafana
  - Maintainer: Klaus Post (Go stdlib contributor)
  - Cross-compilation: Works with `go build` without CGO
  - Minimum version: v1.17.0 or latest stable

## Out of Scope

- **Hash verification optimization (Phase 2)**: Whether SHA256 verification is a bottleneck is unknown until Phase 1 profiling is complete. This is deferred pending profiling results.
- **Reflink/copy-on-write support (Phase 3)**: OS-specific filesystem optimizations (FICLONE, clonefile, FSCTL_DUPLICATE_EXTENTS) carry high complexity and maintenance burden. Per original author: "If you break it, you own the pieces."
- **Optional verification skip**: Security implications make this dangerous for shared/networked environments.
- **Migration tooling**: `rcc holotree migrate --to-zstd` command is post-Phase-1 tooling once the core feature is stable.
- **Parallel decompression**: klauspost/compress supports this but is out of scope for initial implementation.
- **Decoder pooling**: Reusing zstd decoder instances across calls is a potential optimization but out of scope for initial implementation.
- **Configurable compression levels**: `SpeedFastest` is fixed; user-configurable levels add complexity without clear benefit.

## Security Considerations

- File integrity verification via hash comparison remains unchanged and enabled by default
- No new security attack surfaces introduced—same data flow, different compression algorithm
- External bundle format (ziplibrary) unchanged—no impact on bundle security model
- Malformed compressed data results in explicit errors, not silent data corruption

## Clarification Log

| Question | Answer | Source |
|----------|--------|--------|
| Which zstd compression level? | `SpeedFastest` (level 1) - decompression speed is identical across levels, compression speed is maximized | BLAZINGLY_FAST_SPEC.md |
| Memory constraints for decompression? | None imposed - streaming API used, consistent with current gzip behavior | Design decision |
| Error handling for malformed zstd? | Return error from decoder, propagate to caller | FR-012 added |
| What is "approximately 3x faster"? | 2.5x-3.5x range to account for variability across file types and hardware | SC-001 refined |
| Disk space tolerance? | Within 10% of gzip (relaxed from 5% to account for `SpeedFastest` ratio) | SC-002 refined |
