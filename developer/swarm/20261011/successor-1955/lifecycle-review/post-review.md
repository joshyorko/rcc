# Independent overlay and receipt review

**Disposition: conditional scoped GO for the next owned transport/materialization comparison.** This does not approve production integration or claim general private-home semantics. Use only after exact preflight of all owned fixture inputs/roots as `0700`, enclosing guards at `0700`, and strict post-run guard verification. Keep the explicit unchanged shared-mode controls. No execution or source mutation was performed in this review.

## Reviewed identity and result

Frozen RCC source is `7945f956b428359928908a9f42e36417b113dafd`. Original `/workspace/rcc-frozen/pathlib/functions.go` SHA-256: `ba1e7697dcd2b1a239292def345391d0d5fcaf1d49f326dcc988728807f04dcc`. The overlay copy SHA-256 is `bdcebbd7598a7669bdb7c93cac220a708413d704f9c5a95b069191076624c270`; patch SHA-256 is `cab5b473851f33e28532321cdb18216f12d2d9d9660e067c1e824f706658f81d`. The `pathlib` prototype change adds only a private `0750` missing-leaf parent exception when the parent path equals `common.Product.Home()`: it `Lstat`s, requires a real directory of exact mode `0700`, checks current UID/GID, and skips the recursive chmod. Missing-home behavior recurses as before. Existing leaf behavior and `0777` shared behavior are untouched. The separate overlay entry redirects the fixed global HTFS path to the admitted owned `0750` scratch directory.

The baseline import probe built and ran with exit 1. Its stdout records `guard_pass:false`, baseline home `/workspace/rcc-successor-lifecycle/baseline/rcc-home`, observed mode `0750`. The retained `strace` shows `fchmodat(...baseline/rcc-home, 0750)` immediately before hololib creation. Prototype controls built and ran with exit 0; all 12 named controls pass. Its trace shows hololib descendants created/chmodded to `0750` and no chmod of `/workspace/rcc-successor-lifecycle/prototype/rcc-home`.

Current retained filesystem inspection reports outer `/workspace/rcc-successor-lifecycle`, baseline and prototype enclosing directories, prototype RCC home, and prototype controls root as `0700`; the diverted `global-ht` is `0750`; baseline RCC home is `0750`. All observed owners are UID/GID `1000:1000`. Baseline input mode `0700` is evidenced by the preparation script creating it so; trace records the subsequent mode change. Prototype control explicitly checks its RCC home and enclosing boundary remain `0700`.

## Scope limits and remaining gaps

- The foreign-owner negative is not an isolated UID/GID test: `/opt/codex` is both foreign-owned and mode `0755`, while the implementation checks exact `0700` mode before owner. Record it as a combined rejection only.
- An existing configured home at `0750` is rejected by this prototype, although baseline `hasCorrectMode(0750)` accepts it. The next fixture must require exact preflight `0700`; do not infer compatibility for other existing home modes.
- Existing explicit leaves remain on the original `ensureCorrectMode` path. The control confirms that calling `MakeSharedDir(home)` on an existing `0700` leaf changes it to `0750`; this fix does not cover that case.
- `Lstat` protects only the configured-home leaf. This is not a no-follow traversal, race-free path confinement, or general ancestor-symlink guarantee.
- The prototype imports `syscall.Stat_t` in the overlaid shared source file. It is qualified only for the Linux/amd64 overlay; no cross-platform build claim follows.

## Required gate for the next comparison

Before the new owned fixture starts, verify every input directory it owns—including RCC home and each enclosing boundary—is exactly `0700`, has expected owner/group, and contains only the listed fixtures. Check that the separate redirected global path remains its separately admitted `0750`. Run the baseline/prototype pair only inside those owned paths, then inspect and record actual home, guard, descendant, and global-path modes after each arm. Stop on any mismatch; do not chmod repairs. Preserve the baseline home-mode failure and the foreign-owner test limitation as separate facts.

Evidence hashes: baseline trace `c727ff7c16d73b71caec9ba67f84ec85a6fb8f0f05a70034e23e66e247145944`; prototype trace `d026dbf8b448c32972494a315201133f97b2f910fc2e34e3ed7b7b4a94d2daf1`; baseline result `8a82bbe294ac1b6d766de7f8b238c075016726e8a15cef3da17af219f867d484`; prototype-control result `5ed07a69caf24ab76ddb511d1073a5638d1d579e8bd813b5b3498cd269ed8075`.
