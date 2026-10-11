# #181 current-main source checkpoint

## Source and integration

- Frozen current main: `7945f956b428359928908a9f42e36417b113dafd`.
- Accepted remote source checkpoint: `e53624a359c1e73a6ef01e5daf7c2138b693d3a3` (tree `ac1044edc1ede2a6fea6354eabf686af963bd4fa`), with parents `3e57fde849398ea69954b4c09d5dad9ca0111361` and `727c1ff8b679cdbbe12836e53c87734a1edb0071`.
- New local branch: `codex/rcc-181-scale-acceptance-current-20261011`.
- Current checkpoint: `fa464a23ce5bc8c2b903bfc62d53b71205379812`, tree `70724fb92fec2d3b7fb257d646dd9d76fc7b0c52`.
- Merge parents, in order: `e53624a359c1e73a6ef01e5daf7c2138b693d3a3` and `7945f956b428359928908a9f42e36417b113dafd`.
- Merge commit includes DCO `Signed-off-by: Josh Yorko <joshua.yorko@gmail.com>`.
- Historical worktree `/workspace/work/rcc-swarm/scale-181-acceptance` remains at `a87a67dd65a5415372d2df28c7403047cf4a490a`; it was not reset or edited.

The three-dot diff against current main contains only `anywork/worker_test.go`: 200 insertions, no production changes. `git diff --quiet 7945... HEAD -- . ':(exclude)anywork/worker_test.go'` exited 0. Both intended parents are ancestors of the merge head. The index contains no unmerged paths, `git show --cc --format= HEAD` is empty, `git diff --check 7945...HEAD` exited 0, and the worktree is clean.

## Review boundary

The retained tests cover configured worker scaling, observed concurrent capacity against `Scale`, work queued while prior work is held, and panic recovery across all workers followed by successful full-capacity dispatch and cleared failure state. The prior `continuation-20261011/181/report.md` records known limits: it does not prove Sync registered its internal wait before the second enqueue, and default CPU/cap scaling plus closed-pipeline shutdown remain unproven.

Historical focused/race/shuffle receipts are bound to the earlier test checkpoint, not this merged current-main commit. No Go compile or tests were run on the new merge head; fresh hosted/native verification is still required before these regressions can be accepted for the current head. No push, PR, issue, Actions, or main mutation was performed.

## Verification commands

- `git status --short --branch` — exit 0, clean branch.
- `git show --no-patch --format='%H%n%T%n%P%n%s%n%b' HEAD` — exit 0; head, tree, parent order and DCO above.
- `git diff --name-status 7945...HEAD` — exit 0; only `M anywork/worker_test.go`.
- `git diff --stat 7945...HEAD` — exit 0; `200` insertions, `0` deletions.
- `git diff --check 7945...HEAD` — exit 0.
- `git diff --quiet 7945 HEAD -- . ':(exclude)anywork/worker_test.go'` — exit 0; no non-test difference.
- `git merge-base --is-ancestor e536... HEAD` and same for `7945...` — both exit 0.
- `git ls-files -u` and `git show --cc --format= HEAD` — no unmerged paths or conflict-resolution diff.

One initial combined read-only check exited 1 because it incorrectly expected `git diff --cc HEAD^1 HEAD` to be empty; that command compares the merge tree with a parent and is not an unresolved-conflict check. The corrected unmerged-index and combined-diff checks above were empty. No source or evidence files were changed by that failed check.

## Documentation receipt

- Canonical guidance: no change; no durable guidance gap was found.
- Durable learning: the merge adds only the previously accepted test checkpoint to current main; historical tests need fresh evidence on the appended head.
- Evidence: commit/parent/tree inspection, test-only diff, clean index, and exact validation commands above.
- Stale guidance removed: none.
- Remaining uncertainty: fresh hosted/native test results for this new merge head; existing #181 behavioral gaps listed above.
