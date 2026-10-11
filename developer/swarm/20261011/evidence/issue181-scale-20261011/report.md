# Issue #181 focused scale and queue acceptance

Issue: https://github.com/joshyorko/rcc/issues/181

## Scope and source

- Base: main commit `07db6221a3540d227f2106eccf69fe0e7c11fee0` (merge of accepted #226 worker initialization/limit fix).
- Final commit: `a87a67dd65a5415372d2df28c7403047cf4a490a`.
- Final tree: `76a02d87b2e20543e9b7991fe0db04b9ad2e20f1`.
- Changed file: `anywork/worker_test.go` only; no production changes.
- Classification: Maintenance (test-only).

The pre-change focused package test passed with 83.8% coverage. `Scale`, `Backlog`, `Sync`, `process`, and `OnErrPanicCloseAll` were already 100% statement-covered through existing tests and indirect callers; the remaining gap was behavioral observability. `AutoScale` was 0% in the parent test process because the existing worker-limit test uses a subprocess, whose coverage is not merged into its parent.

## Added acceptance

- `TestAutoScaleGrowsToConfiguredWorkerCount` exercises `AutoScale` directly on the initialized two-worker pool with `WorkerCount=3`, then verifies `Scale()` reports three.
- `TestScaleMatchesConcurrentWorkerCapacity` holds channel-controlled tasks and asserts the observed maximum number of simultaneous workers equals `Scale()`, then releases all work and verifies `Sync` drains it.
- `TestSyncDrainsTasksQueuedWhilePriorWorkIsHeld` launches a `Sync` goroutine while initial work is held, queues a second held task before releasing the first, and confirms both tasks are eventually drained. The signal in this test precedes the `Sync()` call; it does **not** prove that `Sync` registered its internal wait before the second enqueue. The API exposes no deterministic wait-entry signal, so that interleaving remains unproven.
- `TestWorkerPoolSurvivesPanicsFromEveryWorker` runs in a deadline-bounded subprocess. It holds exactly `Scale()` panic tasks until all workers have started, releases them, and requires `Sync` to report exactly `Scale()` failures. It then holds `Scale()` successful tasks until that many workers start again, releases them, and verifies all complete and the next `Sync` reports no stale failure.

Existing tests continue to cover nil backlog entries, basic completion, single-panic reporting, and non-nil closer cleanup.

## Verification

Final checks used `/home/agent/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go`, confirmed as `go version go1.26.9 linux/amd64`, with `GOROOT=/home/agent/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64`, `GOTOOLCHAIN=auto`, `CC=/usr/bin/gcc`, `CGO_ENABLED=1`, and `GOCACHE=/workspace/work/rcc-swarm/evidence/resume-acceptance/release-proposal/gocache`. Exact baseline/final executable paths, outputs, and numeric exits are in `verification.log` alongside this report.

- Base baseline, before edits: `go test ./anywork -count=1 -cover` — exit 0, 83.8% coverage. It was run through the contained Go 1.26.5 driver with `GOTOOLCHAIN=auto`; `go env GOVERSION` and `GOROOT` in the same environment confirmed selected Go 1.26.9.
- Final exact-head unit/coverage command: `/home/agent/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go test ./anywork -count=1 -coverprofile=/tmp/rcc181-scale.cover` with the environment above — exit 0, `coverage: 91.2% of statements`. `go tool cover -func` reports `AutoScale` 75% (configured count branch exercised; default CPU/cap branches remain partly uncovered), and Scale/Backlog/Sync/process/OnErrPanicCloseAll at 100%.
- Final exact-head race command: same environment with `/home/agent/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go test -race ./anywork -count=1` — exit 0, `ok github.com/joshyorko/rcc/anywork 3.030s`.
- Final exact-head ordering command: same environment with `/home/agent/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go test ./anywork -count=20 -shuffle=on` — exit 0, `ok github.com/joshyorko/rcc/anywork 0.060s`.
- `gofmt` and `git diff --check` passed.

No full repository tests or nested RCC/toolkit runs were performed; this lane was limited to the affected package and race acceptance.

## Acceptance status and remaining gaps

Covered: direct configured scaling; observable agreement between `Scale` and running worker capacity; eventual draining of work queued while earlier work is held; panic handling on every reported worker followed by full-capacity task execution and error-count reset. Existing direct cleanup tests remain in place.

Not proven: that the second enqueue occurs after `Sync` has registered its internal wait. Remaining coverage gaps include default CPU-derived/cap branches of `AutoScale` and the closed-pipeline worker exit path (the package exposes no shutdown operation). Concurrent writes to global `WorkerCount`/`AutoScale` are not tested. This is focused evidence for #181, not a claim that every anywork lifecycle branch is covered.

## Documentation receipt

- Canonical guidance: no change.
- Durable learning: deterministic worker tests can use reported capacity with channel-held tasks as an independent runtime oracle; no sleeps are needed, and internal wait-registration claims require an observable synchronization point.
- Evidence: exact-base focused coverage and final exact-head unit/race/shuffle commands above.
- Stale guidance removed: none.
- Remaining uncertainty: deterministic observation of `Sync` wait registration, default CPU/cap scaling branches, and shutdown lifecycle.
