# Cleanup-error propagation before overlapping execution

This ONE bounded follow-up continued the same owned RCC successor from **5fb5c8595fad597a036dcc908745c2c9d5fc91bd**. The parent checkpoint was remotely verified again:30 files/233,441 bytes matched, all3,752 preceding entries preserved. Repository `joshyorko/rcc`, ID1051101627, and current main7d17122436584dcbfe9b8afddeb56d7c81a10af7 were refreshed. Existing worker names were reused, with no new root/task, replacement worker or wider fan-out.

## Selected gate and actual result

The intended next control was two overlapping executions in one Go process: cancelA, prove its descendants reaped and lease released whileB remains live with its lease unchanged, then proveB responds and cleans up. Independent source review identified a prerequisite. The retained private Linux group supervisor can fail its cleanup census/drain, but frozen `execute.go` only exposes `supervisor.close()` errors when no earlier error exists. Cancellation already sets an error, so the cleanup error is hidden and the lease-release defer still runs. The overlap control remained **NOTRUN** while this prerequisite was tested.

Two NEW untimed controls exercised normal public `Acquire` and `ExecuteWithStreams` with a fresh consumer CAS each time. Both reused the exact retained gzip1 fixture and unchanged cancellation driver. Fault injection returned a cleanup error only after the actual group census was empty. This deliberately tests cleanup-error propagation and lease-retention policy without leaving a live child behind. It is not evidence that a live-descendant timeout was handled, or a production shipping-binary failure.

| Control | Actual outcome |
| --- | --- |
| Frozen Execute implementation plus private group supervisor and injected close failure | `UNSAFE_BASELINE_OBSERVED`: context cancellation was detected, the injected cleanup error was hidden and the lease was automatically released. |
| Private Execute overlay with the same fault injection | `PASS_PRIVATE_FAIL_CLOSED`: cancellation and cleanup errors were joined; automatic lease release was blocked; the exact owned lease remained until explicitly recorded recovery. |

The private overlay adds a per-invocation release guard and combines errors with `errors.Join`. It is stored only as an overlay in this evidence prefix. No production RCC source, format, materializer default or release artifact changed.

The fixed control durably wrote `RETAINED_LEASE_BEFORE_RECOVERY` with `leasesReleased=false`, `closeFailurePropagated=true`, `leaseRetainedBeforeRecovery=true`, `recoveryReleased=false`. Only after both recorded PID/start identities were absent, including zombies, did the harness call ordinary RCC `Release` using the exact retained lease snapshot. Its final row records recovery and zero remaining leases. That recovery is distinct from automatic release and does not erase the barrier receipt. No manual file deletion or fallback process signal occurred.

Negative parent20028/start1539462 and child20029/start1539485, and fixed parent20139/start1543349 and child20140/start1543373, were all absent in the coordinator's actual postcontrol `/proc` readback. Both controls had complete outer return0/stopnull/no final observed live identities, identical strict private0700 owner/mode guards and513 materialized regular files with correct sizes/SHA256. Together there were1026 file checks, zero mismatches and zero final leases. All542 build/runtime source bindings and681 frozen input files/modes remained unchanged; the historical failed private-home0750 remains unrepaired.

Raw [negative worker](results-negative/worker.jsonl), [fixed worker including pre-recovery barrier](results-fixed/worker.jsonl), [coordinator integrity readback](coordinator-integrity-readback.json), and [independent runtime review](runtime-independent-review.md) are retained. The raw control receipts contain clocks/resource fields for bounded execution accounting; they are not performance samples.

The transient retained lease bytes/hash were not saved. Their validation and reuse are supported by the exact source-bound harness and successful ordinary RCC Release, plus the durable pre-recovery state flags. This limits independent reconstruction of that instant. A future overlap control must retain the raw A/B lease snapshots and B file identity across cancellation.

## Exact identities and verification

| Artifact | SHA-256 |
| --- | --- |
| 542-file source/build/runtime binding | `2a9bcafbd9e1e664aded2e461e4801808551d1f6a2d117be71d4012255b9068c` |
| New private control harness | `50e3b1ca47cd53e1add883b669d7c9f5a74e76663d4db3e49a9106c0c1aaa0a7` |
| Injected private group-close overlay | `c2e9fc52c9016b86f4f06a18e077471d01ddb225c6cc2cf292b8266069331f7a` |
| Private fail-closed Execute overlay | `888345d4b949634cfbea4229b132474aea7ed570516db06cea23fb9444d18f49` |
| Negative private binary | `178ff4098a6f5914ac087617673674782390d914900ab7529892d32f35b5e234` |
| Fixed private binary | `f479d38a4c7e3f8fb671d81b1bcb8b424d7a21ce69dd566df361b5fe4a54614d` |

Both binaries were newly built with selected Go1.26.9 from frozen source7945f956b428359928908a9f42e36417b113dafd plus the explicitly bound private home/global-path and group/Execute overlays. The production shipping binary remains5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21. Binary outputs reside in the owned tool directory; exact source, overlay, build commands, receipts and binary hashes are committed for review. They are not release artifacts.

