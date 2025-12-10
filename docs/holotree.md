# Holotree: A Deep Dive

Holotree is RCC's content-addressed storage system for Python environments. It is the
innovation that transforms RCC from "a tool that creates Python environments" into
"infrastructure for reproducible automation at scale."

This document explains what Holotree is, why its design is elegant, where it falls
short, and how we plan to improve it.

---

## Part I: Why Holotree is Beautiful

### The Core Insight

Every Python environment is just a directory tree of files. When you run
`pip install pandas`, you're downloading and extracting tarballs into predictable
paths. The `numpy` you install today produces identical bytes to the `numpy` someone
else installed yesterday from the same version.

Holotree exploits this observation: **environments are data, and identical data
should be stored once.**

This is the same insight that powers Git (commits are content-addressed), Docker
(image layers are deduplicated), and modern backup systems (block-level deduplication).
But where Git handles source code and Docker handles containers, Holotree handles
Python environments.

### The Architecture

Holotree stores environments in two complementary structures:

```
~/.robocorp/hololib/
├── library/          # Content-addressed file storage
│   ├── a1/
│   │   └── b2/
│   │       └── c3/
│   │           └── a1b2c3d4e5f6...  # File stored by its hash
│   └── ...
├── catalog/          # Environment manifests (blueprints)
│   ├── 7f8a9b...v12.linux64       # What files comprise this environment
│   └── ...
└── usage/            # Last-used timestamps for cleanup

~/.robocorp/holotree/
├── h8a7b6_c5d4e3f2g1h0i9/         # Active environment spaces
│   ├── bin/
│   ├── lib/
│   └── identity.yaml
└── ...
```

**The Library** stores every file by its content hash. A 50MB `libopenblas.so` that
appears in 20 different environments exists exactly once on disk. The path structure
(`a1/b2/c3/`) distributes files across directories to avoid filesystem limitations.

**The Catalog** stores environment blueprints—JSON manifests that list every file in
an environment along with its content hash, size, mode, and relocation markers. A
catalog is a complete description of an environment that can be verified, transferred,
and restored.

**Spaces** are the live, working environments that robots actually use. They're
populated by reading a catalog and linking or copying files from the library.

### Content Addressing: The Magic

Every file in the library is stored by its SipHash-128 digest:

```go
// From common/algorithms.go
func NewDigester(legacy bool) hash.Hash {
    if legacy {
        return sha256.New()
    } else {
        return siphash.New128([]byte("Very_Random-Seed"))
    }
}
```

SipHash is chosen over SHA-256 for two reasons:

1. **Speed** — SipHash is dramatically faster, which matters when hashing thousands
   of files during environment builds.
2. **Sufficiency** — We're not defending against adversarial collisions. We're
   detecting accidental duplicates. SipHash's collision resistance is more than
   adequate.

When RCC builds an environment, it computes the hash of every file. If that hash
already exists in the library, the file is skipped. If not, it's compressed with
gzip and stored. The result: environments that share 90% of their files only store
the unique 10%.

### The Blueprint: Environment as Identity

An environment's identity is the hash of its specification—the `conda.yaml` file
that declares what packages to install:

```go
// From common/algorithms.go
func BlueprintHash(blueprint []byte) string {
    return Textual(Sipit(blueprint), 0)
}
```

This hash becomes the key for everything:
- The catalog filename includes it
- The space directory can be derived from it
- Remote pulls use it to request specific environments

Change one line in your `conda.yaml`? Different hash. Different catalog. Different
environment. The mapping is deterministic and reproducible.

### Relocation: The Hardest Problem

Python environments have a brutal limitation: **hardcoded paths**.

When you install Python to `/home/alice/.robocorp/holotree/abc123/`, that path gets
embedded in shebang lines, `__pycache__` files, compiled extensions, and more. You
can't just copy the environment to `/home/bob/.robocorp/holotree/xyz789/` and expect
it to work.

Holotree solves this with **relocation markers**:

