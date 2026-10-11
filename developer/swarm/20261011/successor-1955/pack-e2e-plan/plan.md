# RCC #125 private packed-provider end-to-end plan

Status: source review and proposal only. No Go source, test, benchmark, package listing/build, provider construction, fixture generation, RCC home/cache write, or network operation was performed for this lane. Do not execute until the coordinator explicitly admits the implementation/run.

## Question and smallest useful scope

Can a test-only packed provider preserve the current provider contract and produce the same cold acquire, materialize, lease, and executable result as `artifactprovider.Filesystem` for one newly published, tiny synthetic artifact with one executable plus 512 unique small payload files?

Use the same standard v1 manifest and object index for both arms. Publish once to a new `artifactprovider.Filesystem`; compare (A) that provider and (B) an in-test packed adapter wrapping the same Filesystem for metadata while routing only stored gzip object descriptors through a private pack. Use separate, newly created consumer homes per arm, run sequentially, and require strict root mode `0700` at the coordinator's existing pre/post boundary checks. Never chmod an unexpected home mode or relax the guard. No production provider, artifact format, manifest, default, cache, Actions, or old 4,649-object corpus changes.

## Source/API findings

- `artifactprovider.Provider` in `artifactprovider/provider.go` is the complete seam: `Capabilities`, `ResolveManifest`, `MissingObjects`, `PutObject`, `GetObject`, and `CommitManifest`. `AcquireRequest.Provider` accepts it. No new production injection API is needed.
- The baseline `Filesystem.GetObject` in `artifactprovider/filesystem_unix.go` calls `readVerifiedAt`: `openat` with `O_NOFOLLOW`, regular-file and exact-size checks, bounded read, and stored SHA-256 verification. Directory traversal also opens every component with `O_DIRECTORY|O_NOFOLLOW`. `verifyObjectAt` uses the same safe final-file and byte-integrity checks for commit closure validation.
- `NewAcquirer().Acquire(ctx, AcquireRequest{ArtifactDigest:d, Provider:p})` is public and performs real capability negotiation, manifest resolution, compatibility preflight, standard local CAS caching, object-index decode, gzip/logical object verification, legacy install, and materialization. Its local cache remains the existing Filesystem; only the remote input provider varies.
- `Publish(ctx, PublishRequest{Provider: filesystem, Builder: fixtureBuilder})` accepts the public `Builder` interface and a prepared `BuildResult`. This is suitable to publish an immutable synthetic catalog/closure without invoking a solver. Label it fixture assembly, not an RCC build or publish-speed measurement.
- Existing `environmentlifecycle/publish_test.go:newPublishFixture` is a construction reference, but it assigns a no-op `verifyMaterializedCompatibility` hook; the successor must not call that helper or any compatibility override. Build a dedicated fixture and use `compatibilityForMaterialization` / normal `currentWorkerCapabilities` and `validateMaterializedCompatibility` against the actual wrapper. The repository's existing real vertical test shows the actual `NewAcquirer` → `Materializer.Lease`/`Execute` and receipt pattern.
- `ExecuteWithStreams(ctx, NewLocalMaterializer(), materialization, []string{"python", "-c", probe}, nil, &stdout, &stderr)` is public. Construct `Materialization` from the matching `AcquireResult` fields; execution creates and releases a lease around the child. Verify `ExitCode==0`, exact stdout, empty unexpected stderr, and no live lease afterward.
- Historical `go-object-pack-native-preexecution-1600/harness.go` measured a component comparison over the old 4,649-object corpus and did not enter acquisition/materialization. Reuse only its principles (private adapter, digest-bound offsets, negative controls, immutable source closure). Do not import its corpus or repeat its timed wave. Its proposal/report is historical evidence only.

## Fixture and paired execution

Add a new test-only file in `environmentlifecycle` (same package, so it can use fixture helpers/types without widening production APIs). Its fixture constructor should:

