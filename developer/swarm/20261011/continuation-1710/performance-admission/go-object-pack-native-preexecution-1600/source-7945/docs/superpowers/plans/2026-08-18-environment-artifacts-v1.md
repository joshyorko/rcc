# Environment Artifacts v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the approved Linux-amd64 A-to-B environment-artifact vertical around unchanged v12 Holotree catalog and Hololib bytes.

**Architecture:** `environmentartifact` defines and validates immutable identities and v12 inventory, `artifactprovider` publishes those bytes through a strict filesystem CAS and loopback HTTP protocol, and `environmentlifecycle` orchestrates current RCC build/publish/acquire/materialize/lease/execute behavior. A narrow `htfs` adapter rebases only the decoded catalog view before invoking the existing v12 restoration machinery; Cobra handlers remain translation-only.

**Tech Stack:** Go 1.26.5, standard-library SHA-256/JSON/gzip/HTTP/filesystem primitives, existing RCC `htfs`, `conda`, Cobra, `hamlet`, and Robot Framework acceptance tests.

**Spec:** `docs/superpowers/specs/2026-08-18-environment-artifacts-v1-design.md`

## Global Constraints

- Target and runtime proof: Linux amd64 only.
- Preserve existing local RCC, v12 export/import/bundle, `/parts`, `/delta`, and `RCC_REMOTE_ORIGIN` behavior.
- Use `ROBOCORP_HOME` as the process-isolation boundary; do not introduce `RCC_HOME`.
- Treat `BlueprintHash`, catalog names, and Hololib object IDs only as legacy compatibility keys; new trust identities are lowercase `sha256:<64 hex>` over exact stored bytes.
- Keep imported v12 catalog and Hololib bytes byte-for-byte unchanged at rest.
- Artifact v1 supports only uniform gzip storage with SHA-256 logical IDs; reject `compress.no`, raw objects, and mixed modes without mutating that marker.
- Validate all untrusted metadata before deriving or writing destination paths.
- CAS traversal must not follow a symlink in the provider root or any provider-root descendant.
- `env acquire` returns no transferable lease; `env exec` owns a process-scoped lease while internal typed lease APIs remain callable by trusted adapters.
- A ready warm acquire must make zero provider and zero builder calls.
- Do not add zstd, packfiles, FUSE, hardlinks/reflinks, OCI, SBOM, auth, TUI, Kubernetes, Rails/Kamal, Action Runtime integration, scheduling, tags, mutable references, deletion, or GC.
- Write each production behavior only after its focused test has failed for the expected missing-behavior reason.

## File and interface map

- `environmentartifact/digest.go`: canonical SHA-256 digest parsing, hashing, and exact descriptor verification.
- `environmentartifact/canonical.go`: strict single-JSON-value decoding, duplicate/unknown-field rejection, and canonical re-encoding checks.
- `environmentartifact/manifest.go`: Manifest v1, semantic specification, descriptors, identity projection, canonical bytes, and validation.
- `environmentartifact/index.go`: Object Index v1 schema, ordering/totals validation, and canonical bytes.
- `environmentartifact/inventory.go`: immutable v12 catalog/object discovery and stored/logical digest verification.
- `environmentartifact/catalog.go`: hostile validation of decoded v12 paths, symlinks, modes, rewrites, and object references.
- `artifactprovider/provider.go`: provider-neutral request/result types and interface.
- `artifactprovider/filesystem.go`: strict no-follow filesystem CAS, missing negotiation, and atomic manifest commit.
- `artifactprovider/http.go`: HTTP handler/client implementing the provider interface.
- `environmentlifecycle/publish.go`: current-builder adapter and build/publish orchestration.
- `environmentlifecycle/acquire.go`: verified local manifest/content cache and cold/warm acquisition.
- `environmentlifecycle/materialize.go`: portable v12 catalog restore adapter.
- `environmentlifecycle/record.go`: atomic materialization state records.
- `environmentlifecycle/lease.go`: typed local lease lifecycle and execution handles.
- `environmentlifecycle/execute.go`: child launch, signal forwarding, exit propagation, and release.
- `htfs/portable.go`: narrow exported decoded-catalog operations needed by `environmentlifecycle`; no format changes.
- `cmd/environment.go`, `cmd/environmentPublish.go`, `cmd/environmentAcquire.go`, `cmd/environmentExec.go`: thin `rcc env` commands.
- `cmd/cache.go`, `cmd/cacheServe.go`: thin filesystem-provider server command.
- `robot_tests/environment_artifacts.robot` and `robot_tests/environment_artifacts/`: bounded real-process A/B acceptance fixture.

