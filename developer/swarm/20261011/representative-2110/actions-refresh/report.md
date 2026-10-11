# Actions owner-state refresh

Read-only refresh of public `joshyorko/actions` on 2026-10-11 UTC. No Actions/RCC source, refs, pins, homes, worktrees, controllers, or issues were modified; no test, workflow, build, or runtime was launched. Raw GitHub API responses, pagination-complete issue comments, source blobs, and hashes are in this directory.

## Current refs and hosted checks

- PR #221 remains **open draft**. Head `integration/community-release-20261008` is `e2c81adce7858d5a952991ba9a8b7f72e902038a`, tree `afe2679cc93bf27fa5021c7698ee7ad3ee21a1b9`; base `community` is `a70993fafc99a9f94041485542d5a720797ca394`. Current check-runs: 35 total, 34 success, one expected `publish` skip, zero failures. The separate legacy combined-status endpoint says `pending` with no status contexts; this is not a failed check-run.
- PR #308 remains **open draft**. Head `fix/rcc-candidate-wheel-identities-20261010` is `8d49d0865ad5ad27b2d4e325ec135212080625e8`, tree `a80600809d373038ca76e445ba0b7b79bb7eeaa6`; base integration head is `e2c81adce7858d5a952991ba9a8b7f72e902038a`. Current check-runs: 26 total, 25 success, one failure: `build (windows-py3.12-devmode)`, job `114544417671`. The cause is not inferred. The separate legacy combined-status endpoint is `pending` with no status contexts.
- Public commit `396925cb3d6527af1768b7f5333f9653c005fc55` is retrievable and is the merge base of #308's current head; current head is 30 commits ahead, zero behind. This confirms public source ancestry, not native runtime acceptance.

## Issue and comment refresh

All requested issues remain open. The complete comment pages and issue bodies are retained locally with hashes.

- #101: issue body updated 19:25:51Z; it reports current #308 `8d49…`, source FULL `1470 PASS / 13 SKIP` and decision reference `073e235a`, while explicitly saying current RCC 3, released 5 and exact 727 are not run or unqualified and production remains 18.19.3. Its newest comment is older, 05:09:50Z; the later body edit is distinct.
- #133: newest comment 20:32:56Z is this successor's coordination receipt. It says the Actions owner retains #101/#133/#134/#139/#143 and both draft PRs, asks for raw current-3 controller/config/fixture evidence, and preserves the order 3 → released 18.19.5 → exact 727. It explicitly distinguishes `7945`/`5a6a…` shipping RCC from Actions `727`/`be55…`.
- #134: newest comment 01:44:42Z remains source-component-only; actual metadata inspection is **NOT RUN** and full supervisor/controller prerequisites remain on HOLD.
- #139 and #143: no issue comments; both remain open. Their contract bodies remain broad conformance/adapter specifications, not actual-run receipts.
- #221 has two comments; its newest (12:12:50Z) is stale relative to current PR head. #308 has no issue comments. Current PR API objects and checks therefore control current ref/check state.

## Current source, fixture, verifier, and public receipts

At exact #308 head `8d49d…`, the recursive tree is complete (1,742 entries; not truncated), and the PR changed-file API returns nine files. The current adapter/fixture/verifier files were re-fetched from GitHub Contents API at that exact ref and independently verified by Git blob SHA:

| Role | Current path | Git blob SHA | SHA-256 |
|---|---|---|---|
| Adapter / exact test-only compatibility tuples | `action_server/src/actions/server/_rcc_runtime_adapter.py` | `5af4a1aa1113a117fc06947e947df6d5604eb7be` | `c1884571a572277beef50e359f8b5e71abf593e1ebdee4bea2e47290c8da81f2` |
| Runtime adapter source tests | `action_server/tests/action_server_tests/test_rcc_runtime_adapter.py` | `9626a5dac2a84d01317b86f655907fe1c8874dd8` | `1f1156908b523012c40fe95a2063764dbb11696a927e0330dc8d8b28c5de6ee7` |
| RCC acceptance controller/verifier | `action_server/scripts/verify_dakota_rcc_acceptance.py` | `df113b7713afa9d64512fc242b8ae6cb96b7773e` | `8f21502f64fed9ee5d26eb6a080a625ee575954e1f659c1f51ee1ea31bda57c7` |
| Acceptance controller tests | `action_server/tests/action_server_tests/test_dakota_rcc_acceptance.py` | `5bd6d59f49c2f12f2920a8bfc4d5347c65af0d4e` | `8bd97a3f7e2c02553fe205d3bfd0989d6bd1be09e13625c8b326820fdf999e50` |
| Owned package-cache fixture / mode controls | `action_server/tests/action_server_tests/test_source_staging_rcc_consumer.py` | `bf29b862a8efd04a553937c587a55bff20ff598a` | `a30f9e585b8dc422dcf853ac27a2da2c11f84065e58ba501b0a4c32d4f472728` |

The adapter source contains only two exact test selectors: released `v18.19.5`, source `d1aec7d0bb897a81274423c7a6bb747233f9c263`, modified flag `true`, SHA-256 `1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e`; and candidate `v18.19.6`, source `727c1ff8b679cdbbe12836e53c87734a1edb0071`, modified flag `false`, SHA-256 `be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e`. The exact shipping RCC source `7945f956b428359928908a9f42e36417b113dafd` / binary `5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21` is not an approved selector and is not interchangeable with 727/be55.

The verifier defaults to exact production `v18.19.3` / SHA `7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428`. If an explicit test selector is present, it checks exact selected binary digest and reported version through the adapter policy. This is source behavior only. The current source-staging fixture requires a private mode-0700 state root and 0600 marker, while it accepts the client-home mode as either 0700 or 0750 when owned by the current UID/GID; cache target and marker checks remain separate. That fixture policy does not erase historical mode-failure receipts.

The current public tree has only three committed `candidate-wheel` evidence JSONs. All three are RCC 18.19.3 / `7e588…` source-bound historical failures: source SHAs `67e82ef7…`, `7cf11c8b…`, and `e33987b6…` are public ancestors of current #308. Their RCC receipts have `status=failed`, `exitCode=-1`, reason `child exited non-zero`, and `verification.valid=true`; the latter two explicitly label overall acceptance FAIL (while recording successful HTTP/SQLite/artifact/process-cleanup cells). They are retained failures, not current-head green runtime acceptance. No raw current-head controller/config/fixture run is present; no released-5 or 727/be55 actual runtime receipt is present in the owner repository tree. Current source/checks and committed receipts do not prove the ordered 3 → 5 → 727 runtime chain.

## Admission verdict

**NO QUALIFIED NEWER RCC RUNTIME; ORDERED RUNTIME ADMISSION NOT COMPLETE.** Owner repo evidence proves current source policy/fixtures and hosted source gate state. It does not provide a source-bound successful current-3 raw run with exact controller/config/fixture hashes, numeric exits, Action result, cache/home owner+mode, capacity, lease/disposal, cleanup, joined readers, and final child census; therefore released-5 and 727/be55 remain blocked by the existing stop-on-first-failure order. Do not reinterpret source FULL, CI candidate-wheel success, or RCC component experiments as those runtime receipts. Preserve the 0750-vs-0700 and capacity/cleanup failures as failures. Production pins remain 18.19.3. No new Actions task or root steering is warranted by this refresh.

Raw reads and current source are retained under this directory. SHA inventory: [raw-sha256.txt](raw-sha256.txt). The immutable RCC shipping checkpoint and earlier accepted failures remain distinct.
