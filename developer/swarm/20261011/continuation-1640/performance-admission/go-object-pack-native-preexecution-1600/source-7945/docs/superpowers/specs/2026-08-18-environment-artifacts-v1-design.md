# Environment Artifacts v1 Design

## Status

Approved in chat on 2026-08-18 after repository archaeology and an independent
Hermes/Terra architecture review. This document specifies only the first
architecture-defining vertical of issue #118.

## Objective

Prove that RCC can build one real environment with its current builder, wrap
the unchanged v12 catalog and Hololib bytes in a cryptographically identified
artifact, publish that artifact to a filesystem-backed HTTP provider, acquire
it into an empty worker home, materialize it with the existing v12 Drop
machinery, execute Python without package-network access, and reuse the local
materialization on a second acquisition.

The implementation target is Linux amd64. Existing local RCC workflows and the
legacy rccremote protocol remain unchanged.

## Non-goals

This vertical does not implement zstd, packfiles, FUSE, hardlinks, reflinks,
OCI, SBOM generation, signatures, a TUI, Kubernetes, Rails or Kamal
management, Action Runtime integration, a distributed scheduler, production
remote authentication, provider garbage collection, resumable uploads, tags,
or mutable references.

It does not rewrite the v12 catalog format or current Hololib object bytes.

## Architectural decision

Add an artifact lifecycle beside the current Holotree lifecycle:

```text
current RCC builder
  -> exact legacy blueprint bytes
  -> unchanged v12 catalog and Hololib objects
  -> artifact inventory and canonical metadata
  -> immutable filesystem-backed HTTP provider
  -> verified worker-local legacy installation
  -> in-memory portable v12 reconstruction view
  -> existing MakeBranches / RestoreDirectory / DropFile machinery
  -> local materialization record
  -> bounded execution lease and fresh execution handle
```

The current `BlueprintHash`, catalog filename, and catalog object IDs are
compatibility keys. They remain necessary to drive the v12 reader but are not
new remote trust identities, even when a historical key happens to use
SHA-256. New provider and artifact trust uses explicit SHA-256 descriptors over
the exact bytes transferred and stored.

The artifact is an exact-build identity. Because an unchanged v12 catalog
contains its producer path and identity, two builds with equivalent semantic
inputs can produce different catalog and artifact digests. The semantic
specification digest is the stable comparison key; the artifact digest binds
the exact catalog and objects that were actually published.

## Package boundaries

### `environmentartifact`

Owns schema types, canonical encoding, digest calculation, validation, and
v12 inventory metadata. It has no HTTP, command, or process execution logic.

### `artifactprovider`

Owns the provider interface, strict filesystem content-addressable storage,
HTTP client, HTTP handler, missing-object negotiation, and atomic manifest
commit. It does not understand Holotree materialization.

### `environmentlifecycle`

Owns build/publish orchestration, verified acquisition, installation into
legacy local paths, materialization records, leases, execution handles, and
process execution. It adapts current `htfs` and `conda` behavior instead of
duplicating them.

### `htfs`

Retains the v12 catalog and object reader, relocation logic, and Drop
implementation. It gains only the narrow portable reconstruction seam needed
to decode an immutable producer catalog into worker-local derived state.

### `cmd`

Adds thin, JSON-capable commands:

```text
rcc env publish
rcc env acquire
rcc env exec
rcc cache serve
```

The commands translate flags into typed lifecycle inputs and render typed
results. Business rules do not live in Cobra handlers.

## Identity model

### Digest syntax

All new digests use lowercase `sha256:<64 lowercase hex characters>`. Parsers
reject unknown algorithms, uppercase or mixed-case hex, wrong lengths,
surrounding whitespace, path separators, and non-canonical spellings.

### Semantic specification

`specification` describes the canonical dependency/build semantics for this
artifact. In this first vertical its content is a canonical JSON projection
derived from the current normalized blueprint plus platform and builder
compatibility fields. It is not consumed by the v12 reader.

Required fields:

```text
mediaType
digest
size
sourceKind
platform os/arch/rccPlatform
builder kind/rccVersion/compatibilityKey
```

Its bytes are stored as an immutable provider blob.

### Legacy blueprint

