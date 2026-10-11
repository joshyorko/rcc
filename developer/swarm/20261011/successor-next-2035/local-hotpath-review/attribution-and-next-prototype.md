# Local CAS / Acquire hot-path review

Read-only review, no build or workload rerun. The six retained profiles were collected from the private filesystem-provider Acquire comparison. Go pprof report-only output was generated from the retained `.pprof` files; all six report the same binary build ID `3b6fd52876c355328c660d00ea8687963e17f601` and executable SHA-256 `89463c7df9b75ea717dcad45ca6041822b3f797e7154b86c08562f56b80dc7a2`.

Source is bound to frozen RCC checkpoint `7945f956b428359928908a9f42e36417b113dafd`. The inspected `/workspace/rcc` checkout is clean at `7d17122436584dcbfe9b8afddeb56d7c81a10af7`; `git diff <checkpoint> -- <eight inspected source files>` is empty. File SHA-256 values, the exact compression harness/controller/overlay/build binding, and retained profile hashes are in `profile-attribution.json`.

## What the profiles say

The default CPU tops show Linux `internal/runtime/syscall/linux.Syscall6` at 65.31–73.81% flat sampled CPU in all six runs. Cumulative pprof puts `acquireVerifiedContentLockedWithLocal` at 61.70–79.07% and the enclosing real `Acquirer.Acquire` at 70.21–88.37%. Small samples occasionally show `artifactprovider.openObjectDirectory`/`readVerifiedAt`, JSON, and gzip routines; profiles do not resolve the generic syscall samples to exact syscall types. This identifies Acquire’s filesystem path as the hot area, but does not prove a particular syscall or support a performance-winner claim.

## Concrete duplicate work in the cold import path

In `environmentlifecycle/acquire.go`, `cacheProviderObject` is called once per descriptor at lines 88–92 and 108–111. `Filesystem.MissingObjects` then iterates descriptors and `hasObject` reopens the root and digest directory chain for each one (`artifactprovider/filesystem.go:288`, `filesystem_unix.go:117–135`). Filesystem `GetObject` repeats that root/digest walk (`filesystem_unix.go:138–160`). `openObjectDirectory` traverses `objects/sha256/<2>/<2>` with `openat` for every descriptor (`filesystem_unix.go:294–318`); the acquired bytes are then opened, `fstat`ed, bounded-read, and SHA-256 checked (`readVerifiedAt`, lines 370–395).

There are also separate whole-object closure passes: filesystem `ResolveManifest` calls `verifyManifestClosure` before returning the manifest, and local `CommitManifest` calls it again before publishing (`filesystem_unix.go:163–210`; `ResolveManifest` later in the file). Each closure pass rereads the object index and all referenced objects. Import then verifies local objects logically by reading and decompressing them at `acquire.go:138–145`; after commit it reads those objects again for installation at `151–159`. `readProviderObject` checks their stored digest, and `installLegacyImmutable` performs another descriptor hash over the in-memory bytes (`content_unix.go:23–28`). These repeated checks cross remote/local storage and publication boundaries; do not remove closure, no-follow, regular-file, size, or digest checks without a verified snapshot/generation proof.

## Narrow next prototype

The most direct source seam for the profile’s syscall-heavy evidence is a **per-Acquire filesystem session** in `artifactprovider` that pins a validated root FD and the `objects/sha256` directory FD for the duration of a cold acquisition, and reuses them for `MissingObjects`, `GetObject`, `PutObject`, and `CommitManifest`. Keep each leaf `openat(... O_NOFOLLOW)`, `fstat` regular-file/size, bounded read, descriptor hash, and manifest closure check. Cache only a bounded set of opened two-byte shard FDs (or begin with root plus `objects/sha256` only); close all session descriptors on every exit. Add checks that the current root path still resolves to the pinned inode at session end and fail closed on replacement. Preserve the current provider API via an optional internal/session interface and generic-provider fallback.

This targets repeated absolute root/path traversal without weakening per-object integrity. Before considering it, test symlinked root/intermediate/leaf rejection, root or shard rename/replacement during a session, descriptor cleanup on cancellation/error, identical rejection of malformed/mutated content, and real cold Acquire/Execute against identical fixtures with the strict 0700 root guards. Measure path/syscall counts separately from CPU, and keep the full pipeline as the outcome.

A secondary low-risk micro-seam is to avoid the final in-memory duplicate descriptor hash by using a private validated-content type between `readProviderObject` and `installLegacyImmutable`; retain the verifying function for untrusted/direct callers. CPU profiles do not show hashing as a dominant flat cost, so prioritize FD/path traversal first.

Compatibility work is not the obvious duplicate: host OS/CPU/filesystem preflight is distinct from executing the artifact’s Python to validate its declared ABI after materialization (`acquire.go:71`, `materialize.go:144–148`, `compatibility.go:59–76`). Keep both unless a tested capability receipt can prove which fields are invariant and fresh enough.

## Limitations

This is diagnosis only. The current profiles cover a tiny synthetic workload, a private diagnostic binary with overlays, warm/uncontrolled OS cache, and filesystem-provider workers. The pprof result gives no per-syscall or statistically useful timing attribution. The prototype is a design proposal only; no source or frozen checkpoint was changed and no confidence winner is claimed.