```go
// From htfs/functions.go
func Locator(seek string) Filetask {
    return func(fullpath string, details *File) anywork.Work {
        return func() {
            // ... hash the file while recording where the path appears
            locator := RelocateWriter(digest, seek)
            _, err = io.Copy(locator, source)
            details.Rewrite = locator.Locations()  // Byte offsets of path
            details.Digest = fmt.Sprintf("%02x", digest.Sum(nil))
        }
    }
}
```

During environment creation, RCC scans every file for the installation path. It
records the byte offsets where that path appears. When restoring to a new location,
it seeks to those offsets and writes the new path:

```go
// From htfs/functions.go
func DropFile(...) anywork.Work {
    return func() {
        // ... copy file from library ...
        for _, position := range details.Rewrite {
            _, err = sink.Seek(position, 0)
            _, err = sink.Write(rewrite)  // Write new path at recorded offset
        }
    }
}
```

This is surgical path rewriting—no regex, no find-and-replace, just direct byte
manipulation at known offsets. It's fast and reliable.

### The Catalog Format

Catalogs are gzip-compressed JSON that describe the complete tree structure:

```go
// From htfs/directory.go
type Root struct {
    *Info
    Lifted bool `json:"lifted"`
    Tree   *Dir `json:"tree"`
}

type Dir struct {
    Name    string           `json:"name"`
    Symlink string           `json:"symlink,omitempty"`
    Mode    fs.FileMode      `json:"mode"`
    Dirs    map[string]*Dir  `json:"subdirs"`
    Files   map[string]*File `json:"files"`
}

type File struct {
    Name    string      `json:"name"`
    Symlink string      `json:"symlink,omitempty"`
    Size    int64       `json:"size"`
    Mode    fs.FileMode `json:"mode"`
    Digest  string      `json:"digest"`
    Rewrite []int64     `json:"rewrite"`  // Path relocation offsets
}
```

A catalog captures everything needed to reconstruct an environment:
- Complete directory tree structure
- File metadata (permissions, sizes)
- Content hashes for integrity verification
- Relocation markers for path rewriting
- Symlink targets for link preservation

### Export and Import: Air-Gapped Deployment

Holotree environments can be exported as `hololib.zip` files that contain:
- The catalog(s)
- All referenced library files

```go
// From htfs/library.go
func (it *hololib) Export(catalogs, known []string, archive string) error {
    // Create zip with catalog and all referenced library files
    for _, name := range catalogs {
        err = fs.Treetop(ZipRoot(it, fs, zipper))
        // ... add catalog to zip ...
    }
}
```

These zip files can be copied to air-gapped machines and imported directly:

```bash
rcc holotree export -c catalog_hash -o environment.zip  # On connected machine
# ... copy zip via USB, sneakernet, etc. ...
rcc holotree import environment.zip                     # On air-gapped machine
```

The environment appears instantly. No internet. No conda channels. No pip indexes.
Just bytes from the zip to the library.

### Delta Transfers: The rccremote Protocol

When pulling from a remote server, Holotree minimizes bandwidth:

```go
// From operations/pull.go
func pullOriginFingerprints(origin, catalogName string) (string, int, error) {
    // Request list of all file hashes in the catalog
    response := client.Get(request)

    // Check which hashes we're missing locally
    for {
        if !pathlib.IsFile(htfs.ExactDefaultLocation(flat)) {
            collection = append(collection, flat)  // We need this one
        }
    }
    return strings.Join(collection, "\n"), len(collection), nil
}
```

The client sends a list of hashes it doesn't have. The server creates a zip
containing only those files. Shared files between environments are never
transferred twice.

---

## Part II: Why Holotree is Fast

### Restore vs. Build

The fundamental performance win is avoiding package installation:

| Operation | Time |
|-----------|------|
| Fresh environment build | 5–15 minutes |
| Holotree restore from cache | 2–10 seconds |