The independently reviewed controller permitted only `build-negative`, `run-negative`, `build-fixed`, `run-fixed`. Fixed phases required the actual unsafe negative receipt first. A first admission schema mismatch stopped before any build/native execution; the original admission bytes and failure were preserved, the independent reviewer added the required hash field, and the build then ran. No accepted control was replayed. Source review, admission, repaired schema receipt and every build/outer result are retained.

Bounds were build60s, native context25s, outer30s, GOMAXPROCS2/GOMEMLIMIT256MiB, with retained sampledv4 limits of2GiB process-tree RSS,128MiB logical scratch,8MiB output and2.5GiB free-space floor. These are sampled observations, not kernel quotas or absolute peaks. The coordinator owned all builds and native execution. Reused `capability_integrity` performed source/admission review; reused `actions_admission_refresh` performed owner refresh and independent raw review. Historical configuration is LunaHigh for these workers and requested GPT-6.1SolHigh coordinator. Provider execution telemetry is unavailable.

Source Go files pass selected-toolchain gofmt, the controller parses, and repository guidance structural validation passed. Full Go/Robot production qualification was **NOTRUN** because this remains an isolated injected-failure experiment. Production/frozen worktrees remain clean.

## Existing Actions owner and retained measurement truth

[Fresh owner evidence](actions-refresh/report.md) records current PR221e2c81adce7858d5a952991ba9a8b7f72e902038a with34 successes/one publish skip and PR3088d49d0865ad5ad27b2d4e325ec135212080625e8 with25 successes/one Windows devmode failure. #101 updated23:34:41UTC still marks the current native control3/released5/exact727 chain NOTRUN or unqualified. The final owner refresh retained the same heads/status boundary. PR305519a0beb7451cb6e4410f0bee18c826fc493086e and PR313499a83d802ca9a0e2ebe3d77071af10625879297 remain on older integration bases; their existing owners retain cleanup/bounds review and source work.

Historical Actions communitya70993fafc99a9f94041485542d5a720797ca394 has a real six-call warm18.19.3 Action PASS bound to its source/binary/fixture. It performed no acquisition, and its teardown receipt remains failed/-1 despite separately verified disposal. This is retained evidence, not the missing current-source native admission. Do not claim18.19.3 never ran, or relabel historical teardown as PASS.

Existing Actions root01a128d1-75e2-71fe-9528-54b0163600dc keeps #101/#133/#134/#139/#143 and PR221/308, source, pins, worktrees, homes and controller. No source repair or duplicate qualification run occurred. Consume its actual source/hash-bound current18.19.3 baseline PASS, then released18.19.5, then exact727c1ff8b679cdbbe12836e53c87734a1edb0071; stop at first failure. Shipping7945/5a6a and Actions727/be55 remain separate tuples. Historical direct steering NOT_FOUND is not delivery; repository coordination requires actual POST and independent GET receipts.

No accepted timing wave was repeated. The actual maintained-workload complete-controller medians7.085s fresh/5.663s resident remain descriptive20.1% evidence from the corrected two-pair cohort. The old60.253/6.283ms pack component remains outer-guardFAILED and non-equivalent. The accepted filesystem1.6101s versus pack1.6427s pipeline showed no latency gain. Allocation observations are accounting. Gzip6 stored15.88% fewer bytes with mixed latency. Native Go zstd/mixed-size real materialization is a future qualified experiment, not an adopted codec.

All six #125 directions remain in the preserved ledger/graph. Clone ENOTSUP results are host-specific; /dev/fuse and an admitted loop/image route remain absent, with the actual bwrap UID-map read-only failure preserved. No new capability provisioning, privileged mounts, credentials, permission bypass or production deployment.

## Remaining limits and documentation receipt

This turn closed only the private injected-close-error propagation/lease-retention prerequisite. A separately admitted A/B overlap control still needs distinct groups/leases, B lease byte and file-identity preservation during A cancellation, postcancel B response, final descendant census and zero leases. Real incomplete-drain failures, session escape, release-error paths, hostile/concurrent task security, long-lived pools, macOS/Windows and production acceptance remain unproven. No additional experiment or turn starts automatically.

v18.19.6 remains **PUBLICATION HOLD** under185; approval-policy/signing/operator andGO-2026-5970/GO-2026-6629 dispositions remain unresolved. No tag, release or publisher dispatch. Draft224/225 remain **NO-GO**. Research is not a release gate and no issue is closed by these controls.

Documentation receipt
- Canonical guidance: no change. Existing benchmark guidance requires exact source and independent workload/platform/lifecycle proof. This append-only private experiment records the policy integration finding; a future production proposal must own its maintained behavior and documentation.
- Durable learning: a cancellation error must not hide an independent cleanup failure, and automatic lease release must be conditional on successful cleanup. Record lease retention before explicit recovery so a clean final state cannot erase the earlier failure barrier.
- Evidence: frozen Execute source plus retained private group overlay, newly admitted fault source, unsafe baseline and fail-closed raw receipts, before-recovery durable row, independent runtime review and1026-file readback.
- Stale guidance removed: none; original cancellation/home/observer failures and all parent manifests remain intact.
- Remaining uncertainty: overlap, real incomplete-drain and release-error paths, escaped/hostile/long-lived work, nonLinux, current Actions admission and publication approval.
