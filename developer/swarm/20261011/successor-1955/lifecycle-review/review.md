# Private-home mode recursion: source review

**Decision:** conditional GO for a Go-overlay prototype only after narrowing the exemption to the exact configured `common.Product.Home()` parent. **NO-GO for the proposal as broadly phrased** if it preserves every existing parent mode under private holotree mode. This is source review only; no prototype, build, test, or runtime was run.

Reviewed frozen source at RCC `7945f956b428359928908a9f42e36417b113dafd`. Exact hashes for the reviewed files are in `source-sha256.txt`.

## Source path

At `pathlib/functions.go:236-256`, a missing directory is created by first calling `shared.MakeSharedDir(filepath.Dir(fullpath))`, then `os.Mkdir(fullpath, correct)`. In private holotree mode, `pathlib/variables.go` selects `privateSetup`; its `MakeSharedDir` calls `makeModedDir(..., 0750)` (`pathlib/shared.go:23-24`). When that recursion reaches an existing `Product.Home()` directory with mode `0700`, `hasCorrectMode` fails because `0700 & 0750 != 0750`, so `ensureCorrectMode` chmods the home to `0750`.

`htfs/directory.go:35-40` initializes the holotree and hololib paths at import. For private holotree mode, `common.HololibCatalogLocation`, `HololibLibraryLocation`, `HololibUsageLocation`, and `HololibPids` resolve below `common.Product.Home()` (`common/variables.go:244-270`). Their recursive creation reaches the configured home as an existing parent. This supports the proposed parent-recursion fix for the observed path. Common's earlier `ensureDirectory` uses `os.MkdirAll`; it does not chmod an existing home, so the reviewed chmod path is the private `MakeSharedDir` recursion.

## Minimal prototype boundary

In the missing-leaf branch of `makeModedDir`, before calling `shared.MakeSharedDir(parent)`, bypass that recursive chmod only when all are true:

- `correct == 0750` and `common.SharedHolotree == false`;
- the absolute, cleaned parent path equals the absolute, cleaned `common.Product.Home()` path;
- `os.Lstat(parent)` reports an existing real directory, not a symlink; and
- its exact permissions are `0700` or `0750`.

For a missing home parent, continue through the existing recursion unchanged. Propagate other `Lstat` errors and fail closed for a symlink, non-directory, or unexpected home mode. Leave the existing-directory/explicit-leaf branch unchanged. Keep `0777` shared setup unchanged. This preserves the observed `0700` home while allowing the requested child directory to be created with `0750`.

A generic exception for all existing parents is not safe enough: `makeModedDir`/`privateSetup` is used beyond these four hololib paths, including other shared-directory helpers. Preserving any parent could silently keep an unexpected restrictive mode and prevent traversal, or preserve an unsafe ancestor as though it had passed the intended mode policy. Comparing with `Product.Home()` also respects RCC's `ROBOCORP_HOME` override; comparing with OS `HOME` would target the wrong boundary. `Lstat` on the exact parent rejects a leaf symlink, but path-based creation still has a check/use race and does not validate symlink components above that parent. The prototype should claim only the bounded configured-home case, not general race-free path confinement.

## Required integrity negatives before accepting the prototype

- Private mode: existing configured home `0700` remains exactly `0700`; newly created hololib descendants are `0750`; an existing home `0750` stays unchanged.
- Missing configured home: existing recursive creation behavior remains intact and creates the home/descendants with the existing private mode policy.
- Exact boundary: an existing non-home parent at `0700` still follows the existing `0750` behavior; this proves the exemption is not global.
- Fail closed: a configured-home symlink and a non-directory home parent produce errors without creating descendants or changing the symlink target; unexpected home permissions also reject without chmod.
- Ownership: confirm the configured home has the expected RCC user/group before preserving it; `Lstat` proves the leaf type and mode, not ownership. A foreign-owned home must fail without creating descendants.
- Scope limits: an explicit requested leaf already present at `0700` still takes the existing leaf path and is not claimed fixed. Test/report this limitation. If that case must be supported, it needs a separate explicit decision and guard.
- Shared mode: `0777` behavior remains unchanged.
- For each baseline/prototype comparison, snapshot the separately owned root guard and assert it is exactly `0700` before and after. Also assert configured-home mode and descendant modes; do not infer them from RCC exit alone.

The current proposed `Lstat`/symlink/non-directory checks do not by themselves establish ownership or eliminate a path replacement race. The prototype must check expected ownership separately and should not claim race-free path confinement; root/fixture ownership guards remain independent and must be observed unchanged. Explicit-leaf mode correction and other home paths remain outside this narrow fix.

Documentation receipt
- Canonical guidance: no change.
- Durable learning: private hololib import reaches `Product.Home()` through `MakeSharedDir` recursion; `common.ensureDirectory` is not the chmod site.
- Evidence: frozen source at `7945f956b428359928908a9f42e36417b113dafd`; exact hashes in `source-sha256.txt`.
- Stale guidance removed: none.
- Remaining uncertainty: no overlay execution was performed; configured-home race and explicit-leaf behavior remain unqualified.