---

### Task 1: Canonical identities, Manifest v1, and Object Index

**Files:**
- Create: `environmentartifact/digest.go`
- Create: `environmentartifact/digest_test.go`
- Create: `environmentartifact/canonical.go`
- Create: `environmentartifact/canonical_test.go`
- Create: `environmentartifact/manifest.go`
- Create: `environmentartifact/manifest_test.go`
- Create: `environmentartifact/index.go`
- Create: `environmentartifact/index_test.go`

**Interfaces:**
- Produces:

```go
type Digest struct{ hex string }
func ParseDigest(value string) (Digest, error)
func DigestBytes(content []byte) Digest
func (d Digest) String() string
func (d Digest) Hex() string

type Descriptor struct {
    MediaType string `json:"mediaType"`
    Digest Digest `json:"digest"`
    Size int64 `json:"size"`
}
func VerifyDescriptor(desc Descriptor, content []byte) error

type Platform struct { OS, Arch, RCCPlatform string }
type Builder struct { Kind, RCCVersion, CompatibilityKey string }
type Specification struct { /* ordered fields from the approved spec */ }
type LegacyBlueprint struct { Descriptor Descriptor; LegacyBlueprintKey string }
type CatalogDescriptor struct { Descriptor Descriptor; LegacyName string }
type Requirements struct { CatalogReader, Encoding, LegacyLogicalDigestAlgorithm string; RequiredFeatures []string }
type Manifest struct { /* ordered Manifest v1 fields */ }
type manifestIdentity struct { /* every identity-bearing field except ArtifactDigest */ }
func NewManifest(input ManifestInput) (Manifest, []byte, error)
func DecodeManifest(content []byte) (Manifest, error)
func (m Manifest) CanonicalBytes() ([]byte, error)
func (m Manifest) Validate() error

type ObjectEntry struct {
    LegacyObjectID string `json:"legacyObjectId"`
    StoredDigest Digest `json:"storedDigest"`
    StoredSize int64 `json:"storedSize"`
    LogicalSize int64 `json:"logicalSize"`
    Encoding string `json:"encoding"`
    LegacyLogicalDigestAlgorithm string `json:"legacyLogicalDigestAlgorithm"`
}
type ObjectIndex struct { /* ordered fields from the approved spec */ }
func NewObjectIndex(entries []ObjectEntry) (ObjectIndex, []byte, error)
func DecodeObjectIndex(content []byte) (ObjectIndex, error)
func (i ObjectIndex) Validate() error
```

- [ ] **Step 1: Write digest parser and descriptor RED tests**

Add table tests proving only lowercase canonical `sha256:<64 hex>` parses, rejecting uppercase/mixed hex, whitespace, wrong algorithms/lengths, separators, and malformed input. Add `TestVerifyDescriptorRejectsWrongSizeAndDigest` over real byte slices.

- [ ] **Step 2: Verify digest tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact -run 'Test(ParseDigest|VerifyDescriptor)' -count=1`

Expected: FAIL because `ParseDigest` and descriptor verification do not exist.

- [ ] **Step 3: Implement canonical digest and descriptor verification**

Use `sha256.Sum256`, `hex.EncodeToString`, exact size comparison, and an unexported validated lowercase hex field so invalid `Digest` values cannot be constructed through JSON decoding.

- [ ] **Step 4: Verify digest tests are GREEN**

Run the Step 2 command; expected: PASS.

- [ ] **Step 5: Write strict canonical JSON and golden-vector RED tests**

Add tests named:

```go
func TestManifestGoldenCanonicalBytesAndArtifactDigest(t *testing.T)
func TestManifestDigestExcludesOnlySelfReference(t *testing.T)
func TestDecodeManifestRejectsUnknownDuplicateAndTrailingJSON(t *testing.T)
func TestObjectIndexGoldenCanonicalBytesAndDigest(t *testing.T)
func TestObjectIndexRequiresStrictlySortedUniqueEntriesAndExactTotals(t *testing.T)
func TestSemanticSpecificationAndLegacyBlueprintAreDistinctDescriptors(t *testing.T)
```

The golden assertions must contain literal compact JSON bytes and literal digest strings. Mutate each identity field independently and assert the artifact digest changes; mutate only `ArtifactDigest` before reconstruction and assert the identity projection is unchanged.

- [ ] **Step 6: Verify schema tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact -run 'Test(Manifest|DecodeManifest|ObjectIndex|Semantic)' -count=1`

