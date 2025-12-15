# Data Model: Holotree Zstd Compression

**Feature**: `002-holotree-zstd-compression`
**Date**: 2025-12-15

## Entity Overview

This feature does NOT introduce new data entities. It modifies the **storage format** of existing entities.

## Existing Entities (Unchanged Schema)

### Hololib File

Individual cached file in the holotree library.

| Field | Type | Description |
|-------|------|-------------|
| `digest` | string | SHA256 content hash (filename in hololib) |
| `content` | bytes | File content (NOW: zstd compressed, BEFORE: gzip compressed) |

**Storage Location**: `~/.robocorp/hololib/library/{digest[0:2]}/{digest[2:4]}/{digest[4:6]}/{digest}`

**Change**: Content is now written with zstd compression instead of gzip.

### Catalog (Root)

JSON manifest of an environment's file tree.

```go
type Root struct {
    *Info
    Lifted bool `json:"lifted"`
    Tree   *Dir `json:"tree"`
}

type Info struct {
    RccVersion string `json:"rcc"`
    Identity   string `json:"identity"`
    Path       string `json:"path"`
    Controller string `json:"controller"`
    Space      string `json:"space"`
    Platform   string `json:"platform"`
    Blueprint  string `json:"blueprint"`
}
```

**Storage Location**: `~/.robocorp/hololib/catalog/*.dat`

**Change**: Catalog file is now written with zstd compression instead of gzip.

### File (within Catalog)

Metadata for a single file within an environment.

```go
type File struct {
    Name    string      `json:"name"`
    Symlink string      `json:"symlink,omitempty"`
    Size    int64       `json:"size"`
    Mode    fs.FileMode `json:"mode"`
    Digest  string      `json:"digest"`
    Rewrite []int64     `json:"rewrite"`  // Relocation offsets
}
```

**No changes to schema**. The `Rewrite` field continues to track relocation offsets.

### Dir (within Catalog)

Directory node in the catalog tree.

```go
type Dir struct {
    Name    string           `json:"name"`
    Symlink string           `json:"symlink,omitempty"`
    Mode    fs.FileMode      `json:"mode"`
    Dirs    map[string]*Dir  `json:"subdirs"`
    Files   map[string]*File `json:"files"`
    Shadow  bool             `json:"shadow,omitempty"`
}
```

**No changes to schema**.

## New Internal Types

### CompressionFormat (internal enum)

```go
type CompressionFormat int

const (
    FormatUnknown CompressionFormat = iota
    FormatRaw
    FormatGzip
    FormatZstd
)
```

**Purpose**: Result of magic byte detection, used to select decompression path.

### Magic Byte Constants

```go
var (
    gzipMagic = []byte{0x1f, 0x8b}             // gzip header
    zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd} // zstd frame magic
)
```

**Purpose**: Used for format detection when reading files.

## State Transitions

### Hololib File Lifecycle

```
                    ┌─────────────────┐
                    │   Source File   │
                    │   (uncompressed)│
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
    LiftFile()      │  zstd compress  │  ← NEW (was gzip)
                    │  + write to lib │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Hololib Cache  │
                    │  (zstd or gzip) │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
    DropFile()      │ detect format   │  ← NEW
                    │ zstd/gzip/raw   │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  decompress +   │
                    │  verify hash    │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  apply rewrites │
                    │  (if needed)    │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Target File    │
                    │  (uncompressed) │
                    └─────────────────┘
```

### Catalog File Lifecycle

```
                    ┌─────────────────┐
                    │   Root struct   │
                    │   (in memory)   │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
    SaveAs()        │  JSON marshal   │
                    │  + zstd compress│  ← NEW (was gzip)
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  Catalog file   │
                    │  (zstd or gzip) │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
    LoadFrom()      │ detect format   │  ← NEW
                    │ zstd/gzip       │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  decompress +   │
                    │  JSON unmarshal │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   Root struct   │
                    │   (in memory)   │
                    └─────────────────┘
```

## Validation Rules

1. **Magic byte detection**: Must check zstd (4 bytes) before gzip (2 bytes)
2. **Hash verification**: Digest must match after decompression (existing behavior)
3. **Relocation handling**: Files with `len(Rewrite) > 0` still require full decompress + copy + rewrite
4. **Symlinks**: Bypass compression entirely (existing behavior)

## Backward Compatibility

| Scenario | Behavior |
|----------|----------|
| New RCC reads old gzip hololib | ✅ Detected via magic bytes, decompressed with gzip |
| New RCC reads old gzip catalog | ✅ Detected via magic bytes, decompressed with gzip |
| New RCC writes new file | ✅ Written with zstd compression |
| Old RCC reads new zstd file | ❌ Will fail (acceptable: users upgrade forward) |