1. Create an owned producer home and `hololib` catalog root; create one `bin/python` regular file mode `0755` whose small shell wrapper `exec`s the exact already installed host Python executable (resolve the absolute executable before run; do not install anything). The wrapper is only an execution-path probe. It does not prove a self-contained Python environment, a supported distribution, or Actions behavior. The child Python workload must read all 512 payload files, verify each path/size/SHA-256 against the fixture receipt, and emit a canonical count plus ordered aggregate digest; capture this output and a before/after source-hash/count receipt.
2. Add 512 uniquely named small payload files with distinct, non-relocatable byte patterns and no symlinks. Store each under its legacy SHA-256 path as gzip bytes, create corresponding `htfs.File`/directory entries with exact logical digest, size, mode, and empty rewrite offsets, then save the v12 portable catalog. Keep every path and payload inside the owned producer fixture.
3. Derive compatibility requirements from the actual materialized wrapper and current platform with `compatibilityForMaterialization`; do not hard-code guessed Python ABI values and do not replace compatibility functions. Use an immutable `BuildResult` returned by a tiny fixture `Builder`; call normal `Publish` into a new Filesystem. Assert the resulting index has 513 distinct objects (wrapper plus 512 payloads), all object stored sizes fit the deliberately tiny fixture bounds, and the manifest/index identities are retained.
4. Build a private packed sidecar from the published stored-object bytes, sorted by stored digest. Canonical index fields: schema/format ID, artifact digest, standard object-index descriptor digest, pack byte length and SHA-256, and per-object stored digest, size, offset. Require strictly increasing digests, unique entries, checked `offset <= packSize` then `size <= packSize-offset`, no overlaps, exact coverage, and no integer overflow. Pin the canonical index SHA-256 and pack SHA-256 in the test's own expected fixture receipt; at provider initialization, compare both pins, manifest artifact identity, object-index identity, pack identity and exact sizes. Sidecar bytes live outside the RCC manifest and do not alter any published descriptor.
5. Implement an unexported test-only `packedProvider` that satisfies `artifactprovider.Provider`. Embed/delegate non-object methods and all metadata object reads to the real Filesystem. For gzip-object `GetObject`, require exact media type/digest/size match to the validated index before allocating, open the pack root once by walking every absolute path component with directory-relative `O_NOFOLLOW|O_DIRECTORY` (matching RCC `openProviderRootPath`), open the pack with `openat(O_NOFOLLOW)`, and keep the owned root/file descriptors. At initialization `fstat` a regular file of exact expected length and hash the entire pack, matching its pinned SHA-256. Before each read, re-`fstat` and require regular type/exact length; check `offset <= fileSize` then `size <= fileSize-offset` before allocation; `pread` the exact frame and verify its raw stored SHA-256. Return a fresh owned byte slice/reader for each call. A whole-pack hash at initialization plus per-frame hash on every read avoids an unchecked fast path; retain secure root/stat/allocation semantics. Reject symlink roots/files, wrong descriptors, truncated or missing frames, and corrupted content. The synthetic-only code is a test fixture, not a general provider or production-format proposal.
6. Snapshot/hashes of producer object payloads, catalog, standard index, manifest bytes/digest, private pack and pack index before both arms; verify unchanged after. For each arm use a distinct new consumer home set before acquisition, observe its strict `0700` root pre/post without repairing it, assert the exact same artifact digest, acquire cold with provider provenance, verify every expected logical payload in materialized location, run the wrapper through `ExecuteWithStreams`, then verify lease release. Run the arms sequentially because `common.Product.Home` is process-global. Do not allow a previous artifact/materialization or local content cache to make either arm warm.

Sketch of the shared public flow (fixture construction and `packedProvider` are private test helpers):