`legacyBlueprint` is a separate descriptor for the exact normalized YAML bytes
produced by `htfs.ComposeFinalBlueprint` and consumed by current v12 lookup and
restoration.

Required invariants:

```text
sha256(exact legacy blueprint bytes) == legacyBlueprint.digest
common.BlueprintHash(exact legacy blueprint bytes) == legacyBlueprintKey
htfs.CatalogName(legacyBlueprintKey) == catalog.legacyName
```

The exact bytes are stored as an immutable provider blob. The semantic
specification and legacy blueprint may describe the same inputs, but their
formats and purposes are not conflated.

### Catalog

The manifest contains one catalog descriptor in this vertical:

```text
mediaType: application/vnd.rcc.holotree.catalog.v12+gzip
digest: SHA-256 of the exact existing catalog file bytes
size: exact stored byte count
legacyName: exact expected v12 platform filename
```

No field from the decoded catalog is copied into the manifest as a substitute
for the exact catalog descriptor.

### Stored objects

Each non-symlink catalog file object appears exactly once in the Object Index.
Each entry contains:

```text
legacyObjectId
storedDigest
storedSize
logicalSize
encoding
legacyLogicalDigestAlgorithm
```

`storedDigest` hashes the exact bytes at the existing Hololib object path.
`legacyObjectId` remains the ID referenced by the v12 catalog. Duplicate legacy
IDs must have identical descriptors and logical sizes or inventory fails.

The first vertical supports one uniform legacy materializer mode per artifact:

```text
encoding: gzip
legacyLogicalDigestAlgorithm: sha256
```

Inventory rejects raw objects, mixed modes, malformed gzip streams, logical
content that does not match its legacy object ID, and a producer home with
`compress.no` active. Acquisition never creates, removes, or toggles
`compress.no`. A worker whose current Hololib mode is incompatible fails before
installing content.

### Object Index

The Object Index is compact canonical JSON with entries sorted by
`legacyObjectId`. It also records:

```text
mediaType
schemaVersion
count
totalStoredBytes
totalLogicalBytes
encoding
legacyLogicalDigestAlgorithm
entries
```

The manifest references the exact Object Index bytes by SHA-256 and size.

### Manifest and artifact digest

The manifest contains:

```text
mediaType
schemaVersion
artifactDigest
specification descriptor
legacyBlueprint descriptor and legacyBlueprintKey
platform
builder compatibility
catalog descriptors
objectIndex descriptor
requirements
```

The artifact digest is SHA-256 over a separately typed identity projection.
That projection contains every semantic field above except the self-referential
`artifactDigest`. Provider locations, local paths, signatures, attestations,
timestamps, and mutable references do not exist in Manifest v1.

Canonical encoding uses structs rather than identity-bearing maps, compact
JSON, declared field order, sorted slices, UTF-8, and no insignificant
whitespace. Decoders reject duplicate keys, unknown fields, trailing content,
unknown required features, and non-canonical digest representations. Golden
vectors fix the exact encoded bytes and digests.

## Artifact validation

Validation occurs before any destination path is derived or written.

Manifest validation proves:

- exact media type and schema version;
- canonical artifact digest;
- supported Linux amd64 platform;
- supported `v12` catalog reader;
- supported gzip/SHA-256 legacy materializer mode;
- one catalog with the exact expected legacy name;
- canonical, unique descriptors and internally consistent sizes;
- no unknown required feature.

Object Index validation proves:

- canonical index digest and declared totals;
- strictly sorted unique entries;
- valid legacy IDs and new digests;
- uniform supported encoding and logical digest algorithm;
- no conflicting duplicate legacy identity.

Decoded catalog validation proves:

- expected platform and legacy blueprint key;
- only relative, single-component directory and file names;
- no empty, `.`, `..`, absolute, volume-prefixed, or separator-containing
  names;
- no duplicate logical paths or file/directory collisions;
- modes are within the supported v12 file/directory/symlink surface;
- rewrite offsets are non-negative, ordered, non-overlapping, within logical
  file bounds, and sized for the recorded producer identity;
- every non-symlink file references exactly one Object Index entry;
- symlink targets are valid for current conda environments but cannot escape
  the materialization root when resolved;
- the catalog contains no unindexed stored object reference.