When a catalog exists in the library, "creating" an environment means:
1. Read the catalog (milliseconds)
2. Create directory structure (milliseconds)
3. Link or copy files from library (seconds)

There's no network access to conda channels or PyPI. No package resolution. No
compilation. No post-install scripts. Just filesystem operations.

### Parallel Everything

Holotree uses a worker pool (`anywork` package) for all heavy operations:

```go
// From htfs/functions.go
func ScheduleLifters(library MutableLibrary, stats *stats) Treetop {
    return func(path string, it *Dir) error {
        for name, file := range it.Files {
            // Schedule file copy as background work
            anywork.Backlog(LiftFile(sourcepath, sinkpath, compress))
        }
        return nil
    }
}
```

File hashing, compression, copying, and integrity checks all run in parallel across
available CPU cores. The `--workers` flag lets you tune this, but the default
auto-scaling handles most cases well.

### Dirty Tracking

During restore, Holotree tracks which files actually need updating:

```go
// From htfs/functions.go
func RestoreDirectory(library Library, fs *Root, current map[string]string, stats *stats) Dirtask {
    return func(path string, it *Dir) anywork.Work {
        return func() {
            // Compare current state to desired state
            shadow, ok := current[directpath]
            golden := !ok || found.Digest == shadow
            ok = golden && found.Match(info)
            stats.Dirty(!ok)
            if !ok {
                // Only update files that actually changed
                anywork.Backlog(DropFile(...))
            }
        }
    }
}
```

If you modify files in an environment and then restore, only changed files are
replaced. The "dirtyness" percentage is tracked in statistics:

```go
// From journal/buildstats.go
stats.Statsline(tabbed, "Dirty", asPercent, func(the *BuildEvent) float64 {
    return the.Dirtyness
})
```

### Layer Optimization

Environments are built in layers: micromamba packages first, then pip packages,
then post-install scripts. Each layer gets its own catalog:

```go
// From htfs/commands.go
func RestoreLayersTo(tree MutableLibrary, identityfile, targetDir string) conda.SkipLayer {
    layers := config.AsLayers()
    mambaLayer := []byte(layers[0])
    pipLayer := []byte(layers[1])

    if tree.HasBlueprint(pipLayer) {
        // Restore pip layer directly, skip micromamba install
        return conda.SkipPipLayer
    }
    if tree.HasBlueprint(mambaLayer) {
        // Restore mamba layer directly, skip base install
        return conda.SkipMicromambaLayer
    }
}
```

If you have environment A with `python=3.10, numpy=1.23` and create environment B
with `python=3.10, numpy=1.23, pandas=2.0`, the base layer is identical. RCC
restores the cached base and only installs the new pandas packages.

---

## Part III: What's Wrong With Holotree

### Problem 1: Discoverability

The functionality is powerful but hidden. Consider the average user's discovery
path:

```bash
$ rcc --help
# Shows "holotree" buried among 15+ command groups

$ rcc holotree --help
# Shows 19 subcommands with terse descriptions

$ rcc holotree pull --help
# Finally reveals --origin flag for remote servers
```

Compare this to Docker:
```bash
$ docker pull nginx  # Just works. Everyone knows how.
```

Users must discover:
- That `rcc holotree` even exists
- That `rccremote` is a separate binary
- That `RCC_REMOTE_ORIGIN` controls the default remote
- That `rcc ht pull` can fetch pre-built environments

This is a documentation and UX problem, not an architecture problem.

### Problem 2: No Push

Holotree supports pulling environments from a remote server, but there's no built-in
push operation:

```go
// From htfs/library.go
func (it *hololib) Export(...) error {
    // Exports to local zip file
}
```

To share environments, you must:
1. Export to a zip file
2. Manually upload to the server
3. Hope the server picks up the new catalog

A proper `rcc ht push` command would:
1. Calculate what files the server is missing
2. Upload only the delta
3. Register the catalog atomically

### Problem 3: Catalog Format Versioning

