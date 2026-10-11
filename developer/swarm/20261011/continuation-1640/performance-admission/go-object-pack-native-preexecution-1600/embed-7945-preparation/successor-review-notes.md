# Successor harness draft for root review only

`harness-successor-draft.go` is an additive proposal derived from the frozen original harness. It has not been gofmt'ed, compiled, or run. The original `harness.go` remains unchanged. Do not treat the current offline `go list -deps` receipt as compilation or performance evidence.

The draft addresses the three identified harness blockers:

1. `requireSameTmpfs` checks Linux tmpfs magic and device identity for the supplied pack file and provider destination, before provider copy and again after copy. The external runner must stage a verified pack copy and provider root as siblings under the same newly owned `/dev/shm` directory. The original overlay pack remains immutable.
2. `/proc/self/io` parsing requires exactly one valid `rchar`, `read_bytes`, and `syscr` field; read/parse/missing-field errors fail the run. Post-pass RSS errors also fail rather than becoming zero.
3. An exclusive JSONL receipt is opened before timed passes. It is `Sync`'d after `run_start`, every completed pass, any pass failure record, and `run_complete`; stderr emits `PASS_START` / `PASS_DONE` markers for an external per-pass 30 s watchdog. Completed passes survive a later failure.

The draft additionally hashes every selected pack slice untimed before the timed sequence and records the 4,649-slice / 4,484,936-byte identity receipt. Timed passes still verify payloads. The existing schedule remains 3AB/2BA partial balancing, not a full alternating balance. CPU counters include in-process verification and observer overhead; RSS is a post-pass sample, not a peak. These limits need to be carried into the eventual plan and any report.

This is a code review draft only. A separate root admission is still required for any compile or timed run. The fixture was not read in this preparation lane.
