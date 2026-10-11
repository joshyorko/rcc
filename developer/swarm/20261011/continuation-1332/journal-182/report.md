# Issue #182 focused journal coverage checkpoint

Issue #182 remains open. Its live request is deterministic coverage for journal creation/opening, persistence and reload, malformed/unavailable storage, and lifecycle/error branches, followed by a reproducible repository-wide coverage run after embedded assets are generated. The historical issue percentage (11.3%) was stale; accepted main 47a/c49 assessment recorded 11.6% focused package coverage. C49 made no changes under `journal/`.

The test-only branch is `codex/rcc-182-journal-tests-20261011`, exact HEAD `36a84430083b3af6f9758408b94054e5197b7f8b`, tree `2fa0167aa679a4d60c7a5c0002801dc4bdb94ff7`. It was constructed from a DCO-signed test commit on 47a (`2ab311be5cb8b1e19e9af42dd4bffe6ce51146d0`) followed by a no-fast-forward merge of exact accepted main 727 (`727c1ff8b679cdbbe12836e53c87734a1edb0071`). The resulting diff from 727 is only `journal/coverage_test.go` (+114 lines). Worktree is clean.

The new tests exercise (1) `ForRun` routing through `common.RunJournal`, persisted event normalization, and `StopRunJournal` disabling subsequent writes; (2) the `WarrantyVoided` append no-op; (3) missing-file error handling in `Events`; and (4) a representative build-event lifecycle persisted by `BuildEventStats`, loaded by `Stats`, and filtered by `MakeStatistics`. Tests restore global state and do not run in parallel.

Focused verification passed on the exact HEAD using the official Go 1.26.9 toolchain:

```sh
CC=/usr/bin/gcc /workspace/work/rcc-swarm/tools/go1.26.9/bin/go test -p 1 -count=1 -cover ./journal
```

Result: exit 0, `ok github.com/joshyorko/rcc/journal 0.004s coverage: 82.5% of statements`. The resource receipt records 1.466 seconds wall time, 77,058,048-byte process-tree RSS peak, 9,293,824-byte owned `/dev/shm` temp peak, 5,922,816-byte GOCACHE growth, and filesystem free space 2,713,186,304 bytes before to 2,707,263,488 after; all stayed within the admitted limits. The unique temp directory was removed after its log was copied. Raw command output and observations are in `go-test-2bb800f7cb00b0fb.log` and `resource-2bb800f7cb00b0fb.json`.

This does not establish full issue completion. The repository-wide coverage workflow and hosted run after embedded-asset generation remain unverified, along with platform and race acceptance. No PR was opened, no issue was changed or closed, and no release candidate was modified.

Documentation receipt
- Canonical guidance: no change; test-only work does not reveal stale repository guidance.
- Durable learning: the public run-journal lifecycle routes through `common.RunJournal`; package coverage also includes build-event persistence and aggregate stats, not only event-log append/read.
- Evidence: exact-head focused Go coverage command and resource receipts named above.
- Stale guidance removed: none.
- Remaining uncertainty: repository-wide/hosted coverage and broader journal package acceptance remain outstanding.


Receipt erratum (2026-10-11)
- The original resource receipt is preserved unchanged (SHA-256: `854d3590756ca7381d7848a7436b0ec83eabcbb9bcca27cb0b20404f84caf84e`). Its `owned_ram_tmp_peak_allocated_bytes` field records the final sample (4,096 bytes), not the peak. Recomputing the maximum from its preserved samples gives 9,293,824 bytes, below the 134,217,728-byte limit; corrected guard status remains PASS.
- The 77,058,048-byte RSS figure is the maximum observed in periodic `ps`-based samples, not an absolute transient peak. The sampled maximum is below the 536,870,912-byte limit.
- See `resource-2bb800f7cb00b0fb.erratum.json` for the machine-readable correction. No rerun was performed; the original receipt and runner script were not overwritten.
