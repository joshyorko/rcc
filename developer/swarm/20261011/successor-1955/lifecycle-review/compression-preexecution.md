# Compression harness pre-execution review

Read-only review of frozen source SHA-256 `2d1ae24dd6aa2a4c7347b7aa30fb27256c990cd3ff2cae8d8f46b303e19d2dc0` and controller SHA-256 `a47f743816e895c2bcad9a31318557a250a1ea6fdb5b2ae012bbf1dbb35e1049`. No build or execution performed by reviewer. Build-1 evidence present in `compression-e2e/build-1/result.json` (reported successful by coordinator).

## Decision

**Conditional GO; the results-root prerequisite is now satisfied by controller SHA-256 `3b6d3e1467e4495ee5c0a6bc4f0bbae73258ffa819c0b66cf38ec579ad5c374f`. It creates results mode 0700 and checks ownership/mode before writes and in the completion record. The retained run review is in `compression-e2e/independent-runtime-review.md`. No other reviewed condition blocked the scoped filesystem-provider experiment.

## Checks and scope

- Source SHA remained exact. The normal cold path and warm path both call public `Acquire` and `ExecuteWithStreams`; the warm path verifies local materialization reuse, zero provider calls, matching output/count/digest, and lease removal. Timing resets immediately before warm execution (line 619); `WarmPipelineNS = WarmNS + WarmExecuteNS` does not double-count. I withdraw my earlier timing concern.
- Controller uses only filesystem-provider worker arms, six counterbalanced runs (three pairs, alternating levels), fresh worker/consumer homes and fresh CAS per arm. Both fixtures use the same logical files/workload/expected output; real Python is selected. Guards check owned exact-mode roots before and after bounded native subprocesses; global diversion is 0750. No persistent-worker or Actions/full-cold claim is warranted; OS cache is uncontrolled.
- Source contains bounded pack/index reads, no-follow regular-file checks, identity/hash and standard-index binding checks, plus negative controls, but this controller does not run packed-provider arms; do not use this experiment as pack acceptance evidence.
- Controller preserves per-command stdout/stderr and bounded-runner evidence and fails on nonzero subprocess results. Profiles/pprof are collected after native runs, outside the 30-second wrapper; this is post-run analysis, not part of timed measurements.
- The controller does not explicitly assert fixture receipt `gzipLevel` equals requested arm or that prepared pack/fixture bytes differ by compression level. The flag plumbing is present; record and compare `gzipLevel`, `packBytes` and/or pack hash before interpreting this as a compression-effect result.

## Remaining limitation

Even after the results-root fix, this is a private diagnostic comparison with overlays and filesystem-provider workers, not shipping-binary, cold-OS-cache, packed-provider, or persistent-worker evidence. Preserve the original strict-700 admission failure separately.