Expected: FAIL because Manifest/Object Index construction and strict decoding do not exist.

- [ ] **Step 7: Implement schemas and canonicalization minimally**

Use ordered structs, compact `encoding/json`, sorted copied slices, a token pre-scan that rejects duplicate object keys recursively, `json.Decoder.DisallowUnknownFields`, and a second decode that must return `io.EOF`. `DecodeManifest` and `DecodeObjectIndex` must re-encode and require byte equality so non-canonical whitespace or ordering cannot become an alternate identity.

- [ ] **Step 8: Verify and refactor Task 1**

Run:

```sh
gofmt -w environmentartifact
GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact -count=1
git diff --check
```

Expected: PASS.

- [ ] **Step 9: Commit Task 1**

```sh
git add environmentartifact
git commit -m "feat(artifacts): define manifest and object index identities"
```

---

### Task 2: V12 inventory and hostile catalog validation

**Files:**
- Create: `environmentartifact/inventory.go`
- Create: `environmentartifact/inventory_test.go`
- Create: `environmentartifact/catalog.go`
- Create: `environmentartifact/catalog_test.go`
- Create: `environmentartifact/testdata/` fixtures generated by test helpers from actual `htfs.Root` values
- Create: `htfs/portable.go`
- Create: `htfs/portable_test.go`

**Interfaces:**
- Consumes: `Digest`, `Descriptor`, `ObjectEntry`, and `NewObjectIndex` from Task 1; current `htfs.NewRoot`, `Root.LoadFrom`, `Root.Treetop`, `htfs.ExactDefaultLocation`, `htfs.CatalogName`, and `common.BlueprintHash`.
- Produces:

```go
type InventoryInput struct {
    CatalogPath string
    LegacyBlueprint []byte
    ExpectedPlatform string
}
type Inventory struct {
    LegacyBlueprint Descriptor
    LegacyBlueprintKey string
    Catalog CatalogDescriptor
    Index ObjectIndex
    IndexBytes []byte
    Objects map[Digest]string // validated stored digest to producer file path
}
func InventoryV12(input InventoryInput) (Inventory, error)
func ValidateV12Catalog(root *htfs.Root, index ObjectIndex, producerIdentity string) error

type PortableCatalog struct { /* wraps decoded Root without serializing it */ }
func LoadPortableCatalog(filename string) (*PortableCatalog, error)
func (p *PortableCatalog) Root() *Root
func (p *PortableCatalog) Rebase(base, retainedIdentity string) error
func (p *PortableCatalog) Restore(library Library, target string) error
```

- [ ] **Step 1: Write legacy identity and unchanged-byte RED tests**

Create a real small gzip-mode catalog fixture with `htfs.Root.SaveAs` and Hololib objects produced using the current gzip path. Assert:

```go
sha256.Sum256(legacyBlueprintBytes) == inventory.LegacyBlueprint.Digest
common.BlueprintHash(legacyBlueprintBytes) == inventory.LegacyBlueprintKey
htfs.CatalogName(inventory.LegacyBlueprintKey) == inventory.Catalog.LegacyName
catalogBytesBefore == catalogBytesAfterInventory
objectBytesBefore == objectBytesAfterInventory
```

Also prove semantic specification bytes are never passed to `BlueprintHash` or catalog lookup.