SHA-256 and declared size are verified for the semantic specification, legacy
blueprint, catalog, Object Index, and every stored object. Gzip objects are
also decompressed with bounded reads and verified against their legacy logical
ID before publication and before initial installation.

## Provider contract

### Interface

The core provider exposes typed operations equivalent to:

```text
Capabilities
ResolveManifest
MissingObjects
PutObject
GetObject
CommitManifest
```

Objects include semantic specifications, legacy blueprints, catalogs, Object
Indexes, and Hololib stored bytes. They share one SHA-256-addressed blob
namespace.

### HTTP surface

```text
GET  /v1/capabilities
POST /v1/objects/missing
PUT  /v1/objects/sha256/<hex>
GET  /v1/objects/sha256/<hex>
POST /v1/manifests/sha256/<hex>/commit
GET  /v1/manifests/sha256/<hex>
```

There are no tags or mutable names in this vertical.

### Strict filesystem CAS

The filesystem provider owns an exclusive storage root. All request-derived
paths are constructed only after strict digest parsing; arbitrary path
segments are never joined into the root.

The provider root and every existing descendant traversed to reach a digest
path are inspected without following symlinks. Digest fanout components are
created or validated as real directories beneath the already-opened provider
root. A valid digest filename never authorizes traversal through a symlinked
parent component.

Blob publication:

1. Create a private temporary regular file in a dedicated temporary directory
   on the destination filesystem using exclusive creation and no-follow
   semantics.
2. Stream the request through a SHA-256 digester with a declared-size limit.
3. Verify exact size and digest.
4. Flush and fsync the file.
5. Check the destination with `lstat`; reject symlinks and non-regular files.
6. Atomically rename into its immutable digest path.
7. Fsync the destination parent directory.
8. If an identical destination already exists, re-open without following
   symlinks, re-hash it, and return idempotent success. Mismatched existing
   bytes fail closed.

The implementation uses a dedicated CAS primitive, not
`pathlib.TryRename` or `pathlib.IsFile`.

### Atomic manifest commit

Commit accepts canonical manifest bytes whose URL digest matches their
artifact digest. Under a provider-local commit lock it:

1. Parses and fully validates the manifest and Object Index.
2. Re-opens every referenced CAS blob as a regular no-follow file.
3. Re-checks exact size and SHA-256 from the stored bytes.
4. Writes the canonical manifest to a private same-filesystem temporary file.
5. Fsyncs the file, atomically renames it to the immutable manifest path, and
   fsyncs the parent directory.

The provider has no delete or GC operation in this vertical, so provider-owned
mutations cannot invalidate a commit between verification and publication.
External mutation of the provider root is outside the filesystem provider's
trust boundary; every acquisition still verifies all bytes independently.

Interrupted uploads remain unreachable because no manifest references them
until commit completes. Concurrent identical puts and commits are idempotent.
Conflicting content under one digest fails closed.

## Publication flow

`rcc env publish --robot <robot.yaml> --provider <url> --json` performs:

1. Resolve exact normalized legacy blueprint bytes through current RCC code.
2. Build with the current RCC builder and `htfs.RecordEnvironment` path if the
   legacy catalog is not already complete.
3. Locate the exact v12 catalog and referenced Hololib objects.
4. Validate the producer catalog and uniform supported legacy mode.
5. Construct and hash the semantic specification, legacy blueprint
   descriptor, catalog descriptor, Object Index, and Manifest v1.
6. Ask the provider which immutable blobs are missing.
7. Upload only missing blobs.
8. Atomically commit the complete manifest.
9. Return the artifact digest, specification digest, legacy blueprint key,
   counts, uploaded bytes, reused bytes, and provider reference.

The current local build path is not rerouted through the provider. Provider
failure does not alter or invalidate the locally built environment.

## Acquisition flow

`rcc env acquire --artifact <sha256:...> --provider <url> --json` performs:

1. Check the local canonical manifest cache by immutable artifact digest.
2. If absent, fetch the manifest and verify its canonical artifact digest.
3. Resolve and verify the Object Index, semantic specification, legacy
   blueprint, and catalog.
4. Validate platform, requirements, legacy compatibility, catalog tree, and
   symlink/rewrite safety before writing legacy paths.
