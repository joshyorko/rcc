# Actions compatibility admission refresh

Read-only refresh completed 2026-10-11 19:51 UTC against GitHub `joshyorko/actions`; baseline was checkpoint `2f6570c99d5ba98f26ceb7b74d6da69fb84f99e8`. No Actions or RCC issue/PR was modified, no runtime or workflow was launched, and no build/source edit was made. Raw API responses, current source files, comments, and SHA-256 inventory are retained in this directory.

## Current public state

| Record | State / last update | Exact revision and checks |
|---|---|---|
| Issue #101 | Open; 174 comments; updated 19:25:51Z | Body now reports PR #308 head `8d49d0865ad5ad27b2d4e325ec135212080625e8`, configured SOURCE FULL `1470 PASS / 13 SKIP`, and independent acceptance reference `073e235a`. It also preserves an original parent-post failure and a separate read-only supplement. No newer RCC binary has executed or qualified; production stays at 18.19.3. |
| Issue #133 | Open; 7 comments; updated 17:46:58Z | Latest comment remains RCC coordination. It says source `396925…` was then not publicly inspectable and asks the Actions owners to provide exact source/controller/fixture and current-control evidence. That source SHA is now publicly inspectable and is an ancestor of PR #308 head. |
| Issue #134 | Open; 5 comments; last updated 01:44:42Z | No newer comment or status change observed. |
| Issue #139 | Open; 0 comments; last updated 2026-09-07 | No newer comment or status change observed. |
| Issue #143 | Open; 0 comments; last updated 2026-09-07 | No newer comment or status change observed. |
| PR #221 | Open draft; updated 19:25:54Z | Head `e2c81adce7858d5a952991ba9a8b7f72e902038a`, tree `afe2679cc93bf27fa5021c7698ee7ad3ee21a1b9`; base `a70993fafc99a9f94041485542d5a720797ca394`. 35 check-runs, all terminal: 34 success, 1 skipped, 0 failure. Legacy combined status is pending with zero statuses, distinct from check-runs. |
| PR #308 | Open draft; updated 19:25:56Z | Head `8d49d0865ad5ad27b2d4e325ec135212080625e8`, tree `a80600809d373038ca76e445ba0b7b79bb7eeaa6`; target/base `e2c81adce7858d5a952991ba9a8b7f72e902038a`, tree `afe2679cc93bf27fa5021c7698ee7ad3ee21a1b9`. 26 check-runs, all terminal: 25 success, 1 failure. The failure is `build (windows-py3.12-devmode)` [job 114544417671](https://github.com/joshyorko/actions/actions/runs/38164596550/job/114544417671); this review does not infer its cause. Legacy combined status is pending with zero statuses. |

The public commit `396925cb3d6527af1768b7f5333f9653c005fc55` (tree `84f850f0d4e08b21afcd8220413ce94322370acf`) is now retrievable. GitHub compare reports it as PR #308's merge base, with the current head 30 commits ahead and zero behind. Thus the reported source checkpoint is public and integrated into the current PR branch. The check-run results above are tied to the exact current PR heads; they do not certify a newer RCC runtime.

## Source and admission boundary

The current public #308 source has the exact released-5 and candidate-727 test-only selector identities:

- released `v18.19.5`: source `d1aec7d0bb897a81274423c7a6bb747233f9c263`, modified source flag `true`, binary SHA-256 `1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e`;
- candidate `v18.19.6`: source `727c1ff8b679cdbbe12836e53c87734a1edb0071`, modified source flag `false`, binary SHA-256 `be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e`.

The exact shipping RCC tuple remains different: source `7945f956b428359928908a9f42e36417b113dafd`, binary SHA-256 `5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21`. It is absent from the adapter's approved test selectors and must not be relabeled as `727`/`be55`.

Current public changed-file blobs include adapter `5af4a1aa1113a117fc06947e947df6d5604eb7be`, staging consumer fixture `bf29b862a8efd04a553937c587a55bff20ff598a`, and verifier `df113b7713afa9d64512fc242b8ae6cb96b7773e`. The fixture binds package-cache state to source/binary identity and publication evidence; it checks owner UID/GID, a private `0700` state root, `0600` marker, symlink/containment constraints, and a cold-first publication. Its current client-home assertion accepts exactly mode `0700` or `0750`, owned by the current UID/GID. That source change is not a new raw native run and does not rewrite the earlier failure.

Earlier RCC #125 evidence remains distinct and preserved:

- the first real RCC 3 control stopped at the 2.35 GiB operational-capacity threshold and strict captured-child cleanup failed (RCC #125 owner comment `6110447853`; Actions issue #101 preserves this as the original capacity/cleanup STOP);
- the later two-fixture current-3 guard failure is owner-reported with config SHA-256 `1411879501e1ef2bd5aec7fbf9627457aa840badc8e86be980c25b207ee9d5dc` and failure receipt SHA-256 `61506cf064896dc750ccf47450d440170861cef4cf9a5c68060ac3ea3bedf5b1`: client mode `0750` versus fixture-required `0700`, before acquisition, Action, or lease. The receipt bytes themselves are not present in the public fetch set here.

The current source accepts the observed `0700`/`0750` client-home modes, but the successful fresh current-3 control receipt, actual Action outcome, lease/disposal results, cleanup/readers/final child census, and provider/cache facts are still absent. Keep both earlier failures as failures. Released 5 and exact 7276 are still NOT RUN/unqualified; source LIGHT/FULL, adapter tests, synthetic selector tests, and hosted check-runs do not substitute for actual raw runtime acceptance. Do not claim 3→5→7276 qualification. No selector covers the shipping `7945`/`5a6a…` tuple.

## Next dependency and readiness

RCC-side component review and measurements remain independently actionable under #125's existing constraints; they do not need Actions production publication. A bounded independent review can now inspect the public #308 source at exact head `8d49d…`, including the mode-aware fixture delta, candidate identity schema, and the owner-reported source FULL references. It can accept only source/control evidence within that scope.

Actual newer-runtime work remains blocked on the Actions owner supplying a current-target, inspectable source/controller/fixture run and a fresh exact current-3 control with raw receipts and numeric exits, then a separately admitted released-5 run and exact-7276 run in order. Each must bind source/tree, executable bytes/version, fixture/controller/config, owned cache and home mode/UID/GID/symlink/containment, capacity, Action result, lease/disposal, cleanup, joined readers and final child census. Preserve historical failures and any new failed attempts. If shipping `7945` is to be tested, its owner must add/qualify its own exact tuple separately.

All six RCC performance directions remain separate; this refresh produced no workload measurements, optimization winner, or materializer recommendation. Shipping/release publication remains a separate HOLD.

Documentation receipt
- Canonical guidance: no change.
- Durable learning: `396925…` is now publicly inspectable and is the merge base ancestor of current PR #308 head `8d49d…`.
- Evidence: raw GitHub issue/PR/check/commit/compare responses, current source blobs, and SHA-256 inventory in this directory.
- Stale guidance removed: none.
- Remaining uncertainty: private/raw source FULL and native runtime receipts are not available through this public refresh; Windows devmode failure cause is unknown.