- [ ] **Step 2: Verify inventory identity tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact -run 'TestInventoryV12(LegacyIdentity|PreservesStoredBytes)' -count=1`

Expected: FAIL because `InventoryV12` does not exist.

- [ ] **Step 3: Implement read-only inventory and logical verification**

Reject an existing `common.HololibCompressMarker()`. For each unique non-symlink file ID, open `htfs.ExactDefaultLocation(id)`, hash exact stored gzip bytes for `StoredDigest`/`StoredSize`, decompress through a bounded reader, hash logical bytes with SHA-256, and require lowercase 64-hex logical ID equality and exact `File.Size`. Reject duplicate legacy IDs with conflicting descriptors or logical sizes.

- [ ] **Step 4: Write hostile catalog RED matrix**

Add table tests covering absolute names, empty/`.`/`..` names, slash/backslash/volume names, duplicate logical paths, file-directory collisions, unsupported mode bits, escaping symlinks, negative/overlapping/out-of-bounds/wrong-width rewrites, missing/unindexed objects, raw gzip failure, and mixed logical algorithms. Include a valid relative symlink and valid fixed-offset rewrite fixture.

- [ ] **Step 5: Verify hostile validation tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact -run 'TestValidateV12Catalog|TestInventoryV12Rejects' -count=1`

Expected: FAIL because hostile catalog checks are absent.

- [ ] **Step 6: Implement catalog validation and portable decode seam**

Validate names component-by-component without cleaning them first; validate symlink resolution against a synthetic materialization root; sort rewrite offsets and reject mutation of their original order; require every rewrite span to equal `len(producerIdentity)`. `htfs.PortableCatalog.Rebase` may change only decoded `Root.Path` and retain the producer identity until `Root.Relocate(target)` replaces it; it must never call `SaveAs`.

- [ ] **Step 7: Write and pass portable rebase preservation tests**

Add `TestPortableCatalogRebasesDecodedViewWithoutChangingCatalogBytes`, checking producer and consumer homes differ, identities have equal width, relocated file bytes contain the B identity at all recorded offsets, and the source catalog checksum is unchanged.

Run:

```sh
gofmt -w environmentartifact htfs/portable.go htfs/portable_test.go
GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact ./htfs -run 'Test(Inventory|ValidateV12|PortableCatalog)' -count=1
git diff --check
```

Expected: PASS.

- [ ] **Step 8: Commit Task 2**

```sh
git add environmentartifact htfs/portable.go htfs/portable_test.go
git commit -m "feat(artifacts): inventory portable v12 holotree content"
```

---

### Task 3: Strict filesystem CAS and atomic HTTP provider

**Files:**
- Create: `artifactprovider/provider.go`
- Create: `artifactprovider/filesystem.go`
- Create: `artifactprovider/filesystem_unix.go`
- Create: `artifactprovider/filesystem_other.go`
- Create: `artifactprovider/filesystem_test.go`
- Create: `artifactprovider/http.go`
- Create: `artifactprovider/http_test.go`

**Interfaces:**
- Consumes: canonical `environmentartifact.Digest`, `Descriptor`, `Manifest`, and `ObjectIndex`.
- Produces:

```go
type Capabilities struct { SchemaVersions []int; DigestAlgorithms, Encodings []string }
type Blob struct { Descriptor environmentartifact.Descriptor; Reader io.Reader }
type Provider interface {
    Capabilities(ctx context.Context) (Capabilities, error)
    ResolveManifest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error)
    MissingObjects(ctx context.Context, descriptors []environmentartifact.Descriptor) ([]environmentartifact.Digest, error)
    PutObject(ctx context.Context, blob Blob) error
    GetObject(ctx context.Context, descriptor environmentartifact.Descriptor) (io.ReadCloser, error)
    CommitManifest(ctx context.Context, manifest []byte) error
}
func NewFilesystem(root string) (*Filesystem, error)
func NewHandler(provider *Filesystem) http.Handler
func NewHTTP(baseURL string, client *http.Client) (*HTTP, error)
```

- [ ] **Step 1: Write strict CAS path and publication RED tests**

Add tests named:

```go
func TestCASRejectsSymlinkRoot(t *testing.T)
func TestCASRejectsSymlinkParentComponent(t *testing.T)
func TestCASRejectsSymlinkOrNonRegularDestination(t *testing.T)
func TestCASRejectsWrongSizeAndDigestWithoutVisiblePartial(t *testing.T)
func TestCASRejectsExistingConflictingContent(t *testing.T)
func TestCASConcurrentIdenticalPublicationIsIdempotent(t *testing.T)
func TestCASPublicationSurvivesFilesystemProviderRestart(t *testing.T)
```

The parent-component test replaces each fanout/temp/manifests directory in turn with a symlink to an external sentinel directory and asserts neither reads nor writes reach the sentinel.