5. For each required object, verify an existing local legacy path against its
   exact stored descriptor or fetch it into a same-directory temporary file,
   verify it, and atomically install it.
6. Install the exact catalog bytes at the expected legacy catalog name through
   the same verify-then-rename discipline.
7. Materialize through the portable v12 adapter.
8. Atomically write a ready local materialization record.
9. Return artifact identity, materialization identity/path, and cache-hit
   provenance.

Local paths are derived from validated legacy IDs only after exact shape and
hex-length checks. Existing mismatched local content fails closed in this
vertical rather than being silently overwritten.

## Portable v12 materializer adapter

An unchanged imported catalog encodes A's absolute stage path. Calling current
`Library.Restore` directly on B would target A's base or fail
`Root.Relocate`. The stored catalog must remain unchanged, so the adapter:

1. Opens and decodes the verified v12 catalog into a `Root` reconstruction
   view.
2. Preserves the producer identity string used to discover rewrite offsets.
3. Validates that the producer identity and B's target space label have the
   exact equal byte length required by v12 relocation.
4. Replaces only the decoded view's base path with B's
   `common.HolotreeLocation()` while retaining a same-length identity.
5. Selects B's worker-local target label.
6. Runs the existing `Relocate`, `MakeBranches`, `RestoreDirectory`,
   `DropFile`, metadata save, and logical digest verification behavior.

The adapter does not serialize the rebased view back to the catalog and does
not change stored artifact bytes. Tests include files with rewrite offsets and
legitimate relative symlinks.

## Materialization records and warm acquisition

Worker-local records live below `ROBOCORP_HOME` in an artifact-specific area,
separate from the legacy catalog and object paths. Records are derived state
and never enter artifact identity.

A materialization record contains:

```text
artifactDigest
legacyBlueprintKey
materializationId
path
state
createdAt
verifiedAt
```

State transitions are:

```text
verified-content -> materializing -> ready
```

Only `ready` records are reusable or leaseable. Failed or interrupted
materialization never produces a ready record.

A warm second acquire:

- verifies the canonical local manifest and ready record;
- verifies the materialization metadata file matches the expected legacy
  blueprint and target path;
- checks the materialization root and Python executable still exist as regular
  files/directories;
- returns `cacheHit: local-materialization`;
- performs no provider request, package build, solver, or package-network
  operation.

The warm-path acceptance test supplies provider and builder implementations
that fail immediately if any method is invoked after the first acquire. Zero
calls are therefore a correctness invariant, not a timing inference.

Full live-space content hashing is not required for the warm path because
current Holotree spaces are intentionally writable derived state. Deletion or
metadata mismatch invalidates the record and rematerializes from verified
local legacy content.

## Leases and execution handles

`rcc env acquire` creates or reuses a materialization but does not return a
process lease owned by a CLI that has already exited.

CLI v1 exposes leases only through process-scoped `env exec`; the internal
typed lifecycle retains explicit `Lease`, `ExecutionHandle`, and `Release`
operations for trusted embedded or runtime adapters. `env acquire` never
returns a transferable lease capability.

`rcc env exec`:

1. Acquires or reuses a ready materialization.
2. Creates a local lease immediately before spawning the child.
3. Records owner PID plus process-start identity, artifact digest,
   materialization ID, and creation time.
4. Derives a fresh execution handle through existing
   `conda.CondaExecutionEnvironment` behavior.
5. Runs the requested child command with the derived environment.
6. Forwards termination signals, waits for child exit, and releases the lease
   idempotently.

The execution handle contains derived local data:

```text
artifactDigest
materializationId
leaseId
cwd
executable
environment key/value entries
cacheHit
```

Leases are local resource-protection records, not distributed Action Run or
Attempt ownership. This vertical does not implement GC, but the lease contract
is shaped so future GC can refuse to reclaim an actively leased
materialization.

## CLI contract

### Provider server

```text
rcc cache serve --root <directory> --listen 127.0.0.1:0 --json
```

The JSON startup record includes the effective listen URL and storage root.
The server shuts down cleanly on SIGINT or SIGTERM.

### Publish

```text
rcc env publish --robot <robot.yaml> --provider <url> --json
```

### Acquire

```text
rcc env acquire --artifact <sha256:...> --provider <url> --json
```

If the artifact is fully available locally, `--provider` may be omitted and
no network request is attempted.

