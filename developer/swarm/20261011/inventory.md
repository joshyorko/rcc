# Live inventory and dependency graph

Public GitHub snapshot: **2026-10-11 02:19:58 UTC**. Main is `07db6221a3540d227f2106eccf69fe0e7c11fee0` / tree `04338ee249398525abcf9ef69b79ee1a072e2c10`; latest stable/release remains v18.19.5. Counts: **10 open issues, 3 open PRs, 69 branches, 33 tags, 27 releases, 0 milestones**. Open issue and PR search returned the sets below. #208 is closed after #231 acceptance; #98 is open again.

## Open issues

| Issue | State and acceptance boundary |
|---|---|
| [#185](https://github.com/joshyorko/rcc/issues/185) | Open, unassigned. Separate operator-governance lane; L5 hold-gated. No agent, test, label, merge, or release promotes authority to L6. |
| [#182](https://github.com/joshyorko/rcc/issues/182) | Open, unassigned. Journal lifecycle and failure-branch coverage remains. |
| [#181](https://github.com/joshyorko/rcc/issues/181) | Open, unassigned; updated 02:19:44Z. Published test-only branch `codex/rcc-181-scale-acceptance-20261011` at `3e57fde849398ea69954b4c09d5dad9ca0111361` / tree `76a02d87b2e20543e9b7991fe0db04b9ad2e20f1` adds Scale/capacity, queued-drain and all-worker panic recovery tests. Root unit/race runs passed (91.2% coverage); worker 20-shuffle run passed. No PR and not integrated. Sync wait-entry timing, default CPU/cap branches, closed-pipeline lifecycle and concurrent global WorkerCount mutation remain unproven; keep open. |
| [#135](https://github.com/joshyorko/rcc/issues/135) | Open, unassigned. Delta lint is green; original baseline findings need individual disposition. |
| [#133](https://github.com/joshyorko/rcc/issues/133) | Open, unassigned. Contributor setup/build guidance remains stale. |
| [#132](https://github.com/joshyorko/rcc/issues/132) | Open, unassigned. Import-time initialization remains; no explicit bootstrap/DI boundary. |
| [#131](https://github.com/joshyorko/rcc/issues/131) | Open, unassigned. Command/operations error-path tests and durable coverage report remain. |
| [#125](https://github.com/joshyorko/rcc/issues/125) | Open, unassigned. Research only; no materializer optimization selected. Consumer/version evidence was added. |
| [#118](https://github.com/joshyorko/rcc/issues/118) | Open, unassigned. Compatibility parent for #121/#124/#226/#227/#125 with a separate #185 governance boundary. Package-specific CPU behavior, real Rosetta, transported-path fixtures, and ABI timing remain. The architecture lane retains two-stage ABI evidence: preflight plus an exact runtime check after restoration but before Ready. Body refreshed at 02:08:16Z; provider runtime probe is bound to ancestor 097. |
| [#98](https://github.com/joshyorko/rcc/issues/98) | Open again. Private dependency dashboard returned HTTP 403; alert inventory/clearance remains unverified. Do not infer clearance or bypass access. |

## Open PRs

| PR | Head / state | Live gates |
|---|---|---|
| [#233](https://github.com/joshyorko/rcc/pull/233) | Draft, published head `324524fffa4bd0f76c618f9fbb6ede30e733b2ff`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`, freeze `codex/rcc-integrated-candidate-20261011-hosted-gate`. PR base metadata points at `11155e58…`; root verified prospective merge tree against current main 07db. | Five auxiliary checks passed: Coverage **38103534344**, CodeQL **38103534328**, PR metrics **38103534368**, Quality **38103534370**, and Lint **38103534384**. Rcc **38103534317** and contained full release gate **114364186629** are still running; no full artifact/receipt is available. All four current-head native platform receipts were independently verified by root. |
| [#224](https://github.com/joshyorko/rcc/pull/224) | Draft / NO-GO, head `32d51bd00852b003fd0d82050f5fd6d281f362d5`, old base `47a60ea…` | Historical exact-head Rcc failed. S3, materializer/CLI, integration, persistence/concurrency and platform acceptance gaps remain. |
| [#225](https://github.com/joshyorko/rcc/pull/225) | Draft / NO-GO, head `b24b29fffab4581456a313a240257d681ad744fd`, old base `47a60ea…` | Historical exact-head Rcc/lint failed. Inode/hardlink, state/concurrency, integration/security and native/full Robot gaps remain. |

Latest stable/release is unchanged. #208 was accepted and closed following #231. #98 remains open following its reopening.

## Current 324 source and active gates

Current source/tree: `324524fffa4bd0f76c618f9fbb6ede30e733b2ff` / `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`, frozen branch `codex/rcc-integrated-candidate-20261011-hosted-gate`. Root fetched the published Git object, matched it to the reviewed local tree, and verified that the prospective merge tree against main 07db is identical. The four changed files are `.github/workflows/README.md`, `.github/workflows/rcc.yaml`, `scripts/test_validate_release_topology.py`, and `scripts/validate_release_topology.py`.

Workflow implementation evidence: 23 focused topology tests passed; the actual embedded shell pipeline preserved numeric exits 0, 7 and 10, and receipt-bound metadata outside the checkout passed positive/negative controls. Root verified all four native platform receipts on current source, 12 actual bundle entries built with Go 1.26.9 and `vcs.modified=false`, and a nine-asset topology simulation using the current workflow generator and a synthetic v18.19.5 retention index. The simulation was not published. Five auxiliary checks passed; Rcc and contained ReleaseCandidateVerification remain active with no full artifact/receipt yet. These validations do not complete the current candidate release gate.

## Prior candidate attempts and inherited checks

The local 097 full-gate attempt exited numerically 1 after eight of eleven gates passed; one gate failed and two did not run. Robot was 171 PASS / 0 FAIL / 1 expected Windows skip. SelfHost failed because `GOTOOLCHAIN=local` leaked into a Conda Go 1.26.5 bootstrap that rejected the Go 1.26.9 minimum. goVet and coordination did not run. The subsequent 2de local wrapper did not launch because preflight disk was 6.82 GB, under the 10 GB minimum. Historical 680f acceptance is separate: all eleven gates were reported passed, but the outer wrapper numeric exit was not preserved. Do not rebind 097, 680f, or 2de results to 324.

Strict-consumer and security evidence remains ancestor-bound to 097. Strict verification passed four numeric commands, preserved the original padded root/signatures, and rehashed 5,049 actual CAS files against 5,045 indexed objects without mismatches; the refreshed empty revocation snapshot was explicitly unsigned. The four-target default-tag security scan had no call-traced findings. GO-2026-5970 package-level and GO-2026-6629 module-level findings remain. The 17-command provider probe found named-profile setup needs an explicit trust carrier on both candidate and stable controls; unavailable HTTP trust fails closed, while retained filesystem-carrier offline use succeeds with the same policy/evidence. These are not direct 324 checks.

The Actions opt-in version adapter at immutable `joshyorko/actions` commit `a70993fafc99a9f94041485542d5a720797ca394`, adapter SHA-256 `8f7642f4b1ee8ff658dbf2414ec34ca28747054833f73f4996fb4210d9e5dde5`, accepts only RCC v18.19.3; v18.19.5 and v18.19.6 are rejected. This is an existing consumer constraint, not a candidate regression. No Actions repository state changed.

## Dependency graph

```mermaid
flowchart LR
  I118["#118 open: compatibility parent"]
  I181["#181 open: test-only branch 3e57, not integrated"]
  I121["#121 closed: predecessor acceptance, residual fixtures"]
  I124["#124 closed: trust/carrier acceptance"]
  I226["#226/#227 merged: worker/coordination"]
  I208["#208 closed after #231 acceptance"]
  P228["#228 merged: server"]
  P231["#231 merged: provider docs"]
  I125["#125 open: benchmark/consumer research"]
  P229["#229 merged: observational profiles"]
  P233["#233 draft: current 324; hosted gate active"]
  P234["#234 merged: UV alias runtime fix"]
  P224["#224 draft / NO-GO"]
  P225["#225 draft / NO-GO"]
  I185["#185 open: operator governance, L5 hold"]
  P230["#230 merged: compiler/N-1 gate"]
  I98["#98 open again: dashboard unresolved"]
  I118 -->|explicit| I121
  I181 -->|branch evidence; no PR/integration| T181["3e57 test-only checkpoint"]
  I118 -->|explicit| I124
  I118 -->|explicit| I226
  I118 -->|explicit| I125
  I118 -->|explicit, separate authority| I185
  P228 -->|server acceptance for| I208
  P231 -->|docs acceptance for| I208
  I118 -->|explicit| I208
  P229 -->|evidence; no optimization selected| I125
  I118 -->|explicit experiment refs| P224
  I118 -->|explicit experiment refs| P225
  P234 -->|merged in main| P233
  P233 -->|requires exact-head acceptance| REL["hosted 11 gates + native platforms + distribution/signing acceptance"]
  P230 -->|mention; clearance not proven| I98
```

## Curated source-bound receipts

| Claim | Receipt | SHA-256 |
|---|---|---|
| Current 324 actual 12 binary entries (Go 1.26.9, VCS clean) | `evidence/hosted-native/pr233-current-324524f/root-all-eight-and-native-bundle-verification.json` | `949572a7a25da3f1151a0557862eec833a31a3cc25eabdd84a6706f87190f2a3` |
| Current 324 all-four native platform verification | `evidence/hosted-native/pr233-current-324524f/root-native-progress-verification.json` | `69776b371357d9365be3a906046c79843f02253b886ef2426fe2819625a8f48d` |
| Current 324 distribution consumer and nine-asset simulation | `evidence/hosted-native/pr233-current-324524f/root-distribution-consumer-verification.json` | `3b590d7a1d7917a82e8696b12521fdf36aa101c4b59d0482fbdd044701231232` |
| Published 324 object/tree and prospective main merge-tree binding; 23 topology tests; shell fixtures 0/7/10 | `evidence/hosted-rc-gate/root-publication-verification.json` | `8c469eac802f8986894dfef9714c6696a54a08ba44fc6916b3c2064bcf46e04b` |
| Workflow shell-exit review and publication boundary | `evidence/hosted-rc-gate/root-independent-workflow-review.json` | `991aa8f8cebdd411eeff13df01ea5862c14d43fa29a49d71efd7e89a28f50489` |
| 2de bundle identity (parent source only) | `evidence/hosted-native/pr233-current-2de5/root-all-eight-and-native-bundle-verification.json` | `48fdef427adbea72071d439dd46a0ea3f46dab792a6ef661631ea27bf743eb44` |
| 2de native progress (parent source only) | `evidence/hosted-native/pr233-current-2de5/root-native-progress-verification.json` | `ce193a5407821871da7aaba4ac76822690a77a292745008a8b18810e258610a2` |
| #181 test-only branch publication and source-tree binding (not integrated) | `evidence/issue181-scale-20261011/root-publication-verification.json` | `74b9e4ed4986f07b0d73590cf4f61bd95029836dee05e9b612c6da2e92fc4fe3` |
| #181 root unit/race verification, 91.2% coverage (not integrated) | `evidence/issue181-scale-20261011/root-independent-verification.json` | `54a368a60a293b8ab9b8ce5f90a86395f3bafd0514c18294c2bc392bcd62f8e3` |
| 097 strict-consumer proof (ancestor-only) | `evidence/candidate-09796/strict-trust/root-independent-verification.json` | `73660999974864783c0087f341bb2d62d069a9666403de0c20ebf661c60fa0c4` |
| 097 provider runtime probe (ancestor-only) | `evidence/consumer-contract-125-20261011/provider-contract-probe/root-independent-verification.json` | `8d44067601f698c6c0044cf98a014620a37e5366d32378a4dff6f4d4dd1ceb51` |

The focused workflow tests and shell fixtures do not replace the active hosted release gate. No full release acceptance, tag, publisher signature, notarization, or release approval is recorded.

## Worker assignments

| Worker | Requested model / effort | Scope and ownership | State |
|---|---|---|---|
| Root | Sol / High orchestration role | Integration, source identity, independent receipt review and merge decisions | Active |
| pr_readiness | Luna / High | Current324 four native platform receipts, 12 bundle entries and distribution simulation | Complete; full RC still active |
| native_acceptance | Luna / High | Current324 contained full release gate, numeric exit and final artifact/archive verification | Active; no full artifact yet |
| consumer_contract_125 | Sol / Extra High | Consumer and ABI source review complete; now independently verifying N−1 archive object closure | Active |
| workers_226 | Luna / High | Worker correctness merged; hosted gate and #181 scale/capacity/panic tests complete; 3e57 test-only branch has no PR/integration | Complete; #181 remains open |
| identity_118_124 | Luna / High | Strict097 trust proof and lossless owned-home preservation | Complete |
| graph_inventory | Luna / High | Live graph and checkpoint drafts | Complete |
| performance_125 | Sol / Extra High | Observational baseline and publication profile; no optimization | Complete |

Models identify the assigned swarm lanes recorded in the prior handoff. No Astra lane was used. Each implementation lane used its own worktree; evidence-only lanes wrote separate directories. The #181 test lane and does not alter the frozen release candidate.