- [ ] **Step 2: Verify CAS tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./artifactprovider -run 'TestCAS' -count=1`

Expected: FAIL because `Filesystem` does not exist.

- [ ] **Step 3: Implement the strict filesystem primitive**

On Unix, open the provider root and traverse/create fixed provider-owned descendants with directory file descriptors plus `openat`/`mkdirat` and `O_NOFOLLOW|O_DIRECTORY`; create private same-filesystem temporary regular files with `O_CREAT|O_EXCL|O_NOFOLLOW`, stream through `io.LimitReader(size+1)` and SHA-256, fsync, `renameat`, then fsync the destination directory. Re-open existing blobs with `O_NOFOLLOW`, require regular mode, and re-hash before idempotent success. The non-Unix file returns an explicit unsupported-platform error for this v1 runtime rather than silently weakening the invariant.

- [ ] **Step 4: Verify CAS tests are GREEN**

Run the Step 2 command; expected: PASS, including `-race` for concurrent publication:

```sh
GOARCH=amd64 CGO_ENABLED=1 go test -race ./artifactprovider -run 'TestCASConcurrent' -count=1
```

- [ ] **Step 5: Write commit and HTTP RED tests**

Add tests for capabilities, sorted missing results, PUT/GET, wrong URL digest, missing referenced object, corrupt referenced object, manifest invisibility before rename, interrupted request bodies, concurrent identical commits, server restart, unsupported methods/content types, and strict path rejection. A committed manifest must remain unresolvable until all referenced blobs validate and the final manifest rename completes.

- [ ] **Step 6: Verify commit/HTTP tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./artifactprovider -run 'Test(FilesystemCommit|HTTP)' -count=1`

Expected: FAIL because commit and transport behavior are absent.

- [ ] **Step 7: Implement provider commit and HTTP transport**

Use one provider-local commit mutex. Decode canonical manifest/index, expand all descriptors, reopen and reverify every blob from the CAS, then atomically publish manifest bytes under their artifact digest. HTTP routes must accept only exact `/v1/...` shapes, bounded bodies, canonical JSON, and explicit status mappings; the client must verify all response descriptors independently.

- [ ] **Step 8: Verify and refactor Task 3**

```sh
gofmt -w artifactprovider
GOARCH=amd64 CGO_ENABLED=0 go test ./artifactprovider -count=1
GOARCH=amd64 CGO_ENABLED=1 go test -race ./artifactprovider -count=1
git diff --check
```

Expected: PASS.

- [ ] **Step 9: Commit Task 3**

```sh
git add artifactprovider
git commit -m "feat(artifacts): add atomic filesystem HTTP provider"
```

---

### Task 4: Publish and verified acquisition orchestration

**Files:**
- Create: `environmentlifecycle/publish.go`
- Create: `environmentlifecycle/publish_test.go`
- Create: `environmentlifecycle/acquire.go`
- Create: `environmentlifecycle/acquire_test.go`
- Create: `environmentlifecycle/content.go`
- Create: `environmentlifecycle/content_test.go`

**Interfaces:**
- Consumes: Tasks 1-3 plus current `htfs.ComposeFinalBlueprint`, `htfs.RecordEnvironment`, and worker-local `common` paths.
- Produces:

```go
type Builder interface {
    Build(ctx context.Context, robotFile string) (BuildResult, error)
}
type BuildResult struct { LegacyBlueprint []byte; CatalogPath string; Specification environmentartifact.Specification }
type PublishRequest struct { RobotFile string; Provider artifactprovider.Provider; Builder Builder }
type PublishResult struct { ArtifactDigest, SpecificationDigest environmentartifact.Digest; LegacyBlueprintKey string; ObjectCount int; UploadedBytes, ReusedBytes int64 }
func Publish(ctx context.Context, request PublishRequest) (PublishResult, error)

type AcquireRequest struct { ArtifactDigest environmentartifact.Digest; Provider artifactprovider.Provider }
type CacheProvenance string
const (CacheCold CacheProvenance = "provider"; CacheLocalMaterialization CacheProvenance = "local-materialization")
type AcquireResult struct { ArtifactDigest environmentartifact.Digest; MaterializationID, Path string; CacheHit CacheProvenance }
type Acquirer struct { /* home-local dependencies */ }
func NewAcquirer() *Acquirer
func (a *Acquirer) Acquire(ctx context.Context, request AcquireRequest) (AcquireResult, error)
```

