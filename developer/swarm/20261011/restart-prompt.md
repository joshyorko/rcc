# RCC restart prompt

Continue the previously authorized bounded work for `joshyorko/rcc`. Authorization covers isolated RCC implementation and CI/workflow fixes, evidence-backed issue/PR body updates, pushes, and dependency-safe accepted merges. Do not mutate other swarm branches/workflows in `joshyorko/actions`. Do not promote #185 governance authority. Do not tag or release until exact-head gates pass and release authorization is satisfied.

## Current exact public head

Read-only snapshot at **2026-10-11 01:58:27 UTC**: main is `07db6221a3540d227f2106eccf69fe0e7c11fee0` / tree `04338ee249398525abcf9ef69b79ee1a072e2c10`; 10 open issues and 3 open PRs. #208 is closed after #231 acceptance; #98 is open again. Counts are 67 branches, 33 tags, 27 releases, and 0 milestones; latest stable/release is v18.19.5.

PR #233 is an open draft at published head `324524fffa4bd0f76c618f9fbb6ede30e733b2ff`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`, frozen branch `codex/rcc-integrated-candidate-20261011-hosted-gate`. The PR base metadata remains `11155e58…`; root verified that merging its current source against main 07db yields exactly the current source tree. Do not silently treat base metadata as updated.

## Active hosted gate

Latest public workflow read: Rcc run **38103534317** is in progress; Coverage gate **38103534344** has passed. CodeQL **38103534328**, PR metrics **38103534368**, Quality report **38103534370**, and Lint **38103534384** passed. The contained `ReleaseCandidateVerification` gate **114364186629** is running, Build **114364187148** passed. Do not claim success until exact-head workflow run and numeric receipts are complete and verified.

The new workflow/validator implementation passed 23 focused topology tests; actual embedded shell-pipeline controls returned 0, 7 and 10 with those codes preserved, and outside-home metadata positive/negative cases passed. Root independently confirmed the published object, the reviewed source tree, and prospective merge tree. Receipt paths and hashes are in `inventory.md`. This establishes the workflow implementation only; the hosted release gate remains active.

The prior 097 local full gate exited 1 after eight gates passed. Robot was 171 PASS / 0 FAIL / 1 expected Windows skip. SelfHost failed because `GOTOOLCHAIN=local` leaked into the Conda Go 1.26.5 bootstrap; goVet and coordination did not run. The 2de wrapper did not launch because preflight had 6.82 GB free against a 10 GB minimum. Neither is current 324 acceptance. Parent 2de had Linux/macOS-arm/Windows native receipts pass with Intel pending; do not report those as 324 native acceptance.

## Compatibility and release boundaries

Actions opt-in adapter at immutable commit `a70993fafc99a9f94041485542d5a720797ca394` accepts only RCC v18.19.3; stable v18.19.5 and candidate v18.19.6 are rejected. This is an existing consumer constraint. Do not write to `joshyorko/actions`.

Strict-consumer proof, four-target security scan, and 17-command provider runtime probe are bound to ancestor 097, not current 324. The provider probe confirms the named-profile explicit-carrier gap across candidate and stable controls. The strict proof passed four numeric commands and rehashed 5,049 CAS files; the refreshed empty revocation fixture was unsigned. GO-2026-5970 package-level and GO-2026-6629 module-level findings remain despite zero call-traced affected-symbol findings. See the inventory receipt table for paths/hashes and scope.

There is no release tag, publisher signature, notarization, or v18.19.6 release acceptance. #125 remains research with no optimization decision; #118 remains open for CPU, Rosetta, ABI, native/platform, artifact-carried and path compatibility; #98 is open with private dashboard access unresolved; #185 remains independent, L5 hold-gated governance.

Active lanes: root coordinates exact-head gate completion and preserves source-bound receipts; worker226 owns the label-opt-in full-11 hosted workflow, numeric-exit propagation, and actual-binary archive capture. The release-candidate label is already on #233. Strict identity and provider-contract probing are complete only for 097. No model identity is asserted without source evidence. These checkpoint drafts do not mutate GitHub or repository source.

Current324 root build/interface evidence is in `evidence/hosted-native/pr233-current-324524f/root-all-eight-and-native-bundle-verification.json` and `root-distribution-consumer-verification.json`. ABI timing source review is `evidence/architecture-118-abi-20261011/report.md` plus `root-independent-review.json`; this review launches no new runtime tests and does not waive broad118 acceptance.
