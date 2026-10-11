# #235 current-head focused race gate: additive attempt 02

## Outcome

This single admitted attempt is recorded as **FAIL at the supervisor gate**, so it is not a passing current-head race receipt. Go printed `ok github.com/joshyorko/rcc/anywork 2.019s` and the child command was recorded with numeric exit `0`. However, the external guard reached its 120-second deadline while it still considered the Go process group nonempty and returned outer exit `1` with `failure="120-second command watchdog exceeded"`. After the runner exited, a read-only process-group check found no active processes. The exact reason the supervisor did not observe quiescence before the deadline is unresolved; the attempt must not be silently recategorized as PASS. No retry was run.

## Source, toolchain, command

- Current worktree head/tree: `fa464a23ce5bc8c2b903bfc62d53b71205379812` / `70724fb92fec2d3b7fb257d646dd9d76fc7b0c52`; parents are e53624a359c1e73a6ef01e5daf7c2138b693d3a3 and 7945f956b428359928908a9f42e36417b113dafd.
- The exact 200-line worker test patch SHA is the same from base 727 to e536 and from base 727 to current: `fa3e8abdccd07383f60e54be21e5ea69d52874b6306b666f0304320e03d17dc3`.
- Official Go: `/workspace/work/rcc-swarm/tools/go1.26.9/bin/go`, SHA `e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9`, version `go1.26.9 linux/amd64`.
- Compiler: `/usr/bin/gcc`, `gcc (Debian 14.2.0-19) 14.2.0`; `CGO_ENABLED=1`, `GOTOOLCHAIN=local`, `GOPROXY=off`.
- Command: `/workspace/work/rcc-swarm/tools/go1.26.9/bin/go test -race -p 2 ./anywork -count=1 -run '^(TestAutoScaleGrowsToConfiguredWorkerCount|TestScaleMatchesConcurrentWorkerCapacity|TestSyncDrainsTasksQueuedWhilePriorWorkIsHeld|TestWorkerPoolSurvivesPanicsFromEveryWorker)$'`.

## Guard values

- Go child exit: `0`; outer runner exit: `1`; result status: `FAIL`, reason: `120-second command watchdog exceeded`.
- Workspace free minimum: `3,044,864,000 B` against floor `2,688,548,864 B`.
- Sampled process-tree RSS peak: `83,177,472 B` against `536,870,912 B`.
- Reused owned RAM GOCACHE peak: `64,888,832 B` against `268,435,456 B`.
- Attempt02 workspace GOTMPDIR peak: `4,530,176 B` against `201,326,592 B`; final allocation `4,096 B`.
- Time cap: `120s`; observed `120.007637425s`.
- After process completion, active process count in PGID 26615 was zero. The empty owned GOTMPDIR was moved intact to `/dev/shm/rcc181-235-attempt02-gotmp` before another guarded runtime; its mode and mtime match, and it was not executed from RAM.

## Preserved failed attempt 01

The original 64 MiB cap failure and all raw files remain under `p1-235-current-focused-tests/`. Its 750-entry GOTMPDIR tree was moved to `/dev/shm/rcc181-235-failed-gotmp-083fe2e8657a`. The final inventory check confirms the same path set, all 620 file/symlink content hashes, sizes, modes and mtimes, and all 130 directory modes and mtimes. Filesystem-specific directory sizes/blocks differ across workspace and tmpfs, as expected. The prior 64.9 MiB GOCACHE remains at `/dev/shm/rcc181-235-go-cache-083fe2e8657a` and was reused by attempt02; no shared cache was changed.

## Receipt hashes

- Frozen attempt02 plan: `plan.json`, SHA `4e96fe193b608dcdeb55e61312dc5ba8baa54143f6ada90aee20517e2ac607bd`.
- Guarded runner: `guarded_test.py`, SHA `add4e5664d9afd0864bf1cd40c435a60d105faceafb8a6c36cdaba09a3b2d0a6`.
- Command log: `go-test.log`, SHA `e91b909f0aeacc5fd6fc9c2a10d5203b34b3b3f08bed4fee980a28490515f257`.
- Resource/result receipt: `command-result.json`, SHA `423e7d390f65be80b9a27f5fa0bb53cb0dc51850f67bf63693a5742b04219f5f`.
- Prior-tree preservation verification: `failed-gotmp-verification-final.json`, SHA `451559c521fc5eee27f0f46c06f831afda05ae5249855136420d109d20a49fec`.
- Attempt02 empty-temp preservation receipt: `attempt02-gotmp-preservation.json`, SHA `5e38ee4f00fce8d0b3ea3f5b64170b9f3901b31ef6ab16e410aacc7ec90b894a`.

The worktree is clean and no source files changed. There is no current-head acceptance claim from this local gate; root must decide any further action after reviewing why the supervisor timed out despite the Go child result and package `ok` output.