- [ ] **Step 1: Write publish flow RED tests**

Use recording builder/provider fakes around real catalog bytes. Assert build occurs once, exact legacy bytes drive lookup, missing blobs only are uploaded, manifest commit is last, commit failure does not mutate the built local environment, raw/mixed mode fails before provider calls, and `compress.no` is byte-for-byte unchanged.

- [ ] **Step 2: Verify publish tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentlifecycle -run 'TestPublish' -count=1`

Expected: FAIL because `Publish` does not exist.

- [ ] **Step 3: Implement minimal publish orchestration**

Build/resolve through the injected current-RCC adapter, inventory exact bytes, create semantic specification and legacy blueprint blobs separately, call `MissingObjects`, upload only returned canonical digests, then call `CommitManifest` exactly once after all uploads succeed.

- [ ] **Step 4: Write verified cold acquisition RED tests**

Use an isolated `ROBOCORP_HOME` initialized before any `common` path access. Test wrong manifest/index/specification/blueprint/catalog/object size or digest, incomplete commit, incompatible platform/requirements/Hololib mode, conflicting local content, traversal metadata, and interruption before ready state. Assert no validation failure invokes the builder or falls back to package-network work.

- [ ] **Step 5: Verify acquisition tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentlifecycle -run 'TestAcquire' -count=1`

Expected: FAIL because verified acquisition does not exist.

- [ ] **Step 6: Implement verified local content installation**

Store canonical manifests/index/specification/blueprint in an artifact-specific cache below the active `ROBOCORP_HOME`. Fetch into a same-directory private temporary file, bound size, hash exact bytes, fsync, atomically rename, and fsync parent. Derive legacy catalog/object paths only after ID validation; reject existing mismatches rather than overwriting. Verify gzip logical IDs before installing legacy objects.

- [ ] **Step 7: Verify and refactor Task 4**

```sh
gofmt -w environmentlifecycle
GOARCH=amd64 CGO_ENABLED=0 go test ./environmentlifecycle -run 'Test(Publish|Acquire)' -count=1
git diff --check
```

Expected: PASS.

- [ ] **Step 8: Commit Task 4**

```sh
git add environmentlifecycle
git commit -m "feat(artifacts): publish and acquire verified environments"
```

---

### Task 5: Portable materialization, records, typed leases, and execution

**Files:**
- Create: `environmentlifecycle/materialize.go`
- Create: `environmentlifecycle/materialize_test.go`
- Create: `environmentlifecycle/record.go`
- Create: `environmentlifecycle/record_test.go`
- Create: `environmentlifecycle/lease.go`
- Create: `environmentlifecycle/lease_test.go`
- Create: `environmentlifecycle/execute.go`
- Create: `environmentlifecycle/execute_test.go`
- Modify: `environmentlifecycle/acquire.go`
- Modify: `environmentlifecycle/acquire_test.go`

**Interfaces:**
- Consumes: verified local content from Task 4 and `htfs.PortableCatalog` from Task 2.
- Produces:

```go
type Materializer interface {
    Materialize(ctx context.Context, manifest environmentartifact.Manifest) (Materialization, error)
    Lease(ctx context.Context, materialization Materialization) (Lease, error)
    ExecutionHandle(ctx context.Context, lease Lease, command []string) (ExecutionHandle, error)
    Release(ctx context.Context, lease Lease) error
}
type Materialization struct { ArtifactDigest environmentartifact.Digest; ID, Path string; CacheHit CacheProvenance }
type Lease struct { ID, MaterializationID string; ArtifactDigest environmentartifact.Digest; OwnerPID int; OwnerStart string; CreatedAt time.Time }
type ExecutionHandle struct { ArtifactDigest environmentartifact.Digest; MaterializationID, LeaseID, CWD, Executable string; Environment []string; CacheHit CacheProvenance }
type ChildResult struct { ExitCode int }
func Execute(ctx context.Context, materializer Materializer, materialization Materialization, command []string) (ExecutionHandle, ChildResult, error)
```

- [ ] **Step 1: Write state-transition and portable materialization RED tests**