The catalog format is embedded in the filename:

```go
// From htfs/library.go
func CatalogName(key string) string {
    return fmt.Sprintf("%sv12.%s", key, common.Platform())
}
```

That `v12` means "catalog format version 12." When the format changes, old catalogs
become invisible. There's no migration path—you rebuild from scratch.

### Problem 4: No Garbage Collection Knobs

Cleanup is all-or-nothing:

```go
// From conda/cleanup.go
func spotlessCleanup(dryrun, noCompress bool) error {
    // Removes EVERYTHING
    fail.Fast(safeRemove("catalogs", common.HololibCatalogLocation()))
    fail.Fast(safeRemove("cache", common.HololibLocation()))
}
```

There's no:
- "Delete catalogs unused for 30 days"
- "Keep the 10 most recently used environments"
- "Reduce library to only files referenced by existing catalogs"

The `rcc configuration cleanup` command exists, but it's a blunt instrument.

### Problem 5: Opaque Error Messages

When things go wrong, the errors are cryptic:

```go
// From htfs/functions.go
panic(fmt.Errorf("Content for %q [%s] is missing; hololib is broken, requires check!", ...))
```

"Hololib is broken, requires check" tells you something is wrong but not:
- Which environment was affected
- What caused the corruption
- Whether the problem is recoverable
- What `rcc holotree check` will actually do

### Problem 6: Single-Writer Limitation

Holotree uses file locking to prevent concurrent writes:

```go
// From htfs/library.go
locker, err := pathlib.Locker(lockfile, 30000, common.SharedHolotree)
```

In shared mode (`rcc holotree shared --enable`), multiple users can read from the
same library, but only one can write at a time. In CI/CD environments with multiple
parallel jobs, this creates bottlenecks.

### Problem 7: Platform Coupling

Catalogs are platform-specific:

```go
func CatalogName(key string) string {
    return fmt.Sprintf("%sv12.%s", key, common.Platform())  // e.g., "linux64"
}
```

An environment built on `linux64` cannot be restored on `darwin64`. This is
fundamentally correct (binary packages differ), but the UX doesn't help users
understand or work around it.

### Problem 8: Virtual Mode Limitations

The "virtual" holotree mode (for `--liveonly` containers) has limited functionality:

```go
// From htfs/virtual.go
func (it *virtual) Export([]string, []string, string) error {
    return fmt.Errorf("Not supported yet on virtual holotree.")
}

func (it *virtual) Remove([]string) error {
    return fmt.Errorf("Not supported yet on virtual holotree.")
}
```

Container users hit these walls unexpectedly.

---

## Part IV: How We Plan to Improve It

### Immediate: Documentation and Discoverability

**Goal:** The "aha moment" should come in 5 minutes, not 5 hours.

1. **`rcc man holotree`** — Comprehensive guide explaining the Git-like model
2. **Quick-start examples** — Show the 10-minute → 10-second transformation
3. **Error message improvements** — Every error should suggest a fix

### Near-Term: Remote UX Revamp

**Goal:** `rcc remote` commands that feel like `docker login` + `docker pull`.

| Command | Purpose |
|---------|---------|
| `rcc remote status` | Show configured remote, test connectivity |
| `rcc remote catalogs` | List available catalogs on remote |
| `rcc remote serve` | Wrapper around `rccremote` for discoverability |

**Implementation:**
- New `cmd/remote.go` command group
- `rccremote` remains the actual server binary
- Improve error messages when `RCC_REMOTE_ORIGIN` is not set

### Medium-Term: Git-Like Commands

**Goal:** Users familiar with Git should feel at home.

```bash
rcc ht status       # Summary: spaces, catalogs, remote config, disk usage
rcc ht diff <a> <b> # Compare two blueprints (show what changed)
rcc ht push         # Upload catalog to remote (inverse of pull)
rcc ht gc           # Intelligent garbage collection
```

**`rcc ht status` implementation sketch:**