### Execute

```text
rcc env exec --artifact <sha256:...> --provider <url> --json -- \
  python -c "import <representative-package>"
```

The process exit code is the child exit code after lease cleanup. JSON mode
emits the execution handle and final child result without leaking secrets.

## Error handling

All stages fail closed with an error that identifies the failing layer:

- unsupported schema, feature, platform, encoding, or logical-ID algorithm;
- malformed or non-canonical digest;
- manifest, index, specification, blueprint, catalog, or object mismatch;
- unsafe catalog name, path, mode, symlink, or rewrite offset;
- incomplete provider commit;
- existing conflicting immutable content;
- incompatible worker Hololib mode;
- interrupted or failed materialization;
- invalid or stale materialization record;
- lease creation, child spawn, or lease cleanup failure.

No validation error falls back to a cold package build on B. No provider error
changes the behavior of existing no-provider local commands.

## Test strategy

### Inner-loop Go tests

1. Canonical Manifest and Object Index golden vectors.
2. Artifact identity projection and self-reference exclusion.
3. Digest parsing and unknown-field/duplicate-key rejection.
4. Legacy compatibility key versus exact stored-byte digest separation.
5. V12 inventory over real catalog/object fixtures without byte changes.
6. Gzip and legacy logical digest verification; raw/mixed-mode rejection.
7. Catalog traversal, unsafe symlink, invalid mode, and rewrite-offset
   rejection.
8. Strict CAS path, no-follow, regular-file, fsync/rename, idempotency, and
   conflict behavior, including rejection of a symlinked parent component.
9. Commit-before-complete rejection and concurrent identical publication.
10. HTTP capability, missing, PUT, GET, commit, and resolve behavior.
11. Empty-home verified installation and portable v12 rebase.
12. Interrupted materialization and ready-record transitions.
13. Lease ownership, child failure, signal cleanup, and idempotent release.
14. Warm acquisition with provider and builder implementations that fail the
    test immediately if invoked after the first acquire.

### Process A/B integration

Use a built candidate RCC binary and isolated temporary `ROBOCORP_HOME`
directories:

1. Start the filesystem-backed provider on loopback.
2. A builds a real small environment containing Python and a representative
   package using current RCC.
3. A publishes and returns an immutable artifact digest.
4. Remove or disable all package-source access for B while leaving only the
   loopback provider reachable. Set hostile HTTP/HTTPS package proxies and a
   loopback-only `NO_PROXY` exception for the provider.
5. Start B as a distinct process with an empty `ROBOCORP_HOME` set before RCC
   initialization.
6. B acquires, verifies, installs, rebases, and materializes.
7. B executes Python and imports the representative package.
8. Stop the provider or reset its request counter.
9. B acquires again and reports `local-materialization` with zero provider and
   builder calls.

The test also asserts that B created no package-manager download/cache evidence
associated with a cold build.

### Promotion gates

After command and schema behavior stabilize:

1. Focused package tests during every checkpoint.
2. Contained full Go unit suite.
3. Candidate binary build and direct CLI smoke.
4. Bounded Robot acceptance for the public A/B workflow.
5. Full existing Robot regression suite.

Linux amd64 is the only platform claimed by this vertical. Other platforms
must compile where practical but are not claimed as runtime-proven.

## Compatibility guarantees

- Existing `rcc run` and `rcc holotree variables` behavior is unchanged.
- Current v12 export/import and bundle readers remain unchanged.
- Legacy `/parts`, `/delta`, and `RCC_REMOTE_ORIGIN` behavior remains
  available and separate.
- No provider is required for normal local RCC use.
- Existing catalog and Hololib bytes remain byte-for-byte unchanged.
- Telemetry remains disabled and current endpoint/proxy controls remain
  available.

## Checkpoint sequence

1. Schema, canonicalization, and v12 inventory.
2. Strict filesystem CAS and HTTP provider atomic commit.
3. Verified acquisition and portable v12 materialization.
4. Materialization records, leases, execution handles, and warm path.
5. Full A/B CLI vertical and promotion gates.

Each checkpoint produces a candidate SHA, focused verification evidence, and
an independent Terra review when the checkpoint changes an architectural
contract. Only validated blockers are integrated.