Assert only `verified-content -> materializing -> ready` is accepted, failures/interruption never publish ready, catalog/object bytes remain unchanged, A/B home bases differ, fixed-offset producer identity bytes become the equal-width B target identity, legitimate symlinks remain inside the root, and current `MakeBranches`, `RestoreDirectory`, and `DropFile` paths are exercised.

- [ ] **Step 2: Verify materialization tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentlifecycle -run 'Test(Materialization|Portable)' -count=1`

Expected: FAIL because state records and materializer do not exist.

- [ ] **Step 3: Implement atomic records and portable materializer**

Write transition records through same-directory temp/fsync/rename/parent-fsync. Decode immutable catalog, rebase only its in-memory base to `common.HolotreeLocation()`, require producer and target identity byte widths match, call existing v12 restoration functions, verify materialization metadata/Python, then atomically publish ready.

- [ ] **Step 4: Write typed lease/execution RED tests**

Test explicit `Lease`, `ExecutionHandle`, idempotent `Release`, owner PID/start identity persistence, no lease in `AcquireResult`, child spawn failure cleanup, non-zero exit propagation, context/signal cleanup, and fresh execution handles on repeated runs. Verify the environment comes from `conda.CondaExecutionEnvironment(materialization.Path, nil, true)`.

- [ ] **Step 5: Verify lease/execution tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentlifecycle -run 'Test(Lease|Execution|Execute)' -count=1`

Expected: FAIL because the lifecycle is absent.

- [ ] **Step 6: Implement lease and process-scoped execution**

Create leases with exclusive files below the artifact-local state directory. Build a fresh handle immediately before child spawn, start the child with the derived environment and working directory, forward SIGINT/SIGTERM through a testable signal dependency, wait, and always release idempotently. Return the exact child exit code separately from infrastructure errors.

- [ ] **Step 7: Write the fail-on-touch warm-path RED test**

Add `TestWarmAcquireDoesNotTouchProviderOrBuilder`. First acquire must record provider resolve/get calls; after counters reset, replace provider and builder with implementations whose every method calls `t.Fatalf`. The second acquire must return the same artifact and valid materialization with `CacheHit == CacheLocalMaterialization`.

- [ ] **Step 8: Verify warm test is RED, then implement local-first reuse**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentlifecycle -run TestWarmAcquireDoesNotTouchProviderOrBuilder -count=1`

Expected RED: provider/builder fail-on-touch fires or warm behavior is absent.

Implement a pre-provider check that verifies canonical cached manifest, ready record, matching legacy key/target metadata, materialization root directory, and regular Python executable. Missing/stale derived state rematerializes only from verified local legacy content; it must not call the builder.

- [ ] **Step 9: Verify and refactor Task 5**

```sh
gofmt -w environmentlifecycle
GOARCH=amd64 CGO_ENABLED=0 go test ./environmentlifecycle -count=1
git diff --check
```

Expected: PASS.

- [ ] **Step 10: Commit Task 5**

```sh
git add environmentlifecycle htfs/portable.go htfs/portable_test.go
git commit -m "feat(artifacts): materialize and lease acquired environments"
```

---

### Task 6: Thin CLI and real isolated A/B vertical

**Files:**
- Create: `cmd/environment.go`
- Create: `cmd/environmentPublish.go`
- Create: `cmd/environmentAcquire.go`
- Create: `cmd/environmentExec.go`
- Create: `cmd/environment_test.go`
- Create: `cmd/cache.go`
- Create: `cmd/cacheServe.go`
- Create: `cmd/cacheServe_test.go`
- Create: `robot_tests/environment_artifacts.robot`
- Create: `robot_tests/environment_artifacts/robot.yaml`
- Create: `robot_tests/environment_artifacts/conda.yaml`
- Create: `robot_tests/environment_artifacts/task.py`

**Interfaces:**
- Consumes: typed lifecycle and provider APIs from Tasks 3-5.
- Produces exactly:

```text
rcc env publish --robot <robot.yaml> --provider <url> --json
rcc env acquire --artifact <sha256:...> [--provider <url>] --json
rcc env exec --artifact <sha256:...> [--provider <url>] --json -- <command> [args...]
rcc cache serve --root <directory> --listen 127.0.0.1:0 --json
```

- [ ] **Step 1: Write Cobra contract RED tests**

Assert exact command/flag registration, canonical digest rejection before lifecycle calls, provider optional only for local-ready acquire/exec, JSON field stability, `env acquire` output contains no lease, command arguments after `--` remain byte-for-byte ordered, and `cache serve` rejects non-loopback default expansion unless explicitly supported later.

- [ ] **Step 2: Verify CLI tests are RED**

Run: `GOARCH=amd64 CGO_ENABLED=0 go test ./cmd -run 'Test(Environment|CacheServe)' -count=1`

Expected: FAIL because the commands do not exist.

- [ ] **Step 3: Implement translation-only commands**

Register `env` and `cache` groups, parse flags into typed requests, construct the HTTP provider only when needed, encode one JSON object per result, keep diagnostics on stderr, return the child exit code after deferred lease cleanup, and shut the HTTP server down on SIGINT/SIGTERM.

- [ ] **Step 4: Verify CLI tests and build the candidate**

```sh
gofmt -w cmd
GOARCH=amd64 CGO_ENABLED=0 go test ./cmd/... ./environmentartifact ./artifactprovider ./environmentlifecycle ./htfs -count=1
rcc run -r developer/toolkit.yaml --dev -t local
git diff --check
```

Expected: PASS and `build/rcc` exists.

- [ ] **Step 5: Write bounded Robot A/B acceptance before running it**

The fixture uses a representative pure-Python package already available through the current solver, two `mkdtemp`-backed homes, and a provider root. It starts `build/rcc cache serve`, publishes from process A, then runs process B with a separately initialized empty `ROBOCORP_HOME`, hostile `HTTP_PROXY`/`HTTPS_PROXY`, and `NO_PROXY` limited to the loopback provider. Assert B imports the package, A/B homes differ, artifact digests match, provider/build request counters are nonzero only on the first acquire, the second acquire succeeds after provider shutdown, `cacheHit` is `local-materialization`, and no B package-manager download/cache evidence exists.

- [ ] **Step 6: Run bounded acceptance and fix only reproduced failures with RED tests**

Run the candidate build and the exact suite inside the contained toolkit environment:

```sh
rcc run -r developer/toolkit.yaml --dev -t local
rcc task script -r developer/toolkit.yaml --space environment-artifacts-v1 -- \
  python -m robot -L DEBUG -d tmp/output-environment-artifacts \
  robot_tests/environment_artifacts.robot
