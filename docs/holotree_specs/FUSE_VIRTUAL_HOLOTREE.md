# FUSE Virtual Holotree Research

**Issue:** #70
**Status:** Research
**Author:** @joshyorko
**Origin:** Idea from @vjmp (2020-2024 thinking)

## The Problem

Current holotree restoration (DROP) copies every file from hololib to holotree:

```
12,038 files
237 MB compressed in hololib
576 MB uncompressed in holotree
0.66 seconds (best case, Linux SSD)
3-5 seconds (Windows with AV)
10-30 seconds (NFS/SMB)
```

**But most automations only touch a fraction of these files.**

A typical RPA robot might use:
- Python interpreter
- A few dozen imported packages
- Maybe 500-1000 files total

Yet we copy all 12,000+ files every time.

## The Idea

Instead of copying files, mount a virtual filesystem that serves files on-demand:

```
Current:    hololib -> [copy 12k files] -> holotree -> automation
Proposed:   hololib -> [FUSE mount] -> virtual holotree -> automation
```

Files are decompressed only when accessed. Writes go to an overlay.

## How FUSE Works

```
┌─────────────────────────────────────────┐
│         User Application                │
│   open("/holotree/lib/python3.12/...")  │
└─────────────────────────────────────────┘
                    │
                    ▼ (syscall)
┌─────────────────────────────────────────┐
│              Linux Kernel               │
│         VFS (Virtual File System)       │
└─────────────────────────────────────────┘
                    │
                    ▼ (FUSE protocol)
┌─────────────────────────────────────────┐
│         FUSE Userspace Daemon           │
│   (our Go code with go-fuse library)    │
│                                         │
│   - Lookup: map path to hololib digest  │
│   - Read: decompress from hololib       │
│   - Write: redirect to overlay          │
│   - Stat: return stored metadata        │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│              Hololib                    │
│   Compressed files indexed by hash      │
└─────────────────────────────────────────┘
```

## Expected Performance

| Operation | Current | FUSE | Notes |
|-----------|---------|------|-------|
| Mount/activation | 660ms | ~10ms | Just mount, no file copy |
| First file read | 0ms | 1-5ms | Decompression overhead |
| Subsequent reads | 0ms | 0ms | Kernel page cache |
| Disk space | 576 MB | ~0 MB | Only overlay writes |
| Writes (.pyc etc) | instant | instant | Goes to overlay |

## Architecture

### Components

```
rcc
├── cmd/
│   └── holotreeMount.go      # New command: rcc ht mount
├── htfs/
│   └── fuse/
│       ├── mount.go          # FUSE mount/unmount logic
│       ├── filesystem.go     # FUSE filesystem implementation
│       ├── node.go           # File/directory nodes
│       ├── overlay.go        # Copy-on-write overlay
│       └── cache.go          # Decompression cache
```

### Data Flow

```
1. rcc ht mount --space user/abc123

2. Load catalog from /holotree/abc123.meta
   - Contains: path -> (digest, mode, size, mtime, symlink_target)

3. Start FUSE daemon
   - Mount at /holotree/abc123 (or custom mountpoint)
   - Overlay at /tmp/rcc-overlay-abc123

4. File access:
   Lookup(path) -> catalog entry
   Open(path)   -> prepare file handle
   Read(offset) -> decompress from hololib/library/XX/YYYY
   Write(data)  -> copy to overlay, write there

5. Automation runs against mounted path

6. rcc ht unmount --space user/abc123
   - Cleanup overlay (or preserve for dirty environments)
```

### Catalog Structure

We already have this in `.meta` files:

```json
{
  "lib/python3.12/site-packages/pandas/__init__.py": {
    "digest": "a3b2c1d4e5f6...",
    "mode": 33188,
    "size": 12345,
    "mtime": 1704067200
  }
}
```

This becomes our virtual directory structure.

## Go FUSE Libraries

### Option 1: hanwen/go-fuse (Recommended)

```go
import "github.com/hanwen/go-fuse/v2/fs"
import "github.com/hanwen/go-fuse/v2/fuse"

type HolotreeRoot struct {
    fs.Inode
    catalog *Catalog
    hololib string
    overlay string
}

func (r *HolotreeRoot) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
    entry := r.catalog.Get(name)
    if entry == nil {
        return nil, syscall.ENOENT
    }
    // Return inode for file/directory
}

func (f *HolotreeFile) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
    // Check overlay first
    if overlayExists(f.path) {
        return readFromOverlay(f.path, dest, off)
    }
    // Decompress from hololib
    return decompressAndRead(f.digest, dest, off)
}
```

