# Real repository workload and lifecycle evidence

The owned continuation now measures actual unmodified maintained RCC developer suites through normal public `Acquire` and `ExecuteWithStreams`, with cold consumer CAS per arm. Six accepted arms executed 540 unittest cases and 18 full-tree agent-guidance validations. A finite six-request Python child was compared with six fresh children. Independent raw review passed every accepted arm. This is a Linux exploratory prototype on a warm, uncontrolled OS cache; it does not qualify Actions, a persistent daemon, production defaults, or a release.

## Outcomes

| Pair / observer | Fresh batch ms | Resident batch ms | Complete fresh controller ms | Complete resident controller ms |
| --- | ---: | ---: | ---: | ---: |
| 1 / retained v3 | 4102.078 | 3041.904 | 6111.079 | 5399.077 |
| 2 / corrected v4 | 4041.053 | 2660.442 | 7320.566 | 5750.418 |
| 3 / corrected v4 | 4125.176 | 2842.104 | 6848.782 | 5574.617 |

For the corrected v4 cohort only, two-pair controller medians were **7084.674 ms fresh versus 5662.518 ms resident**, a descriptive **20.074%** difference. Batch medians were 4083.114 versus 2751.273 ms. Controller time includes wrapper, normal acquisition/import, source and materialization readback, profiles and controller checks. The API batch component is reported separately. The original accepted first pair was retained without replay. Do not pool the three outer pairs as one unchanged-observer cohort. Untimed smoke, controls, and rejected attempts are excluded from performance statistics.

Raw results are [controller.jsonl](results-group-2/controller.jsonl), [summary.json](results-group-2/summary.json), and [measured-outcomes.json](measured-outcomes.json). [Independent review](postmeasurement-independent-review.md) checked exact raw receipt equality, all maintained test IDs, PID/lease cardinalities, guards, cleanup, every accepted materialized file, and arithmetic. Fresh arms used six PIDs and leases; each resident arm used one PID and lease. Each request loaded the project tests and suite anew; resident standard-library state remained cached. Each task additionally audited all source bytes before and after, so these measurements include declared integrity overhead.

The real workload alternates all five `scripts/test_validate_agent_docs.py` cases plus full-tree `validate_agent_docs.validate()` with all 25 `scripts/test_validate_release_topology.py` cases, three times each. No source tests were modified. The topology tests create local metadata/index fixtures and invoke local Python validation subprocesses; they never publish or access live Actions. The retained artifact's 96-byte `bin/python` wrapper delegates to the selected host Python 3.12.14. This is therefore a real developer workload through materialization and streamed execution, but not the full contained developer toolkit or downstream package fixture.

## Actual barrier, private prototype and preserved failures

Frozen Linux supervisor source kills the direct child on context cancellation. The owned cancel-ready control proved that its same-group sleeping grandchild remained live after the direct child exited. Exact PID/start-bound outer fallback terminated that child; this negative stays **FAILED**. A private overlay then added process groups, child-subreaper adoption, bounded group drain, start-bound pidfd signals and `Wait4` reaping before lease release. The final cancellation control's parent 15470/start637472 and child15471/start637496 were both **absent before outer fallback**, with no fallback signal. The separate poison/error then clean-probe control passed while using the same PID/lease. Both controls preceded timing.

The overlay is an owned Linux experiment, not a production patch or security sandbox. Cooperative process-state restoration covers cwd, environment, argv, sys.path, tempfile state and project modules; task temp directories are removed and logs retained. Session escape, hostile code, concurrent jobs, long-lived worker recycling, Windows and macOS are unqualified. The reviewed design receipt [workload-design.md](workload-design.md) describes its earlier source-only stage; current execution and private group repair are recorded here and in the exact admissions, rather than rewriting that historical preparation claim.

All first failures are preserved:

- `results-negative/`: setup rejected the assumed lease-directory 0750 when observed mode was 0700 under umask0077; zero tests ran. Subsequent private driver strictly required observed 0700.
- `results-negative-2/`: actual frozen direct-child cancellation left the grandchild live; fallback cannot convert it to acceptance.
- `results-group/`: first real task failed before any test because the private unittest result constructor rejected TextTestRunner arguments. The original source, raw failure and lease cleanup are retained; the corrected driver then passed a new untimed fresh/resident smoke with 180 cases before measurement.
- Original `results-group-2/pair-2-resident`: worker eventually passed 90 cases, but the v3 outer sampler aborted on ENOENT while an actual topology test deleted a temporary metadata file. Its outer result is absent: **REJECTED**, excluded from every performance statistic. Both partial and final worker/profile receipts are retained.

The isolated v4 sampler catches only concurrent leaf disappearance, records it, and fails closed on other inspection errors while waiting/cleaning the child. A separate reviewed control wrote/deleted 47,744 files and caught two real ENOENT races; a permission-denied control returned nonzero, terminated/waited its child and left no live identity. It also adds a final private-session census and pidfd/start-bound cleanup. Exact independent `resume-measure` admission permitted only the four remaining arms; pair1 was never replayed. This remains a sampled observer with stated limitations, not a hard quota or hostile containment boundary.

## Exact source and integrity

Repository **joshyorko/rcc, ID1051101627** and main **7d17122436584dcbfe9b8afddeb56d7c81a10af7** were refreshed through authorized GitHub reads. This append-only prefix continues verified parent **af32f876377911d1995706e290a424f0f0e4f417**; its [remote verification](parent-remote-verification.json) preserves the earlier immutable chain from requested2f6570c99d5ba98f26ceb7b74d6da69fb84f99e8. Existing source, branches, manifests and issue ownership are retained.

| Identity | Exact SHA / SHA-256 |
| --- | --- |
| Frozen shipping RCC source | `7945f956b428359928908a9f42e36417b113dafd` |
| Frozen shipping Linux binary, unchanged | `5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21` |
| New private Go driver | `f3bd699f0f5fccdda76252c1848a0a7d69e6c0f2c92563a76145583623577a04` |
| Private group supervisor | `27f147811ed979bfe3dbb90ebbb501ddc9dbd40c2e9fdfcb6267b9fadadbc0cc` |
| Private prototype binary | `3bbd5de7fd97774141950fe183e6636fa49923ad7f346bf61cb33793e6c055d2` |
| Python task driver | `8fc5926c98731e75c7f01df4d4a55640db67bd23dab7cf0fe7241ee7b4f22f7e` |
| Current controller | `38304141264d61e875bdd5bd6225dc3ba637b09c780cea48554cfc17a497f48e` |
| Sampler v4 | `3c953181a2ac08d0bd15d24d4504d87ce1954ea86136c39131e100eddee9194a` |
| 535-file exact build/runtime binding | `8520ff57e69141722509db163b082895652c60ce47e31dab34b9e8d93613964c` |
| Runtime source closure | `ce5985dbf0edc46a7a120ad1b9fb09df9277060ef57b28cb137717b22f66be56` |
| Uncompressed frozen input tar | `70654af0d400a8e04cbacaa72a7f3af65315a7a8f92f9f40d110c59c217cc204` |
| Retained gzip1 artifact | `sha256:0d77a90cd569b46d4e70ac8423584e802c5eb0141ef5a7ebceb23f67fc1c728d` |

[Final integrity readback](final-integrity-readback.json) covers all **17** materializations including rejected/control attempts: **8721** regular-file size/SHA checks, **zero mismatches**, all681 exact input files/modes unchanged, all535 build bindings and519 retained provider files unchanged, strict owned0700 homes/task roots, zero leases, zero task temp directories and no live worker executable. Independent review separately read all3078 files across the six accepted arms. All task logs were copied exactly; earlier failed private-home0750 is preserved unrepaired.

