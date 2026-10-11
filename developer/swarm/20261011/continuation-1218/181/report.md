# Issue #181 continuation review

## Source and exact diff

Frozen main is `727c1ff8b679cdbbe12836e53c87734a1edb0071` (tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`). The existing issue checkpoint is `3e57fde849398ea69954b4c09d5dad9ca0111361` (tree `76a02d87b2e20543e9b7991fe0db04b9ad2e20f1`). The owned local branch at `a87a67dd65a5415372d2df28c7403047cf4a490a` has the same tree and is clean.

The issue branch is based at `07db6221a3540d227f2106eccf69fe0e7c11fee0`. The three-dot diff against frozen main contains only `anywork/worker_test.go` (+200 lines). Frozen main has no `anywork/` changes after the merge base, so this test addition does not duplicate #226 or change worker behavior. `git merge-tree --write-tree 727 3e57` completed cleanly and produced candidate tree `ac1044edc1ede2a6fea6354eabf686af963bd4fa`.

## Acceptance and verification

Live issue #181 asks for deterministic tests for queue scaling, backlog, synchronization, worker processing, and panic/error cleanup, specifically avoiding timing sleeps in favor of controlled channels and completion signals. The checkpoint adds direct configured AutoScale growth, a channel-held observed-capacity assertion, queue drain after held work, and all-worker panic recovery followed by full-capacity successful execution and error-count reset. It contains no production logic.

The exact test-file bytes from `3e57fde` were exercised with a Go overlay; `anywork/` production files matched frozen main. Official Go 1.26.9 with GCC passed focused coverage (`91.2%`), race, and 20 shuffled runs, all exit 0. The owned exact-tree run also passed all three checks. Full command and toolchain receipts are in [verification.log](verification.log). The root-reviewed earlier evidence remains at `/workspace/work/rcc-swarm/evidence/issue181-scale-20261011/report.md`.

## Integration disposition

This branch is independently mergeable after release PR #233: it touches only the test file, no `anywork/` code changed on main since its base, and the merge-tree check is conflict-free. It is not included in frozen main 727 and should remain outside the release candidate. Preserve the existing branch/commit; root can publish or integrate after release sequencing permits it.

The issue should stay open. Remaining gaps are default CPU/cap AutoScale branches, a deterministic signal proving Sync entered its internal wait, shutdown/closed-pipeline lifecycle, and concurrent mutation of global WorkerCount/AutoScale. The Sync test explicitly does not claim wait-entry ordering. Existing package statement coverage is not evidence that these behaviors are all covered.

## Documentation receipt

- Canonical guidance: no change.
- Durable learning: bounded channel-controlled tests provide an independent concurrency-capacity oracle; avoid claiming Sync wait-entry without an observable signal.
- Evidence: live #181 body, exact branch/main comparison, clean merge-tree result, and focused/race/shuffled Go receipts.
- Stale guidance removed: none.
- Remaining uncertainty: the acceptance gaps listed above.