```go
result, err := NewAcquirer().Acquire(ctx, AcquireRequest{
    ArtifactDigest: artifactDigest,
    Provider:       armProvider, // real Filesystem or private packedProvider
})
requireColdProviderResult(result, artifactDigest)
mat := Materialization{
    ArtifactDigest: result.ArtifactDigest, ID: result.MaterializationID,
    Path: result.Path, CacheHit: result.CacheHit, Verification: result.Verification,
    TrustPolicy: result.TrustPolicy, TrustRequest: result.TrustRequest, TrustCarrier: result.TrustCarrier,
}
handle, child, err := ExecuteWithStreams(ctx, NewLocalMaterializer(), mat,
    []string{"python", "-c", "print('pack-e2e-ok')"}, nil, &stdout, &stderr)
```

Assert the baseline and candidate both print the same fixed token, return exit 0, have equal artifact/materialization identities where deterministic, and leave no active lease. Compare object/output bytes and acquisition traces. Stage timings may be recorded for diagnosis only; one paired fixture run cannot establish a speedup or winner.

## Untimed negative and isolation controls

Exercise candidate provider controls before the paired lifecycle calls, in separate owned copies/roots so a negative cannot contaminate a later test:

- Flip one byte within a stored frame: `GetObject` must fail the stored digest check; the regular Filesystem arm's equivalent tamper control should fail its existing check.
- Truncate the pack, remove the pack, or make the expected frame missing: exact file-size/range or missing-object failure; no out-of-bounds allocation or successful empty read.
- Point the packed root or pack file at a symlink: initialization/read must reject it. Verify root and target identities remain unchanged.
- Supply a known digest with a wrong size, unknown/wrong digest, wrong media type, out-of-range offset, overflowing offset/size, overlapping/duplicate index entry, wrong pack hash/index hash, wrong artifact identity, or wrong standard object-index digest: reject before frame allocation/read.
- Request the same frame twice/concurrently: each result buffer has distinct backing storage and identical bytes; mutating the private result from one test helper call must not change the other or the pack.
- For each end-to-end corruption case, use a fresh consumer home and assert no committed local manifest, ready materialization, or executable output is produced. A partially staged local object cache is allowed only if existing importer semantics allow it; source/provider and fixture inputs must remain unchanged.

Use RCC's existing importer as the semantic oracle: `acquireVerifiedContentLockedWithLocal` decodes the unchanged standard ObjectIndex and invokes `verifyLogicalObject` on every stored gzip frame before `installLegacyImmutable`, so the candidate must return the same standard stored bytes. The test must verify materialized payload bytes after acquisition; it cannot treat provider SHA checks alone as logical-object proof.

## Admission gates and blockers

- Coordinator approval is still required before writing/running the Go fixture. No compile or toolchain/cache preparation is requested in this lane.
- Run only inside the coordinator's newly admitted isolated private-home overlay and root controls, with the exact mode-`0700` check active before and after the lifecycle. The baseline home-mode failure remains preserved; do not use it as permission to waive, chmod, or recast the guard. Stop if the admitted overlay/control fails.
- Compatibility is host-bound: the wrapper invokes the installed host Python. This can prove current host ABI probing and execution lifecycle only. It does not prove a vendored Python, portability, native extension, installer behavior, Actions contract, or cross-platform support.
- The execution supervisor may require capabilities not available in the selected environment. If `ExecuteWithStreams` cannot run under the admitted root without weakening supervision, record the blocker and retain the source/API result; do not replace the real execute path with direct `exec.Command` and call it equivalent.
- The current provider package's object/directory bounds and safe-path semantics must be checked against the exact frozen source at implementation time. Do not introduce a generic persistent pack format or update manifest schemas/defaults in this experiment.

## Recommendation boundary

If admitted, implement one package-private test fixture and one tiny test-only provider, run the integrity negatives plus one cold vertical execution per arm, and preserve exact source/manifest/index/pack identities and results. This establishes behavioral parity for the fixture only. It does not establish a runtime speedup, Actions acceptance, a persistent object-pack design, or a #125 winner/closure.