Six existing Go CPU profiles were decoded without workload replay. They cover only post-Acquire Go execution: total samples 20/10/20/10/20/0 ms across fresh/resident pairs, too sparse for a meaningful Go-hotspot conclusion. Python/Acquire CPU is outside these profiles; waited-child CPU is separately recorded. An initial read-only pprof attempt hit the unavailable default cache; its stderr is retained and decoding succeeded using the selected owned Go cache. No provider routing telemetry is exposed: configured coordinator GPT-6.1 Sol High and reused bounded Luna High source/review workers are reported as configuration, not observed provider internals.

Bounds were native context25s, outer30s, build60s, GOMAXPROCS2/GOMEMLIMIT1GiB, sampled2GiB treeRSS/128MiB logical scratch/8MiB output and2.5GiB free-space floor. These are sampled ceilings, not absolute peaks or kernel quotas. All native workload execution belonged to the coordinator; workers performed bounded preparation/read-only review.

## Actions and remaining dependency boundaries

[Actions exact source/fixture refresh](actions-refresh/report.md) and [final live readback](actions-final-refresh/summary.json) retain PR221 head **e2c81adce7858d5a952991ba9a8b7f72e902038a** and PR308 head **8d49d0865ad5ad27b2d4e325ec135212080625e8**. At21:52:49UTC,221 had34 successful checks and publish skipped;308 had25 successes and one Windows Python3.12 devmode failure. Adapter/verifier/tests/fixture Git blobs and SHA256 are bound to that exact308 source. Existing #101 source-suite PASS evidence is explicitly not runtime qualification. No new source-bound baseline18.19.3 runtime PASS was present in existing #133 coordination.

Existing Actions owner root01a128d1-75e2-71fe-9528-54b0163600dc retains #101/#133/#134/#139/#143, PR221/308 and its controller, homes, worktrees and pins. No duplicate source repair, replacement Actions task, or claimed direct delivery occurred. Consume that owner's exact baseline18.19.3 raw controller/config/fixture PASS with capacity/cache/home/lease/reader/census proof, then released18.19.5, then exact727; stop at the first failure. Released5 source d1aec7d0bb897a81274423c7a6bb747233f9c263 / Linux1a617ad7c736fa67c605e20e5ebe3c7d54b02cd548f733e05c809cf49a48db1e and candidate727c1ff8b679cdbbe12836e53c87734a1edb0071 / Linuxbe55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e remain distinct from shipping7945/5a6a. Authorized coordination uses existing repository channels with POST and independent GET receipts only.

All six #125 directions remain tracked in [execution-ledger.json](execution-ledger.json) and [dependency-graph.json](dependency-graph.json). Accepted compression/pack/prewarm/synthetic waves are reused, never replayed: gzip6 stores15.88% fewer bytes but mixed latency, small-object packs no accepted latency gain, real warm acquisition remains scoped. The invalid60.253/6.283 component comparison stays rejected for the original0700→0750 guard and missing production reader behavior. Two exact64KiB FICLONE ENOTSUP95 probes are not a universal clone prohibition. This host lacks /dev/fuse and an admitted loop/image route; actual bwrap UID-map probe failed read-only. Pursue independently feasible workloads; do not provision infrastructure or invent mounted capability. The CAS FD-session proposal remains unimplemented pending pre-visible-commit identity/replacement/symlink/cleanup proofs.

v18.19.6 remains **PUBLICATION HOLD** under #185: unresolved policy/signing scope and GO-2026-5970/GO-2026-6629 dispositions, historic403/required environment404 and no separate publication approval. No tag, release or publisher dispatch. #224/#225 remain draft **NO-GO**. Research is not a release gate and no issue is closed by these measurements.

Documentation receipt is [documentation-receipt.md](documentation-receipt.md). Further prototype or downstream acceptance must retain the stated lifecycle/platform/workload scope; this evidence checkpoint alone authorizes no production change.
