# Independent review: tracked-source build guard

Reviewed exact child commit `68b8c9bf3f33336ed9bfe5a27945cf9cbf075bda` (tree `8a3491337355ade68d0d253914b3986adc5de29d`), parent `29c3cfdcc89591768a7204ec594414be0ce846f4`. Worktree status at review: `## codex/rcc-two-phase-release-20261011`. Source file SHA-256 values are in `verification.json`.

## Finding: developer builds are blocked by a release check

`tasks.py` calls `_assert_clean_tracked_workspace()` unconditionally from `build()` before each target compilation. That helper rejects every tracked working-tree change (`git status --porcelain --untracked-files=no`), and `inv build` is the normal documented developer build command as well as the CI build command. Thus a developer building an intentional local source edit now gets a `RuntimeError` before compilation. This conflicts with the accepted scope: preserve existing default/developer behavior and limit clean-source enforcement to the CI/release path.

The added regression test verifies helper behavior and textual ordering before `go build`, but does not cover the dirty-worktree developer build case. This is a blocker for accepting the patch as scoped. The guard should have an explicit CI/release-only activation path and tests showing default builds retain prior behavior while the hosted release build rejects generated tracked changes.

Other additions reviewed: native Go metadata mismatch diagnostics include expected/actual values, the four-field negative test passes, and the README TOC update corresponds to the release-process heading. No source cause for the earlier embedded Go metadata mismatch is established by this review.

## Verification and limits

Two focused Python tests passed (see `focused-tests.log`). I ran no Go tests/builds, native workloads, artifact downloads, or hosted workflow reruns. Previous parent-SHA CI evidence is not evidence for this child SHA; the reported native acceptance/review-staging facts remain attributed to the parent run only.
