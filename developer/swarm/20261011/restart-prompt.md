# RCC restart prompt

Continue the previously authorized bounded work for `joshyorko/rcc`. Authorization covers isolated RCC implementation and CI/workflow fixes, evidence-backed issue/PR body updates, pushes, and dependency-safe accepted merges. Do not mutate other swarm branches/workflows in `joshyorko/actions`. Do not promote #185 governance authority. Do not tag or publish v18.19.6 without separate release authorization.

## Current public state

Fresh read-only snapshot at **2026-10-11 02:51:36 UTC**: main is `727c1ff8b679cdbbe12836e53c87734a1edb0071`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`. PR #233 merged at 02:29:54Z; main tree equals the accepted PR source tree. There are **10 open issues, 2 open PRs, 70 branches, 33 tags, 27 releases, 0 milestones**. Open PRs #224/#225 remain drafts and NO-GO. #208 is closed after #231 acceptance; #98 remains open. Latest stable is v18.19.5; no v18.19.6 tag/release exists.

## Accepted PR #233 and exact-source gate

PR #233 source `324524fffa4bd0f76c618f9fbb6ede30e733b2ff`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`, merged as main `727c1ff8b679cdbbe12836e53c87734a1edb0071`. All six PR checks and all four native receipts passed. The contained full release-candidate gate run **38103534317 / job 114364186629** has numeric gate exit 0 and tee exit 0; all eleven gates passed. Robot: 171 PASS / 0 FAIL / 1 expected Windows skip. Coordination: six scenarios. Six source/self-host binaries were rehashed and verified as Go 1.26.9 builds. Root verified the actual merged source and tree.

The 2 GiB stream test used a 64 KiB buffer and reported a 118,752-byte heap delta; this is not RSS. The actual N-1 v18.19.5 archive hash was `9ed01cbfe2463f0f064ab10efaf61e8e48a9c96767e7b6aa0629e4e8295bcb85`; root rehashed 5,045 stored/logically closed objects with zero identity mismatch. The uploaded pre-acquire `artifactBefore` snapshot was empty. A private post-acquire snapshot was not persisted, so root cannot independently count the two removed provisional records; the internal numeric-gated validator passed. Preserve this limit without changing the gate result.

Root verified 12 current-source bundle entries built by Go 1.26.9 with `vcs.modified=false`, plus a nine-asset simulation using the actual workflow generator and synthetic v18.19.5 retention index. The simulation is not a published release. Full-gate/native/archive receipt paths and hashes are in `inventory.md`.

## Post-merge main checks

Root independently checked main `727c1ff8…`: assets exited 0, CGO=1 full Go tests exited 0, then all 116 Python script tests exited 0 after building `build/rcc`. The first script attempt exited 1 because that binary had not yet been built; its log is retained. Actual clean Go 1.26.9 main binary SHA-256 is `daf9143e7caf0fc2dcf59e999396c5168d4148c7d1bae4fa59ccbbe620018998`.

All four required main push workflows are green: Rcc **38105340592**, CodeQL **38105340724**, Coverage **38105340564**, and Lint **38105340590**. All four native-platform ZIP/API-digest and actual-binary verifications passed, with Robot, lease/SQLite, warm-provider-dead, mismatch-zero, and Linux JAT evidence; UV logs show 28 top-level and 16 nested checks passed. All12 actual staged bundle entries passed root SHA/size/API-head/compiler/VCS727-clean checks. Main full11 skips on non-tag pushes by policy; the successful numeric-zero 11-gate result remains source-bound to PR candidate 324. Root receipt: `evidence/final-main-727/root-final-main-acceptance.json`, SHA-256 `fa9ab559894b4f819bd7ef68c140f945cb61a874794a29e4f625e6e8599f0461`. Extra workflow run 38106400198 is green but its conditional release bump/dispatch steps were skipped; it is neither a required fifth gate nor publication.

## Separate open issue branches

Issue #133 remains open; its body was updated 02:32:55Z. The published docs-only branch `codex/rcc-133-contributor-map-20261011` at `012703c2474dd8585972edba0695bad4633d9230` / tree `884db3fd157fb540e4d48261d164e36896c20a01` changes only four directory rows in `CONTRIBUTING.md`. Root reviewed/fetched the commit and matched the reviewed tree. No PR exists; it is not integrated.

Issue #181 remains open; its body was updated 02:19:44Z. The separate test-only branch `codex/rcc-181-scale-acceptance-20261011` at `3e57fde849398ea69954b4c09d5dad9ca0111361` / tree `76a02d87b2e20543e9b7991fe0db04b9ad2e20f1` has root unit/race passes at 91.2% coverage and a passing 20-shuffle worker run. No PR exists; it is not integrated. Residual unproven cases: Sync wait-entry timing, default CPU/cap branches, closed-pipeline lifecycle and concurrent global WorkerCount mutation.

## Remaining compatibility/security and publication boundaries