```go
func holotreeStatus() {
    // Show library stats
    fmt.Printf("Library: %s (%d files, %s)\n",
        common.HololibLibraryLocation(),
        countFiles(),
        humanSize())

    // Show active spaces
    fmt.Printf("\nSpaces (%d):\n", len(spaces))
    for _, space := range spaces {
        fmt.Printf("  %s  %s  %s\n", space.Name, space.Blueprint[:8], space.LastUsed)
    }

    // Show remote config
    if origin := common.RccRemoteOrigin(); origin != "" {
        fmt.Printf("\nRemote: %s\n", origin)
    } else {
        fmt.Printf("\nRemote: (not configured)\n")
    }
}
```

**`rcc ht gc` implementation sketch:**

```go
func holotreeGC(keepDays int, dryRun bool) {
    // Find catalogs not used in N days
    cutoff := time.Now().AddDate(0, 0, -keepDays)
    stale := findStaleCatalogs(cutoff)

    // Find library files not referenced by any remaining catalog
    referenced := collectReferencedHashes(activeCatalogs)
    orphans := findOrphanedFiles(referenced)

    // Remove (or report in dry-run mode)
    for _, hash := range orphans {
        if dryRun {
            fmt.Printf("Would remove: %s\n", hash)
        } else {
            removeFromLibrary(hash)
        }
    }
}
```

### Long-Term: Push Protocol

**Goal:** Complete the bidirectional sync story.

Current state: Environments flow one direction (server → client).

Desired state: Any node can push to any other node.

**Protocol sketch:**

```
Client                              Server
   |                                   |
   |---[POST /push/catalog_hash]------>|
   |                                   |
   |<--[200: need hashes a,b,c]--------|
   |                                   |
   |---[POST /upload body=zip]-------->|
   |                                   |
   |<--[201: catalog registered]-------|
```

