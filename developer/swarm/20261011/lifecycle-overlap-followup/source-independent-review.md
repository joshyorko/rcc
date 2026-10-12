# Close-error and lease-retention gate: independent source review

**Decision: GO for the four bounded build/control phases only, with the phase ordering and negative-proof gate encoded in the accompanying admission.** This is a private Linux injected-error policy control, not the same-process A/B overlap experiment and not a timing workload. No build or native execution was performed during this review.

Reviewed inputs and source identities:

- Source binding `source-binding.json`: SHA-256 `2a9bcafbd9e1e664aded2e461e4801808551d1f6a2d117be71d4012255b9068c`; read-only verification checked all 542 listed files (62,153,850 bytes), with zero missing/size/hash mismatches.
- Harness `source/main.go`: `50e3b1ca47cd53e1add883b669d7c9f5a74e76663d4db3e49a9106c0c1aaa0a7`.
- Private fail-closed Execute overlay: `888345d4b949634cfbea4229b132474aea7ed570516db06cea23fb9444d18f49`.
- Private injected group-close fault overlay: `c2e9fc52c9016b86f4f06a18e077471d01ddb225c6cc2cf292b8266069331f7a`.
- Controller: `69d07fa1a70ae74d7db155d34d53dba1a271fcc1df5eca38fb461075d845ca6e`.

The negative overlay retains frozen Execute behavior and injects a close error only after the private supervisor has observed an empty process group. The harness expects context cancellation, confirms the injected error was suppressed, observes the canonical execution lease already released, verifies both recorded PID/start identities are absent (including zombies), and labels the result `UNSAFE_BASELINE_OBSERVED`. This is deliberately a policy regression control: because the injected failure comes after an empty-group census, it does not claim a child remained alive when the baseline released the lease.

The fixed overlay joins supervisor-close errors with the original cancellation error and sets `releaseAllowed=false` before the lease-release defer. On the expected injected close error, the harness requires both errors to be observable, the exact handle-bound lease to remain present and canonical, and both recorded process identities to be absent. It fsyncs a `RETAINED_LEASE_BEFORE_RECOVERY` receipt before invoking the normal RCC `Release` API with the exact decoded lease snapshot; it then requires the lease path and all lease records to be absent. It does not unlink lease files directly or use outer fallback signaling to manufacture success. This addresses the source-level defer bug identified in the prior review.

The controller exposes exactly four phases. `build-fixed` and `run-fixed` require the retained `results-negative/readback.json` to prove the expected baseline outcome; binaries must have exact source-binding receipts; each build/run uses a fresh output path and the bounded v4 outer runner. The run is limited to cancel-control, normal public `Acquire`, one exact retained artifact, and the 25-second native / 30-second outer bounds. Guards, materialization readback, immutable input/provider hashes, receipt barriers, and zero final leases are checked. No timed workload is available through this harness: its CLI rejects modes other than `cancel-control`.

Admission limits: this proves only injected close-error propagation and safe lease retention/recovery for one cooperative Linux group. It does not establish concurrent A/B `ExecuteWithStreams` isolation, active-descendant retention on a genuine reaping failure, hostile code containment, process-session escape handling, long-lived worker behavior, or macOS/Windows behavior. A separate independent review and admission must precede any subsequent overlap control. Production RCC source, format, and defaults remain unchanged.
