# Independent review: release-only clean-source opt-in

Reviewed frozen commit `c6c7a01239635e07f28517e2fc84230e0ab42509` (tree `49baf943b9d64ef79af56ec8f34eebe0385477b1`), parent `68b8c9bf3f33336ed9bfe5a27945cf9cbf075bda`. Worktree status at review: `## codex/rcc-two-phase-release-20261011`. Source-file hashes are recorded in `verification.json`.

## Result

The prior unconditional-guard blocker is fixed in this child. `tasks.build` invokes `_assert_clean_source_if_required`; the helper calls the tracked-tree check only when `RCC_REQUIRE_CLEAN_SOURCE` is exactly `1`. The `rcc.yaml` build job sets that value, so hosted builds fail before `go build` if prerequisites dirty tracked files. The variable is not set for normal local use, preserving existing dirty-tree developer builds.

The new test executes the build body against a committed temporary Git repository with a tracked edit: default mode reaches the mocked compiler; opt-in mode raises before the compiler call. A second test checks the helper’s tracked/untracked semantics and order. The metadata diagnostic reports each expected/actual Go field, and its four negative cases are covered. The README TOC includes the release-process heading; separate source-bound evidence records generation as idempotent with equal before/after README SHA-256.

No blocker found in the reviewed scope. This review does not assert a root cause for the earlier embedded Go metadata mismatch. The 139-test suite and TOC idempotence are attributed to the supplied exact-source receipt, not rerun here.

## Verification

My focused run passed 3 Python tests; YAML parsed and confirmed exact `RCC_REQUIRE_CLEAN_SOURCE: "1"` on the build job; `git diff --check HEAD^ HEAD` exited 0. Full source-bound verification receipt: `/workspace/work/rcc-swarm/evidence/continuation-20261011/p1-hosted/29c3-correction-tests/optin-clean-source-verification.json`, which records 139 tests, TOC before/after SHA equality, exact source/tree, and clean worktree.
