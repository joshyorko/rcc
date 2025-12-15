# Internal API Contracts: Holotree Zstd Compression

**Feature**: `002-holotree-zstd-compression`
**Date**: 2025-12-15

This feature modifies internal Go functions. No external HTTP/REST/GraphQL APIs are affected.

## Function Contracts

### 1. detectFormat

**Location**: `htfs/delegates.go` (new function)

**Signature**:
```go
func detectFormat(r io.ReadSeeker) (CompressionFormat, error)
```

**Input**:
- `r`: An `io.ReadSeeker` positioned at start of file

**Output**:
- `CompressionFormat`: One of `FormatZstd`, `FormatGzip`, `FormatRaw`
- `error`: Non-nil if read or seek fails

**Behavior**:
1. Read first 4 bytes
2. Check for zstd magic (`0x28 0xb5 0x2f 0xfd`)
3. If match → return `FormatZstd`
4. Check first 2 bytes for gzip magic (`0x1f 0x8b`)
5. If match → return `FormatGzip`
6. Otherwise → return `FormatRaw`
7. Seek back to position 0
8. Return format and any error

**Error Handling**:
- Short read (< 4 bytes): Return `FormatRaw` (treat as uncompressed)
- Seek error: Return error

---

### 2. openCompressed (replaces gzDelegateOpen)

**Location**: `htfs/delegates.go` (modified function)

**Signature**:
```go
func openCompressed(filename string) (io.Reader, Closer, error)
```

**Input**:
- `filename`: Absolute path to file in hololib

**Output**:
- `io.Reader`: Decompressed stream (or raw if uncompressed)
- `Closer`: Function to close underlying resources
- `error`: Non-nil if open or decompression setup fails

**Behavior**:
1. Open file
2. Detect format via `detectFormat()`
3. Based on format:
   - `FormatZstd`: Create `zstd.NewReader(file)`
   - `FormatGzip`: Create `gzip.NewReader(file)`
   - `FormatRaw`: Return file directly
4. Return reader and closer

**Error Handling**:
- File not found: Return error
- Format detection fails: Return error
- Decompressor creation fails: Close file, return error

---

### 3. LiftFile (modified)

**Location**: `htfs/functions.go`

**Signature** (unchanged):
```go
func LiftFile(sourcename, sinkname string, compress bool) anywork.Work
```

**Behavior Change**:
- When `compress == true`: Use `zstd.NewWriter(sink, zstd.WithEncoderLevel(zstd.SpeedFastest))`
- Previously: Used `gzip.NewWriterLevel(sink, gzip.BestSpeed)`

**Contract**:
- Input file is read uncompressed
- Output file is written with zstd compression (if compress=true)
- Atomic write via temp file + rename pattern (unchanged)

---

### 4. CheckHasher (modified)

**Location**: `htfs/functions.go`

**Signature** (unchanged):
```go
func CheckHasher(known map[string]map[string]bool) Filetask
```

**Behavior Change**:
- Use `detectFormat()` to determine compression
- Support both zstd and gzip for reading
- Previously: Only gzip with fallback to raw

**Contract**:
- Reads file content
- Computes hash
- Sets `details.Digest` to computed hash
- Removes file if hash not in known set

---

### 5. Root.SaveAs (modified)

**Location**: `htfs/directory.go`

**Signature** (unchanged):
```go
func (it *Root) SaveAs(filename string) error
```

**Behavior Change**:
- Use `zstd.NewWriter(sink, zstd.WithEncoderLevel(zstd.SpeedFastest))`
- Previously: Used `gzip.NewWriterLevel(sink, gzip.BestSpeed)`

**Contract**:
- Marshals Root to JSON
- Compresses with zstd
- Writes to file atomically
- Saves `.info` metadata file

---

### 6. Root.LoadFrom (modified)

**Location**: `htfs/directory.go`

**Signature** (unchanged):
```go
func (it *Root) LoadFrom(filename string) error
```

**Behavior Change**:
- Use `detectFormat()` to determine compression
- Support both zstd and gzip for reading
- Previously: Only gzip

**Contract**:
- Opens catalog file
- Detects format (zstd or gzip)
- Decompresses
- Unmarshals JSON into Root struct

---

## Type Definitions

### CompressionFormat

```go
// CompressionFormat represents the detected compression format of a file.
type CompressionFormat int

const (
    FormatUnknown CompressionFormat = iota
    FormatRaw     // Uncompressed
    FormatGzip    // gzip compressed (legacy)
    FormatZstd    // zstd compressed (new default)
)
```

### Closer (existing)

```go
// Closer is a function that releases resources.
type Closer func() error
```

---

## Dependency Import

```go
import "github.com/klauspost/compress/zstd"
```

Required addition to `go.mod`:
```
require github.com/klauspost/compress v1.17.0
```
