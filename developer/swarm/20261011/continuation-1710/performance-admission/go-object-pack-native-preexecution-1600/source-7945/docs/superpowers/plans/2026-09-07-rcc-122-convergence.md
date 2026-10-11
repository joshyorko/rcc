# RCC #122 Convergence Integration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate the still-required #122 lifecycle work into current main, prove the combined lifecycle/race/native acceptance, merge one reviewable integration PR, and only then cut the RCC release that contains merged #219 plus accepted #122 work.

**Architecture:** Start from `de919082c8e994f4c179bee646adac0d89794cbd` in a fresh integration branch. Preserve useful upstream commit history by applying the accepted PR commits in the required order `#215 -> #213 -> #214`; resolve conflicts against current main in the integration worktree and keep only behavior that remains relevant to #122. The release remains held until the integration PR has exact-head review and all required gates.

**Tech Stack:** Go 1.26.5, RCC contained developer toolkit, Go race detector, Robot Framework, GitHub Actions native matrix, GitHub pull requests and release workflows.

**Spec:** GitHub issue #122, current RCC repository guidance in `AGENTS.md`, `docs/agent-boundaries.md`, and the user’s convergence mandate.

## Global Constraints

- Integration base is exactly `de919082c8e994f4c179bee646adac0d89794cbd`; preserve merged #219 and do not release while this plan is incomplete.
- Apply #215, then #213, then #214; preserve useful original commits where they apply and make conflict-resolution commits explicit.
- Do not merge #188, #190, or #193 as-is; do not blindly merge #189, #192, #197, #202, or #191. Salvage only a defect or test that still reproduces on current main.
- Do not change protections, force-push, bypass reviews, publish tags, dispatch release publication, or update Actions before the integration acceptance and merge.
- Use contained RCC tasks and real lifecycle/native evidence; an interrupted or skipped gate is unverified, not passing.

### Task 1: Establish the integration ledger and ordered source stack

**Files:**
- Create: `docs/superpowers/plans/2026-09-07-rcc-122-convergence.md`
- Runtime ledger: GitHub issue #122 comment and later exact-head updates

**Inputs:**
- #215 commits `01fa4af8a3af86e0cb9e38aab971c66d3f906140`, `8e79e2b1a34024a9a347f9dd01ab0d9950d1ab79`
- #213 commits `a5893f332578bbcb61468e09979b1b0733fda636`, `7fc8d9786647b306f8f1595cfa0a695530f0528f`, `8e6770e956e1bd2c5bb64328fef82cb050131db2`, `f93a2073e3b62644e284128e00585db7178588d2`, `e0c0c4c8e0e83628d2cb49f0b854dee6c61c19b`, `22a511e7ec13847e97e9193572495cd00bedd084`, `9350112831161ba21dfc7db9bacf0f2bdc4b04ec`, `b8f1369162ff58defe1efe38cf2b1bc998e78ba2`
- #214 commits `45227049a370e32dfa19f19af880064ca26a7fdb`, `f9776d221126996f158dd023f1b916f1b4372a53`

- [ ] Confirm the worktree is clean and `HEAD` is `de919082c8e994f4c179bee646adac0d89794cbd`.
- [ ] Record the release hold, current v18.19.3 state, merged #219 SHA, selected source commits, and unmerged open-PR disposition on issue #122.
- [ ] Cherry-pick #215’s two commits in order; stop at conflicts and resolve only in the integration worktree.
- [ ] Cherry-pick #213’s eight commits in order after the resolved #215 stack.
- [ ] Cherry-pick #214’s two commits in order after the resolved #213 stack.
- [ ] Inspect the resulting diff and commit history before any behavioral edit; keep unrelated changes out.

### Task 2: Resolve and verify current-main lifecycle integration

**Files:**
- Modify only conflicted files under `environmentlifecycle/` and their directly required tests/platform files.
- Do not hand-edit generated `blobs/` or unrelated release/version files.

- [ ] Resolve each cherry-pick conflict against current main while preserving #219’s provider/coordination behavior.
- [ ] Run `git diff --check` and inspect the complete integration diff against `de919082`.
- [ ] Run focused lifecycle tests for changed packages before broader gates:
  `rcc run -r developer/toolkit.yaml --dev -t artifactFocused`.
- [ ] Run lifecycle race coverage:
  `rcc run -r developer/toolkit.yaml --dev -t artifactRace`.
- [ ] Record any source defect that still reproduces on current main; do not import unrelated open-PR work merely because files overlap.

### Task 3: Run combined #122 acceptance

**Files:**
- Receipts under `tmp/` only; preserve them as ignored evidence.

- [ ] Run the current RCC A-to-B lifecycle vertical:
  `rcc run -r developer/toolkit.yaml --dev -t artifactVertical`.
- [ ] Run the JAT-class real consumer A-to-B vertical with its contained 30-minute timeout:
  `rcc run -r developer/toolkit.yaml --dev -t artifactConsumerVertical`.
- [ ] Run Environment Artifact Robot acceptance:
  `rcc run -r developer/toolkit.yaml --dev -t artifactRobot`.
- [ ] Run the contained Go unit suite and vet gate:
  `rcc run -r developer/toolkit.yaml --dev -t unitTests` and `rcc run -r developer/toolkit.yaml --dev -t goVet`.
- [ ] Build and exercise the exact integration binary with `rcc run -r developer/toolkit.yaml --dev -t local`.
- [ ] Record PASS, FAIL, or NOT RUN for every lifecycle, race, runtime, and rollback/recovery boundary; never convert an interruption into PASS.

### Task 4: Exact-head independent review and hosted four-native acceptance

- [ ] Push only the coherent integration branch and create one PR against `main` linking #122 and #219.
- [ ] Require the PR’s exact head in every hosted check.
- [ ] Run the existing native matrix through the PR workflow; require Linux, macOS amd64/arm64, and Windows native runtime jobs to pass.
- [ ] Obtain one independent exact-head review covering lifecycle ordering, cleanup/rollback, cross-platform behavior, and preserved release/security boundaries.
- [ ] Address only Critical/Important findings with scoped commits and repeat the affected exact-head gates.
- [ ] Reconcile #215/#213/#214 as absorbed by the integration PR after exact evidence; keep #188/#190/#193 and any unmatched #189/#192/#197/#202/#191 work deferred or separately tracked.

### Task 5: Protected merge, release, and downstream handoff

- [ ] Merge the single integration PR normally at its exact reviewed head; do not squash, force-push, or bypass protections.
- [ ] Verify the merge SHA and successful main workflow before release automation proceeds.
- [ ] Let the established version workflow select the next unused version; retain v18.19.3 and run the tag workflow only after release-candidate gates pass.
- [ ] Verify release-candidate lifecycle/race/native/upgrade/rollback receipts, tag target, release URL, every downloadable asset, and SHA-256 values.
- [ ] Update Actions only once, after the published stable RCC version and exact release evidence exist.
- [ ] Publish the final concise ledger and repository documentation receipt with execution proof, acceptance proof, release proof, skipped gates, and remaining uncertainty.