```

Expected: PASS on Linux amd64.

- [ ] **Step 7: Commit Task 6**

```sh
git add cmd robot_tests/environment_artifacts.robot robot_tests/environment_artifacts
git commit -m "feat(artifacts): expose the A-to-B environment lifecycle"
```

---

### Task 7: Architectural checkpoint and promotion gates

**Files:**
- Modify only files required by validated review blockers or failing promotion tests.

**Interfaces:**
- Consumes: the complete candidate vertical.
- Produces: immutable checkpoint SHA, review receipt, full contained verification, and documentation receipt.

- [ ] **Step 1: Record the candidate and focused invariant evidence**

```sh
git status --short
git rev-parse HEAD
GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact ./artifactprovider ./environmentlifecycle ./htfs ./cmd -count=1
```

Expected: clean tree and PASS. Report the exact SHA and the A→B invariants it proves.

- [ ] **Step 2: Request independent Hermes/Terra checkpoint review**

Give Terra the candidate SHA, approved spec, this plan, focused test output, and diff. Ask only for correctness/security blockers against the locked contract. Validate each claimed blocker directly; write a failing regression test before integrating a valid blocker. Do not integrate preferences or reopen deferred scope.

- [ ] **Step 3: Run contained unit/build promotion gates**

```sh
rcc run -r developer/toolkit.yaml --dev -t unitTests
rcc run -r developer/toolkit.yaml --dev -t local
git diff --check
```

Expected: PASS.

- [ ] **Step 4: Run bounded then full Robot promotion gates**

Run the bounded environment-artifact suite first, then:

```sh
rcc run -r developer/toolkit.yaml -t robot
```

Expected: PASS. Preserve exact logs for any platform/environment limitation.

- [ ] **Step 5: Final commit/push and documentation receipt**

If review or promotion produced changes, commit them with a focused Conventional Commit. Push using the already validated SSH push URL, read back the remote SHA, and report source/build/runtime/publish status separately. Close with the mandatory documentation receipt; load the meta-skill only if this work proved reusable guidance stale or incomplete.
