# Independent postmeasurement review

**Result integrity: PASS for the six accepted measurement labels.** Review was read-only; no build, workload execution, replay, or source change occurred.

The accepted measurement labels are exactly `pair-1-fresh`, `pair-1-resident`, `resume-pair-2-resident`, `resume-pair-2-fresh`, `resume-pair-3-fresh`, and `resume-pair-3-resident`. Each accepted controller receipt exactly matches its final raw worker receipt. Each has six alternating `agentdocs`/`topology` tasks and 90 cases; all task IDs are unique within their task and every status is PASS with zero failures, errors, or skips. I normalized the runtime PID prefixes and compared the IDs both with the maintained-source ID set and directly with the five and 25 `test_` methods in the pinned input snapshot AST. All match. Source bytes have the same before/after SHA in each task.

For each accepted arm, the outer result reports return code 0, no stop reason, and no final observed live identities. Pair 1 uses the retained v3 runner; resumed pairs 2 and 3 use the admitted v4 runner. All six invocations have equal strict pre/post owner/mode guards: owned mode 0700 roots and mode 0750 global holotree. Fresh arms use six PIDs and six leases; resident arms use one PID and one lease. All homes report lease release and contain no lease JSON; task roots retain only task logs, with task temp directories removed. I independently re-read all 513 materialized files in each of the six accepted measurement arms (3,078 file checks): exact path set, regular/no-follow file type, length, and SHA-256 all match the same pinned gzip-1 artifact.

The original `pair-2-resident` v3 sampler attempt remains rejected and absent from accepted rows/summary samples. Its worker completion does not turn the missing outer result into an accepted sample. Pair 1 was retained, not rerun. The resumed schedule contains only the remaining four accepted arms. Runner cohorts are explicit: pair 1/v3; pairs 2 and 3/v4. The full pooled `summary.json` crosses those cohorts and is descriptive only.

## Arithmetic

I recomputed the corrected v4-only medians from accepted pair-2 and pair-3 receipts:

- Batch time: fresh 4,083.114 ms; resident 2,751.273 ms.
- Controller elapsed time: fresh 7,084.674 ms; resident 5,662.518 ms, a 20.074% difference in these two paired samples. Fresh-minus-resident controller deltas are 1,570.148 ms and 1,274.164 ms.

The v4-only result is a two-pair descriptive observation for this finite fixture. Controller elapsed time includes controller and wrapper/readback overhead; it is not worker-only time. The worker `batchNs` is an API batch component. Neither establishes a general RCC or product performance claim. Go CPU profiles cover the post-Acquire `Execute` phase only, not Python or Acquire CPU.

The independently retained summary is `results-group-2/summary.json` (SHA-256 `9f62f5ab5744d758b8b1bd61b10052164a94339bbdb4cf655879e824e7c657d4`). The coordinator's broader readback `final-integrity-readback.json` reports 17 materializations, 8,721 file checks, zero mismatches, 681 inputs, 535 binding entries, 519 provider files, and zero leases; it is additional cross-check evidence, not a substitute for the six direct measurement-arm checks above (SHA-256 `d2db9a07c52b389dd2a15f1c6e8ada409205a6b939cc9fe8662557460c321a68`).

## Frozen identities and raw evidence

- Controller: `38304141264d61e875bdd5bd6225dc3ba637b09c780cea48554cfc17a497f48e`
- Sampler v4: `3c953181a2ac08d0bd15d24d4504d87ce1954ea86136c39131e100eddee9194a`
- Build binding: `8520ff57e69141722509db163b082895652c60ce47e31dab34b9e8d93613964c`
- Worker binary: `3bbd5de7fd97774141950fe183e6636fa49923ad7f346bf61cb33793e6c055d2`
- Task driver: `8fc5926c98731e75c7f01df4d4a55640db67bd23dab7cf0fe7241ee7b4f22f7e`
- Controller raw ledger: `results-group-2/controller.jsonl` SHA-256 `642f31fe433aa0498dd2772db921e4ba6875076213bef4fca3254893a2030888`
- Summary: `results-group-2/summary.json` SHA-256 `9f62f5ab5744d758b8b1bd61b10052164a94339bbdb4cf655879e824e7c657d4`
- Corrected outcomes: `measured-outcomes.json` SHA-256 `dcc2f5aca6640576e9dfe84eec3fc8d1b635db73b68d6acb332870fff8e4a175`

The measured scope is a bounded private fixture and trusted cooperative task lifecycle. It is not a persistent daemon, Actions acceptance, hostile-code sandbox, full-suite run, or release qualification. Prior rejected setup and observer-failure runs remain rejected and are not included in the arithmetic.