Issue #118 remains open and was updated 02:33:43Z. The retained ABI acceptance direction is two-stage: preflight plus exact runtime verification after restoration and before Ready. Real package-specific CPU behavior, Rosetta, transported-path fixtures, and broader ABI/native acceptance are outstanding. #125 remains research with no optimization selected. #98 dashboard access remains unresolved. #185 remains separate, L5 hold-gated governance.

The Actions opt-in adapter at immutable commit `a70993fafc99a9f94041485542d5a720797ca394` accepts only RCC v18.19.3; v18.19.5 and v18.19.6 are rejected. No Actions repository state changed. Strict-consumer, four-target security and provider runtime evidence remain bound to ancestor 097; GO-2026-5970 package-level and GO-2026-6629 module-level findings remain despite no call-traced affected-symbol findings. Do not rebind those receipts to main.

PR #233 is accepted and merged. Main 727 required push and native checks, staged bundle identity, and source build/script verification are independently accepted. No v18.19.6 tag/release exists; separate publisher/signature/tag authorization remains. Main full11 is skipped on non-tag push by policy; keep its successful candidate324 receipt separate. Security dispositions remain: GO-2026-5970 package-level and GO-2026-6629 module-level; private dashboard #98 is unresolved (HTTP 403). A successful gate or merge alone does not grant release authority.

Current main real-stable-index simulation also passes: actual release asset549249378 SHA256fbf3f53357f40463790a9f6b6bd6c9f5e8fcd69ad158831a6620d5d149f4f4de,19 retained records+new6 underexisting20cap; v5/v3 retained, oldestv18.12.0 omittedbycap,noassetsdeleted. Do not reuse the unlaunched2de wrapper unchanged: its transcribed expected prior-binary hash differs from actual bytes. Preserve that historical script and the root reuse note; hosted324 superseded it.

## #125 admitted research ledger (separate from release)

The live #125 body was updated 2026-10-11 02:46:28 UTC. Performance work is explicitly not a v18.19.6 prerequisite; it may select zero optimizations. Keep six candidates separate: gzip baseline/zstd dual-read, small-object packfiles, prewarm/persistent workers, reflink/clonefile, lazy/FUSE, and compressed immutable image/COW overlay. None has a candidate performance result or selected implementation in the Actions research lane. Existing merged #229 instrumentation is observational only; historical #64 figures still need reproduction or explicit retirement.

Before a real Actions workload, use a disposable isolated checkout of immutable adapter commit `a70993fafc99a9f94041485542d5a720797ca394` and unchanged released RCC v18.19.3 control. The adapter accepts v18.19.3 and rejects v18.19.5/v18.19.6. Confirm Actions-owner approval for any candidate qualification and stay within the admitted <=500 MiB new-disk budget. Do not edit Actions workflows, pins, or branches, add infrastructure, change RCC defaults/formats, or claim this lane as release acceptance. Do not touch other workers’ worktrees. No real Actions workload has yet run.

The six admitted workloads are: (1) first Action with cold acquire; (2) warm calls/persistent-worker reuse; (3) source-only autoreload without environment rebuild; (4) environment-changing reload with generation pinning and lease drain; (5) multiple packages/concurrent workers; (6) provider outage/restart/recovery with last-good preservation. Collect repeated wall/CPU/peak-RSS and filesystem/object-operation/cache evidence with exact source, binary, fixture, and platform provenance. Candidate tests require workload evidence, integrity/security/relocation correctness, platform/fallback proof, and rollback gates; they do not authorize implementation or a release default.

Cross-project graph to retain without creating issues: RCC #118 is the compatibility/ABI parent; #125 research consumes the existing Actions #101 campaign, #133/#134 adapter and execution integration, #139/#143 adapter conformance, and #221 Runtime integration. RCC #133 is a separate documentation issue. RCC #229 supplies benchmark instrumentation, not a selected optimization. #185 stays a separate L5 governance hold. The Linux baseline (cold acquire 24.209 s median, warm local 0.700 s, provider-dead local 0.717 s, one publish 34.551 s) is shared-host evidence with no Action fixture or isolated phase timings and cannot rank candidates. The separate 680f profile is not a comparable speedup.


## #125 execution status after main acceptance

Main release verification is complete, but #125 performance research remains an independent, non-release lane. No real Actions workload has run. The six admitted workloads still require the unchanged isolated v18.19.3 adapter control at immutable commit `a70993fafc99a9f94041485542d5a720797ca394`, existing Actions-owner approval for candidate qualification, and the <=500 MiB new-disk budget. The 17-command trust-carrier fixture is a #118 trust-input result on source 097, not an Actions runtime benchmark. Keep all six candidate investigations separate; select no optimization/default or new artifact format absent repeated representative workload evidence.

At the latest lane refresh, four agents are active of seven available: root (Sol/High), `consumer_contract_125` (Sol/Extra High), `pr_readiness` (Luna/High), and this Luna/High evidence ledger. `native_acceptance` completed main push/native verification; identity and worker branches are also complete. #125 workload measurements remain active research planning and have no actual Actions workload result. Native artifact signing evidence is limited: macOS ARM64 reports verified, macOS Intel unsigned/unverified, and Linux/Windows do not report signing. No published asset/tag/release or signing acceptance is claimed.
