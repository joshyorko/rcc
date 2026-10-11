# #235 current-head focused race gate

The one admitted focused race-test invocation did **not** complete. The external guard stopped it after 9.054 seconds because the owned workspace `GOTMPDIR` grew beyond its 64 MiB cap during Go race compilation. It sent SIGTERM; the Go command's numeric exit was `-15`, and the guard wrapper exited `1`. `go-test.log` is empty, so no test pass/fail result was produced. Do not count this as a race-test result and do not retry without root admission and a newly reviewed resource plan.

## Exact source and tool

- Head: `fa464a23ce5bc8c2b903bfc62d53b71205379812`, tree `70724fb92fec2d3b7fb257d646dd9d76fc7b0c52`.
- Parents: `e53624a359c1e73a6ef01e5daf7c2138b693d3a3`, `7945f956b428359928908a9f42e36417b113dafd`.
- The `anywork/worker_test.go` patch SHA256 is identical from base 727 to accepted checkpoint e536 and to current head: `fa3e8abdccd07383f60e54be21e5ea69d52874b6306b666f0304320e03d17dc3`.
- Worktree remained clean at the same head; no source files were modified.
- Official Go 1.26.9 path `/workspace/work/rcc-swarm/tools/go1.26.9/bin/go`, SHA256 `e90c4e12df236ea0c2e7536fd68da8a47fc0b585c19588bf6c1a70cfc296a0f9`; `go version go1.26.9 linux/amd64`.
- `CC=/usr/bin/gcc`, GCC 14.2.0; `CGO_ENABLED=1`, `GOTOOLCHAIN=local`, `GOPROXY=off`.
- Exact attempted command: `/workspace/work/rcc-swarm/tools/go1.26.9/bin/go test -race ./anywork -count=1 -run '^(TestAutoScaleGrowsToConfiguredWorkerCount|TestScaleMatchesConcurrentWorkerCapacity|TestSyncDrainsTasksQueuedWhilePriorWorkIsHeld|TestWorkerPoolSurvivesPanicsFromEveryWorker)$'`.

## Numeric result and resources

- Outer tool result: exit `1`, elapsed `8.989870153` seconds; captured stdout was `{"elapsed_seconds": 9.053845679000005, "failure": "own GOTMPDIR cap breached: 68059136", "gocache_allocated_peak_bytes": 64884736, "numeric_exit_code": -15, "process_tree_sampled_rss_peak_bytes": 261578752, "scratch_allocated_peak_bytes": 68059136, "status": "FAIL", "workspace_free_bytes_min": 2989080576}`.
- GOTMPDIR cap `67,108,864 B`; observed peak/final `68,059,136 B`.
- GOCACHE cap `268,435,456 B`; observed peak/final `64,884,736 B`.
- Tree RSS cap `536,870,912 B`; observed sampled peak `261,578,752 B`.
- Workspace free floor `2,688,548,864 B`; observed minimum `2,989,080,576 B`.
- Command time limit `120s`; observed `9.054s`.
- Process group was confirmed empty afterward; no retry or cleanup was performed.

## Receipts

- Runner: `guarded_test.py`, SHA256 `f0610eeef67148aafdf247a3fd512274ea951bdf40c3aa0cfe3555ce7703eadb`.
- Empty command log: `go-test.log`, SHA256 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.
- Resource/command receipt: `command-result.json`, SHA256 `51f46064429bd85023d2ece4fa031d1fb24c0e1151914d1876d8821ec093964d`.
- Owned cache and temp directories remain preserved under `/dev/shm/rcc181-235-go-cache-083fe2e8657a` and `gotmp-083fe2e8657a`; no unowned cache was modified.

No #235 current-head race acceptance is established by this attempt. Fresh hosted tests remain useful, but this local lane only establishes that the current bounded local scratch limit is insufficient for this cold race compile.