**Pros:**
- High performance (used by Google's Android tools)
- Active development
- Good documentation

**Cons:**
- Linux/macOS only (no Windows)

### Option 2: bazil.org/fuse

```go
import "bazil.org/fuse"
import "bazil.org/fuse/fs"

type FS struct {
    catalog *Catalog
}

func (f FS) Root() (fs.Node, error) {
    return &Dir{catalog: f.catalog, path: "/"}, nil
}
```

**Pros:**
- Pure Go
- Simpler API

**Cons:**
- Less active
- Slightly lower performance

### Option 3: Windows via WinFsp + cgofuse

```go
import "github.com/winfsp/cgofuse/fuse"
```

**Pros:**
- Cross-platform (Linux, macOS, Windows)

**Cons:**
- CGO required
- Windows needs WinFsp installed

## Challenges

### 1. Python Import Performance

Python imports trigger many file operations:

```python
import pandas
# Triggers: ~200 open() calls, ~50 stat() calls
```

**Mitigation:**
- Kernel page cache handles repeated reads
- Prefetch common patterns (site-packages directories)
- Optional: memory cache for hot files

### 2. Symlink Handling

Environments have many symlinks:
```
python -> python3.12
libpython3.12.so.1.0 -> libpython3.12.so
```

**Solution:** Store symlink target in catalog, return in `Readlink()`

### 3. File Relocations

Some files need path rewriting (2-6% per #66 research):
```
/original/build/path -> /holotree/actual/path
```

**Solution:**
- Relocate on-the-fly during Read()
- Or: pre-relocate during Lift, store relocated version

### 4. Writes and .pyc Files

Python generates `.pyc` bytecode files:
```
__pycache__/module.cpython-312.pyc
```

**Solution:** Copy-on-write overlay
- First write to path -> copy from hololib to overlay
- All subsequent access uses overlay version

### 5. Crash/Cleanup

Stale FUSE mounts are problematic.

**Solution:**
- PID file for mount tracking
- `rcc ht cleanup` command
- Auto-unmount on rcc exit (signal handler)

## Prototype Steps

### Phase 1: Read-Only Mount (POC)

```bash
# Goal: Mount catalog as virtual directory, read files from hololib

rcc ht mount --space user/abc123 --mountpoint /tmp/vholotree

ls /tmp/vholotree/bin/python
# Should show file (served from hololib)

python /tmp/vholotree/bin/python -c "import sys; print(sys.version)"
# Should work
```

**Success criteria:**
- Python interpreter runs from virtual mount
- `import pandas` works
- Measure: time to first import vs traditional DROP

### Phase 2: Overlay Writes

```bash
# Goal: Handle writes via copy-on-write

python -c "import pandas"
# Creates __pycache__/*.pyc in overlay

ls /tmp/rcc-overlay-abc123/__pycache__/
# Should contain .pyc files
```

### Phase 3: Integration

```bash
# Goal: Seamless integration with rcc run

rcc run --virtual -r robot.yaml -t task
# Automatically: mount -> run -> unmount
```

## Benchmarks to Run

1. **Mount time:** `time rcc ht mount` vs `time rcc ht vars`
2. **Python startup:** `time python -c "pass"` on virtual vs real
3. **Heavy import:** `time python -c "import pandas, numpy, sqlalchemy"`
4. **Full robot run:** Complete automation on virtual vs real
5. **Disk usage:** `du -sh` overlay after typical workload

## Decision Points

1. **Which FUSE library?** Recommend: `hanwen/go-fuse` for Linux-first
2. **Cache strategy?** Start with: kernel cache only, add memory cache if needed
3. **Relocation approach?** Start with: relocate on-the-fly
4. **Windows support?** Defer to Phase 4, focus on Linux/cloud first

## References

- [FUSE Wikipedia](https://en.wikipedia.org/wiki/Filesystem_in_Userspace)
- [9P Protocol](https://en.wikipedia.org/wiki/9P_(protocol))
- [go-fuse](https://github.com/hanwen/go-fuse)
- [bazil/fuse](https://github.com/bazil/fuse)
- [WinFsp](https://github.com/winfsp/winfsp)
- [cgofuse](https://github.com/winfsp/cgofuse)
- [libfuse](https://github.com/libfuse/libfuse)

## Credit

This idea originated from @vjmp's thinking during 2020-2024. He suggested using FUSE or 9P-style virtual filesystems to eliminate holotree copying entirely.
