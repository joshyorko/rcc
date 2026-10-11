# Issue #182 branch readiness at main 3fe6e20

## Disposition

The exact journal test branch is a unique, narrowly scoped, content-applicable test-only candidate against current default branch `main` at `3fe6e200c0acc06d5920544a3bd5c3c036096855` (tree `a40d30733b5a80abcfa05d3220403afa653d5665`). It is not duplicated by current main, and the compare API shows no changed file beyond `journal/coverage_test.go`. Its merge base is the accepted main commit `727c1ff8b679cdbbe12836e53c87734a1edb0071`; branch is 20 commits behind main and 2 ahead by commit topology, but the current-main diff remains 114 additive lines and zero deletions. This read-only review does not claim a server-side mergeability result: there is no PR, so GitHub has not calculated a PR mergeability state.

Recommend admitting it as a post-release, partial #182 testing checkpoint after standard current-main PR checks pass. Do not close #182 from the 82.5% package number. The issue explicitly requests a reproducible repository-wide coverage run after embedded asset generation, and that run remains unverified.

## Live source and issue state

- Repository default branch: `main`; current head `3fe6e200c0acc06d5920544a3bd5c3c036096855`, tree `a40d30733b5a80abcfa05d3220403afa653d5665`.
- Issue #182 is still open: “[quality] Increase journal package coverage beyond 11.3%.” Live body asks for deterministic tests of journal creation/opening, persistence/reload, malformed/unavailable storage, lifecycle/error branches, followed by a reproducible repository-wide coverage run after embedded assets are generated. Live update timestamp was 2026-10-11 13:23:03Z; current live issue comments include one maintainer update.
- The latest issue comment (6109478587) identifies source `36a84430083b3af6f9758408b94054e5197b7f8b`, tree `2fa0167aa679a4d60c7a5c0002801dc4bdb94ff7`, accepted `727` ancestor, focused result 82.5%, and explicitly says no PR/main merge yet, repository-wide/hosted acceptance outstanding, issue open.
- The referenced branch `codex/rcc-182-journal-tests-20261011` exists and its live ref points to exact head `36a84430083b3af6f9758408b94054e5197b7f8b`.
- PR searches by exact head branch and source SHA returned no PR. Current combined-status queries returned no statuses for either head `36a...` or current main `3fe...`; public Actions run query for head `36a...` returned zero runs. These are observations of current CI metadata, not proof that no test was ever run.

## Exact diff and overlap

GitHub's `compare` API for current main `3fe6e200c0acc06d5920544a3bd5c3c036096855` against branch head `36a84430083b3af6f9758408b94054e5197b7f8b` reports `status=diverged`, `ahead_by=2`, `behind_by=20`, merge base `727c1ff8b679cdbbe12836e53c87734a1edb0071`, and exactly one changed file: `journal/coverage_test.go`, 114 additions and 0 deletions. Its patch adds `encoding/json` plus four tests; current main already contains the prior three tests for append/reload, malformed event lines, and append-open failure. The exact current-main and branch `journal/journal.go` blobs match (`91cefc998adc14f55e769d7f8401a632b3ce8bb1`); `common/journal.go` blobs match (`964e3832ca7ad205083baf707153916c0e556c7c`). Compare reports no production-file delta. The test file at main has blob `f2c86f0ab7a293515f7866f0fa61ae25c8f69b65`; the branch's changed blob is `108fe7541807ff8f40bea978547741df4bba7b8b`.

The added tests exercise:

- `ForRun`/`common.RunJournal` persistence, normalization, and `StopRunJournal` suppressing a subsequent write.
- `WarrantyVoided` making `AppendJournal` a no-op without creating a file.
- `Events` reporting a wrapped error when the journal file is missing.
- Build-event persistence through `BuildEventStats`, JSON reload through `Stats`, and robot filtering in `MakeStatistics`.

The tests use isolated `t.TempDir` product homes; they do not call `t.Parallel`. Global controller/space/build-event state is restored with `t.Cleanup`; the run-journal test also registers `StopRunJournal` cleanup. These additions fill real lifecycle/statistics gaps while preserving the existing malformed-line and append/reload cases. No production source changed.

## Verified prior test evidence

I did not rerun or compile. The source-bound raw log and root verification receipt record this exact command on exact source head `36a...`:

```sh
CC=/usr/bin/gcc /workspace/work/rcc-swarm/tools/go1.26.9/bin/go test -p 1 -count=1 -cover ./journal
```

Result: exit 0; `ok github.com/joshyorko/rcc/journal 0.004s coverage: 82.5% of statements`. The previously accepted current-main focused baseline was 11.6% at `47a60ea...`; the issue's 11.3% is historical and stale.

Raw evidence under `evidence/continuation-20261011/journal-182/`:

- `go-test-2bb800f7cb00b0fb.log`, SHA256 `26745359d1891f50da4ad85416ceda691ea9b68fa6ad09c05b92a5c496cfd45d`.
- `root-verification.json`, SHA256 `fad6d6e7127c02e7adbd357f08dd0d5803d30cdc099112752176a0ed488ea68b`.
- `resource-2bb800f7cb00b0fb.json`, SHA256 `854d3590756ca7381d7848a7436b0ec83eabcbb9bcca27cb0b20404f84caf84e`.
- `resource-2bb800f7cb00b0fb.erratum.json`, SHA256 `465ea9cda140a4533d9903549e6e30d5d07adaf1f1c088d351bdf32c207857bc`.
- `report.md`, SHA256 `f860790169c502ad1fc616d6d7d07d74e4d166d53154d14bf3947be4fa922f8b`.

The resource erratum corrects the original temp-peak field to the maximum observed sample, 9,293,824 bytes (within 128 MiB). RSS was 77,058,048 bytes by periodic `ps` sampling and is not an absolute transient peak. Observed temp and RSS stayed within recorded limits; no rerun was performed.

## Remaining acceptance gates and proposed reviewer disposition

Before merge, require the normal checks on the actual PR head against current `main`, including the ordinary Go test/build/quality checks that the repository applies. Because the branch is behind current main, checks must bind to the PR head that includes current-main changes, not only the old `36a...` commit. For issue completion, still run and preserve a reproducible repository-wide coverage result after embedded assets are generated (with its exact source SHA and report). Hosted/native or race gates should follow the repository's then-current required branch policy; no PR checks currently exist to confirm those requirements from GitHub metadata.

Suggested disposition: accept this as an independent test-only improvement once current-head CI is green; keep #182 open until the repository-wide coverage requirement is satisfied. No issue closure based only on the journal package's 82.5% result.
