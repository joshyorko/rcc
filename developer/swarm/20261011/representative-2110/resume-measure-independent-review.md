# Independent resume-measure review

Review is read-only; no source build or runtime launch was performed for this review.

Pair 1 remains eligible for reuse. In both `pair-1-fresh` and `pair-1-resident`, the retained controller `accepted` receipt equals the final raw worker receipt. Each arm completed six alternating jobs and 90 test cases with `PASS`, released leases, had outer-runner `returncode=0`, `stop=null`, and no final observed live identities. The fresh arm has six distinct worker PIDs and lease IDs; the resident arm has one PID and one lease ID. All rows have clean state and identical source-byte hashes before and after execution. Both preserved outer receipts have matching strict pre/post guard lists, 513-file materialization readbacks, and direct independent path/regular-file/size/SHA-256 revalidation of every materialized file against the pinned fixture. Pair 1 is not to be replayed.

The next old arm, `pair-2-resident`, is rejected as a sample and must remain so. The v3 outer sampler aborted on a concurrent `FileNotFoundError` during scratch-tree inspection. The retained final worker receipt reports six passing tasks/90 cases and released leases, but no outer `result.json` exists; the supervisor recorded no live executables and no remaining leases. This arm is not accepted or reused.

The resumed controller verifies the retained pair-1 outer receipts and requires the last prior row to be the failed pair-2-resident invocation. It proposes only remaining labels `resume-pair-2-resident`, `resume-pair-2-fresh`, `resume-pair-3-fresh`, and `resume-pair-3-resident`; pair 1 is read from retained receipts, not rerun. It reports per-pair runner cohorts and labels pooled outer metrics descriptive only. The v4 sampler’s narrow change catches `FileNotFoundError` for a file concurrently removed during scratch walk, while other observer errors remain fatal; it adds session-ID final census, pidfd-bound cleanup signals, and final output/scratch scans. Scope remains sampled, cooperative, and not an OS containment boundary; new-session escapes and hard resource quotas are not guaranteed.

The new source binding currently hashes 535 entries (62,116,681 bytes) with zero size/hash mismatches. Build-3 return code is 0. Frozen identities: controller `38304141264d61e875bdd5bd6225dc3ba637b09c780cea48554cfc17a497f48e`; sampler v4 `3c953181a2ac08d0bd15d24d4504d87ce1954ea86136c39131e100eddee9194a`; build binding `8520ff57e69141722509db163b082895652c60ce47e31dab34b9e8d93613964c`; worker binary `3bbd5de7fd97774141950fe183e6636fa49923ad7f346bf61cb33793e6c055d2`; task driver `8fc5926c98731e75c7f01df4d4a55640db67bd23dab7cf0fe7241ee7b4f22f7e`.

## Decision

Pair-1 raw review: PASS. Pair-2-resident: rejected and excluded. The prior native admission is stale for the new controller/sampler binding. **No resume-measure admission issued in this review** pending independent final v4 source approval and the requested sampler-control probe receipts. Any later approval must allow `resume-measure` only, retain pair 1 without replay, and keep the failed pair-2 arm excluded. Smoke elapsed values are not measurements.

## Receipt hashes

- `results-group-2/controller.jsonl`: `029b2fa8fa9fc142fe0104aa8b205eee13cc1fdbfd9c56ad2e7117dba9931170`
- `results-group-2/pair-1-fresh-worker.jsonl`: `a0f258db02ebfc81da3a3ef009b4beeab4fae137dbc09bc80a9a9a9bed7b9742`
- `results-group-2/pair-1-fresh/result.json`: `a94de373503a85ca9f304404bb266fd617e1b42d28ec06dc6302789ef6fe1d05`
- `results-group-2/pair-1-resident-worker.jsonl`: `fddd1818ab46e516f94193f3ea19960cdd853dda378f014803f69e100c5d5f1c`
- `results-group-2/pair-1-resident/result.json`: `adcbc5306894e9f178cf8927d4dac2a67e273eb7ee0ffdd4a937b08aa6956fd4`
- `results-group-2/pair-2-resident-final-worker.jsonl`: `e01ad4c3b35a3703f73622c07298a65687e9fd11538e9bb5540ee60c2640beda`
- `sampler-failure-readback.json`: `3281b594b54cb30b5aafff27fcb019f259e4f87240fbf5c8fa95c311861fec55`
- prior build binding: `build-3-binding-pre-runner-fix.json`, `f6c8d9c9f02fce3b712b7785f9871ebd3b5fe707ad643750ca9c8d7acf7ec9a1`