**Implementation considerations:**
- Authentication (currently just `RCC_REMOTE_AUTHORIZATION` header)
- Server-side validation (don't accept malformed catalogs)
- Conflict resolution (what if catalog already exists?)

### Architecture: Improvements Without Breaking Changes

1. **Catalog versioning** — Add format version to catalog JSON, not filename
2. **Migration support** — Auto-upgrade old catalogs when loaded
3. **Better locking** — Read/write locks instead of exclusive locks
4. **Streaming export** — Stream zip creation instead of buffering in memory

### Metrics and Observability

The journal system already tracks build statistics:

```go
// From journal/buildstats.go
type BuildEvent struct {
    Version       string  `json:"version"`
    BlueprintHash string  `json:"blueprint"`
    Started       float64 `json:"started"`
    RestoreDone   float64 `json:"restore"`
    Dirtyness     float64 `json:"dirtyness"`
    // ...
}
```

We could expose this via:
- `rcc ht stats --json` for machine-readable output
- Prometheus metrics endpoint for monitoring
- OpenTelemetry spans for distributed tracing

---

## Conclusion

Holotree is genuinely innovative infrastructure. The content-addressed architecture,
relocation system, and delta transfer protocol solve real problems elegantly. What
it lacks is polish—the documentation, error messages, and command structure that
turn good technology into a great user experience.

The work ahead is clear:
1. **Document what exists** — Most of the functionality is already there
2. **Improve discoverability** — Make the happy path obvious
3. **Fill the gaps** — Push, intelligent GC, better status reporting
4. **Keep it focused** — Holotree is environment infrastructure, not a product

RCC is "Git for environments." Holotree is the object database. Our job is to make
that mental model obvious and the experience seamless.

---

## Appendix: Key Source Files

| File | Purpose |
|------|---------|
| `htfs/library.go` | Core hololib operations (New, Record, Restore, Export) |
| `htfs/directory.go` | Tree structure, catalog format, JSON serialization |
| `htfs/functions.go` | File operations (Locator, DropFile, RestoreDirectory) |
| `htfs/virtual.go` | Virtual holotree for container/liveonly mode |
| `htfs/ziplibrary.go` | Reading environments directly from hololib.zip |
| `htfs/commands.go` | High-level operations (NewEnvironment, RecordEnvironment) |
| `remotree/server.go` | rccremote HTTP server |
| `remotree/delta.go` | Delta transfer (missing file calculation, zip creation) |
| `operations/pull.go` | Client-side pull operation |
| `journal/buildstats.go` | Build event tracking and statistics |
| `common/algorithms.go` | Hashing (SipHash, SHA-256, BlueprintHash) |

---

## Appendix: Holotree Command Reference

| Command | What It Does |
|---------|--------------|
| `rcc ht list` | List active holotree spaces |
| `rcc ht catalogs` | List available catalogs with metadata |
| `rcc ht statistics` | Build/runtime stats over time |
| `rcc ht check` | Verify library integrity, remove corrupted entries |
| `rcc ht export` | Export catalog + library to hololib.zip |
| `rcc ht import` | Import hololib.zip to local library |
| `rcc ht pull` | Download catalog from remote |
| `rcc ht hash` | Calculate blueprint hash from conda.yaml |
| `rcc ht blueprint` | Verify blueprint exists in library |
| `rcc ht delete` | Remove holotree spaces |
| `rcc ht remove` | Remove catalogs from library |
| `rcc ht shared` | Enable/disable shared holotree mode |
| `rcc ht init` | Initialize shared holotree location |
| `rcc ht prebuild` | Build catalogs from environment descriptors |
| `rcc ht variables` | Output environment variables for a space |
| `rcc ht venv` | Create user-managed venv in automation folder |
| `rcc ht plan` | Show installation plans for spaces |
| `rcc ht bootstrap` | Build environments from template set |
| `rcc ht build-from-bundle` | Build from single-file bundle |

---

## Appendix: State of the Art — What We Can Learn From

Holotree doesn't exist in isolation. Other systems have tackled similar problems—
content-addressed storage, environment reproducibility, efficient distribution—and
we can learn from their approaches.

### Nix: The Gold Standard for Reproducibility

**What it is:** A purely functional package manager where packages are stored in
`/nix/store` with cryptographically-derived paths like `/nix/store/b6gvzjyb2pg0…-hello-2.10`.

**Key innovations:**

1. **Derivation-based addressing** — The hash captures the entire build dependency
   graph, not just the output. Change one build flag? Different hash. Different path.

2. **Immutable store** — Packages are never overwritten after they're built. Multiple
   versions coexist without conflict.

3. **Closure completeness** — A package's "closure" includes every transitive
   dependency. Copy the closure, and it works anywhere.

4. **Garbage collection** — Only packages reachable from GC roots are kept. Everything
   else is eligible for deletion.

**What Holotree could adopt:**
- **GC roots concept** — Define what catalogs/spaces should be kept, garbage collect
  everything else
- **Closure tracking** — Export environments with guaranteed completeness
- **Derivation hashing** — Hash the build inputs, not just outputs, for stronger
  reproducibility guarantees

**Why we can't just use Nix:**
- Nix requires the Nix daemon and store
- Python ecosystem tooling (pip, conda) doesn't integrate natively
- Overkill for "just run this Python script" use case

---

### Docker: Layered Content Addressing

**What it is:** Container images are composed of read-only layers, each identified
by a cryptographic hash. Containers add a writable layer on top.

**Key innovations:**

1. **Layer sharing** — If two images share base layers (e.g., `python:3.10`), those
   layers are stored once and shared.

2. **Copy-on-write** — The writable container layer only stores modifications. The
   underlying image layers are never touched.

3. **Content-addressable distribution** — Registries serve layers by hash. Pull only
   the layers you don't already have.

**What Holotree could adopt:**
- **Explicit layer model** — Holotree already does this (micromamba layer, pip layer)
  but could make it more visible
- **Registry protocol** — Standard API for pushing/pulling catalogs and library files
- **Manifest format** — Docker manifests are well-documented; Holotree's catalog
  format could benefit from similar spec

**Why we can't just use Docker:**
- Container overhead for simple automations
- Not all environments need full isolation
- Python environment paths need to be predictable (for IDE integration, etc.)

---

### OSTree: Git-Like OS Deployment

**What it is:** A system for managing bootable filesystem trees using content-addressed
storage and hardlinks.

**Key innovations:**

1. **Hardlink deployment** — Files are "checked out" from the object store via hardlinks.
   The same file appears in multiple places but uses disk space once.

2. **Atomic transactions** — Upgrades and rollbacks are atomic. Either the new version
   is fully deployed, or the old version remains.

3. **Delta updates** — HTTP-based updates transfer only the changed objects, using
   static deltas for common upgrade paths.

**What Holotree could adopt:**
- **Hardlink restoration** — Instead of copying files from library to space, hardlink
  them (where filesystem allows)
- **Static deltas** — Pre-compute common upgrade paths (e.g., v1.0→v1.1) as single
  delta files
- **Atomic space updates** — Prepare new space, atomic swap, cleanup old

**Current Holotree status:**
- Already uses file copying (not hardlinks) for restoration
- Already supports delta transfers via rccremote
- Doesn't have atomic swap for space updates

---

### uv: Modern Python Package Management

**What it is:** A fast Python package manager written in Rust that uses aggressive
caching and deduplication.

**Key innovations:**

1. **Global cache with deduplication** — A single cache directory serves all projects,
   avoiding redundant storage of common packages.

2. **HTTP cache awareness** — Respects cache headers from registries, avoiding
   unnecessary downloads.

3. **Pruning support** — `uv cache prune` removes entries not used by any known project.

4. **Thread-safe, append-only** — Cache is safe for concurrent access without locks.

**What Holotree could adopt:**
- **Smarter cache invalidation** — Track which catalogs reference which library files
- **Automatic pruning** — Remove orphaned library files during routine operations
- **Lock-free reads** — Allow concurrent reads even during writes (MVCC-style)

**Relevant insight:**
uv's cache is simpler than Holotree (it caches wheels, not complete environments)
but achieves similar deduplication benefits. The difference: uv still needs to
"install" packages each time, while Holotree restores complete environments instantly.

---

### Restic: Efficient Backup Deduplication

**What it is:** A backup program that deduplicates data at the chunk level before
storing it.

**Key innovations:**

1. **Content-defined chunking** — Instead of fixed-size blocks, chunks are determined
   by content boundaries. This means small edits don't cascade through all chunks.

2. **Repository format** — All data is encrypted and stored by content hash in a
   well-defined structure.

3. **Incremental-only** — Every backup is "incremental" by construction. If a file
   hasn't changed, its chunks already exist.

**What Holotree could adopt:**
- **Sub-file deduplication** — Large files with small changes could share common chunks
  (probably overkill for Python environments, where files rarely change partially)

**Why this probably doesn't apply:**
- Python environments consist of many small-to-medium files
- Files either change completely (new version) or not at all
- Per-file deduplication is sufficient

---

### pixi / rattler: Modern Conda Tooling

**What it is:** pixi is a modern package manager built on the rattler library, which
provides a Rust implementation of conda functionality.

**Key innovations:**

1. **Lock files as first-class** — Every project has a `pixi.lock` that captures exact
   package versions and hashes.

2. **Cross-language support** — Not just Python—supports R, C++, etc. from conda-forge.

3. **Global tool installation** — `pixi global install` for system-wide tools.

**What Holotree could adopt:**
- **Better lock file integration** — RCC's `conda.yaml` + Holotree blueprint could be
  more explicit about lock semantics
- **Tool isolation** — Each tool could get its own environment without manual management

**Relevant insight:**
pixi and RCC solve similar problems from different angles. pixi focuses on development
workflow; RCC focuses on deployment. Holotree is RCC's deployment advantage.

---

### IPFS: Content Identifiers (CIDs)

**What it is:** A distributed content-addressable storage network where every piece
of data is identified by its cryptographic hash.

**Key innovations:**

1. **Self-describing hashes** — CIDs include metadata about the hash function used,
   making the format future-proof.

2. **Multihash** — A standard for hash function identification, allowing transition
   between algorithms.

3. **Content addressing at scale** — The same content uploaded by different people
   resolves to the same identifier.

**What Holotree could adopt:**
- **Versioned hash format** — Instead of hardcoding SipHash, include algorithm
  identifier in stored hashes
- **Migration path** — When switching hash algorithms, both old and new formats
  can coexist

**Current Holotree status:**
- Uses SipHash-128 with a fixed seed
- Catalog format version (`v12`) is embedded in filename, not content
- No mechanism for hash algorithm migration

---

### Synthesis: What Holotree Does Well vs. Improvement Opportunities

| Aspect | Holotree Status | Best-in-Class | Gap |
|--------|-----------------|---------------|-----|
| Content addressing | SipHash-128, per-file | SHA-256 (Git, Docker), Blake3 (modern) | Algorithm is fast but not self-describing |
| Deduplication | Per-file | Per-file (Docker, OSTree), per-chunk (restic) | Sufficient for use case |
| Distribution | Delta via rccremote | Delta via HTTP (Docker, OSTree) | Works but underdocumented |
| Garbage collection | Manual cleanup | Reference counting (Nix), pruning (uv) | Major gap |
| Reproducibility | Blueprint hash | Derivation hash (Nix), lock file (pixi) | Good, could be better |
| Atomic updates | File-by-file restore | Atomic swap (OSTree), transactional (Nix) | Opportunity for improvement |
| Format evolution | Version in filename | Self-describing (IPFS CID) | Migration is hard |

---

### Concrete Improvement Ideas (Ranked by Impact)

**High Impact, Low Effort:**

1. **Reference counting for library files** — Track which catalogs reference each
   library file. Enable `rcc ht gc` to remove orphans safely.

2. **Hardlink restoration option** — On filesystems that support it (most Linux),
   hardlink instead of copy. Massive speed improvement for large environments.

3. **Better error messages** — Every error should say what went wrong and what to do.

**High Impact, Medium Effort:**

4. **`rcc ht push`** — Complete the bidirectional sync story. Essential for teams
   sharing environments.

5. **Atomic space updates** — Prepare new space in temp location, atomic rename to
   final location. Avoids partial states.

6. **Self-describing hash format** — Include algorithm identifier in stored hashes.
   Enables future migration without breaking existing libraries.

**Medium Impact, High Effort:**

7. **Layer-aware caching** — Expose the micromamba/pip layer split more explicitly.
   Enable "rebuild just the pip layer" workflows.

8. **Registry protocol** — Standard HTTP API for catalog/library operations. Enable
   third-party tooling.

9. **Prometheus metrics** — Export build times, cache hit rates, library sizes for
   observability.

**Research-Level (Future):**

10. **Nix-style derivation hashing** — Hash the build inputs, not just outputs. Would
    require deep integration with conda/pip solvers.

11. **Content-defined chunking** — Sub-file deduplication for large binary files.
    Probably not worth the complexity.

12. **P2P distribution** — IPFS-style peer discovery for rccremote servers. Would
    enable truly decentralized environment sharing.

---

### The Path Forward

Holotree is already in the top tier of environment management systems. It's more
sophisticated than pip's cache, more focused than Nix, and more practical than
containerizing everything.

The improvements that matter most are:

1. **Documentation** — Help people discover what already exists
2. **Garbage collection** — Don't let libraries grow unbounded
3. **Push support** — Complete the distribution story
4. **Hardlinks** — Free performance win on supported systems

Everything else is optimization. Get these four right, and Holotree becomes the
obvious choice for Python environment management at scale
