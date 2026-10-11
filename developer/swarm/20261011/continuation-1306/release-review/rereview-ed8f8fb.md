# Independent review of release-gate CI follow-up

Reviewed frozen commit `ed8f8fb68dccc264a69d2967f8019d027ebd7062` (tree `2564a8cddddc278a72a06b7a5632be13293451af`), parent `83ddea8a8e8304692b53ffe38d9c6bd03ee1d370`, based on main `727c1ff8b679cdbbe12836e53c87734a1edb0071`. The source worktree was clean before and after review. No source files were edited and no release or GitHub actions were performed.

## Review findings

The strict Go compiler check is correct: `read_go_build_info` now tokenizes the first `go version -m` line and requires the exact `go1.26.9` token, rejecting `go1.26.90` while accepting official output such as `rcc: go1.26.9`. The candidate gate exit validator also correctly distinguishes JSON integer `0` from boolean `true` and float `0.0`; both assembly-time and downloaded-bundle validation use the strict helper.

The Homebrew workflow order in source is correct: preflight before tag creation, tag before `gh release create`, and release creation before downstream dispatch. The updated CI test covers preflight before tag and tag before dispatch, but does **not** assert the release-create step precedes dispatch. It would still pass if dispatch were moved between tag creation and release creation. This is a narrow nonblocking test-coverage gap, not a current workflow ordering defect; add an explicit assertion comparing `Publish exactly the nine reviewed release assets` (or `gh release create`) with the Homebrew dispatch step.

The `native_job` test slice at `scripts/test_environment_artifact_tasks.py:318` now splits at `assemble-release-preparation`, which occurs earlier in `rcc.yaml` than `robot`. The second split therefore does not delimit the robot job and the test inspects the remainder of the workflow. All of its current required markers occur in the actual robot job, and the test still catches their absence because the candidate source expression is present only there, but the extraction should use the actual next job boundary or YAML job parsing for clear scope.

No other regression or gate weakening was found in the 38-line follow-up. The release workflow does not create tags/releases from the old recovery path; the recovery workflow remains a read-only hold. The previous semantic index checks are still present.

## Verification

- `python3 -m unittest discover -s scripts -p 'test_*.py'` — exit 0, 131 tests passed. The test suite needs `invoke==2.2.0`; it was supplied from a temporary wheel outside the source worktree. One test hashes an ignored `build/rcc` path while mocking the gate, so a temporary empty ignored fixture was created and removed immediately after the suite.
- The 37 focused release-publication/topology tests are included in the 131-test run.
- The prior re-review’s seven adversarial index cases remain covered by the tests; no repeated heavy or real RCC/native execution was run.
- Worktree status remained clean after cleanup.

## Remaining constraints

This review does not provide hosted exact-candidate release-candidate/native acceptance or prove GitHub's `rcc-production` settings, the audit-token visibility, Homebrew dispatch permission, or complete signing. The Dependabot alert details remain unknown due to the 403 response noted in the preceding security evidence. Root's publication hold remains in effect.
