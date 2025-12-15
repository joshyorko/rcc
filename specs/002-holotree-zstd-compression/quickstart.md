# Quickstart: Holotree Zstd Compression

**Feature**: `002-holotree-zstd-compression`
**Date**: 2025-12-15

## Prerequisites

- Go 1.20+ installed
- RCC source code cloned
- Basic understanding of `htfs/` package

## Development Setup

### 1. Add the zstd dependency

```bash
cd /path/to/rcc
go get github.com/klauspost/compress/zstd@latest
go mod tidy
```

### 2. Run existing tests (baseline)

```bash
# Unit tests
GOARCH=amd64 go test ./htfs/... -v

# Full test suite
inv test
```

### 3. Build RCC

```bash
# Quick local build
GOARCH=amd64 go build -o build/ ./cmd/...

# Or via Invoke
inv build
```

## Key Files to Modify

| File | Purpose |
|------|---------|
| `htfs/delegates.go` | Format detection, decompression |
| `htfs/functions.go` | LiftFile (compress), CheckHasher |
| `htfs/directory.go` | Catalog SaveAs/LoadFrom |

## Testing the Changes

### Profile before changes

```bash
# Clean hololib for baseline
rm -rf ~/.robocorp/hololib

# Run with profiling
./build/rcc task run -r robot.yaml --pprof profile_before.pprof
```

### Profile after changes

```bash
# Clean hololib again
rm -rf ~/.robocorp/hololib

# Run with profiling
./build/rcc task run -r robot.yaml --pprof profile_after.pprof
```

### Compare profiles

```bash
# Interactive comparison
go tool pprof -http=:8080 profile_after.pprof

# Or text comparison
go tool pprof -base profile_before.pprof profile_after.pprof
```

## Verification Checklist

1. **Backward compatibility**: New RCC reads old gzip hololib
   ```bash
   # Keep existing hololib, install new RCC, run robot
   ./build/rcc task run -r robot.yaml
   # Should succeed without cache rebuild
   ```

2. **New format works**: New RCC writes and reads zstd
   ```bash
   rm -rf ~/.robocorp/hololib
   ./build/rcc task run -r robot.yaml
   # Check files are zstd compressed
   file ~/.robocorp/hololib/library/*/*/*/*
   ```

3. **Performance improvement**: Measure 2.5x-3.5x speedup
   ```bash
   # Compare profile times for decompression functions
   ```

4. **Disk space**: Verify within 10% of gzip
   ```bash
   du -sh ~/.robocorp/hololib/
   ```

## Common Issues

### "unknown magic number"

Check that `detectFormat()` is correctly checking zstd magic bytes (4 bytes) before gzip (2 bytes).

### Build fails with CGO errors

Ensure you're using `github.com/klauspost/compress/zstd`, NOT a CGO-based library like DataDog/zstd.

### Hash verification fails

The `Compress()` function return value may affect hash calculation. Ensure consistency between LiftFile and verification paths.

## Robot Framework Tests

```bash
# Run acceptance tests
python3 -m robot -L DEBUG -d tmp/output robot_tests

# Or via Invoke
inv robot
```
