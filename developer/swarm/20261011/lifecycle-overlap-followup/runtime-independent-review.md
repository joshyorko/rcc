# Independent raw review: close-error lease barrier controls

**Verdict: GO for these two untimed private controls only.** I independently rehashed the frozen binding and read the build, outer-supervisor, worker, response, readback, fixture, and materialization records. I did not build, execute, replay, or mutate source or Actions state.

## Binding and artifact identity

All 542 entries in `source-binding.json` still match their recorded byte lengths and SHA-256 values: 62,153,850 bytes total, zero mismatches. The binding SHA-256 is `2a9bcafbd9e1e664aded2e461e4801808551d1f6a2d117be71d4012255b9068c`, matching the two readbacks and `native-admission.json`. That admission allows only the four bounded build/control phases. The frozen harness hash is `50e3b1ca47cd53e1add883b669d7c9f5a74e76663d4db3e49a9106c0c1aaa0a7`; controller hash is `69d07fa1a70ae74d7db155d34d53dba1a271fcc1df5eca38fb461075d845ca6e`.

Each binary hash matches its exact binding and readback: negative/original `178ff4098a6f5914ac087617673674782390d914900ab7529892d32f35b5e234`; fail-closed `f479d38a4c7e3f8fb671d81b1bcb8b424d7a21ce69dd566df361b5fe4a54614d`. Both bounded build receipts exit 0, have no stop reason, and report no live identities.

Both controls use artifact `sha256:0d77a90cd569b46d4e70ac8423584e802c5eb0141ef5a7ebceb23f67fc1c728d` and source closure `ce5985dbf0edc46a7a120ad1b9fb09df9277060ef57b28cb137717b22f66be56`. They acquired into distinct new 0700 homes (`negative/home` and `fixed/home`) from the same retained gzip-1 provider and fixture. I independently compared all 513 materialized regular files per home to `fixture.json`, including relative path set, size, and per-object SHA-256: both arms had zero mismatches. The two materialization IDs match because the content is identical; the home trees and lease IDs are distinct.

## Raw controls

- **Original frozen implementation:** worker/readback status is `UNSAFE_BASELINE_OBSERVED`. One Execute call canceled after the readiness response. Its record shows `closeFailurePropagated=false` and `leasesReleased=true`; the outer wrapper exited 0 without a timeout/stop or fallback observation. Parent and child identities `(20028, 1539462)` and `(20029, 1539485)` are absent, the final session census is empty, and the home contains no lease JSON. This is preserved as the expected unsafe policy baseline, not a safe PASS.
- **Private fail-closed overlay:** worker has a durable `RETAINED_LEASE_BEFORE_RECOVERY` row with cancellation observed, `closeFailurePropagated=true`, `leaseRetainedBeforeRecovery=true`, and `recoveryReleased=false`. The source-bound worker waits until both recorded identities `(20139, 1543349)` and `(20140, 1543373)` are absent (including zombies) before emitting this row. It then decodes the retained lease bytes and calls ordinary RCC `Release` with that same snapshot. The final `PASS_PRIVATE_FAIL_CLOSED` row records recovery and lease release; the bounded outer result exits 0 with no stop, no live identities, and no remaining lease JSON.

For both arms, `responses.jsonl` exactly matches the worker’s `cancel-ready` row, the readback’s pre/post guard snapshots are identical, and all eight checked roots remained owned by UID/GID 1000 at mode 0700. Both bounded outer receipts returned 0 with `stop=null`, empty final live-identity lists, and zero vanished scratch entries. I also checked the recorded PID/start pairs directly after completion; all four identities were absent.

The original implementation suppresses the supervisor close error and proceeds to lease release. The private overlay propagates the injected close error, prevents automatic release, records the retained-lease barrier, then recovers through normal RCC `Release`. The injected close helper returns its exact failure marker only after an empty process-group census; the fixed worker checks for that marker. The negative worker cannot serialize the suppressed error text, so its row records the `false` propagation result; the matching binding and fixed arm’s positive marker check supply the cross-arm injection evidence.

## Limits

This control tests close-error propagation and lease policy **after an empty process-group census**. It does not test release while a descendant is live. The retained raw JSONL does not include the transient lease body or its hash; the exact decoded-snapshot use is established by the source-bound worker’s identity checks and its call to normal `Release`, not by independently rehashing a preserved lease file. Do not infer concurrent/overlapping `Execute`, pool or persistent-worker behavior, hostile-task containment, escaped-session safety, non-Linux support, production readiness, Actions qualification, or release acceptance. Timing fields in these controls are not performance measurements. Previously preserved strict-home-mode failures remain unchanged.

Machine-readable counts, raw receipt hashes, source/build binding, and check results are in [runtime-independent-review.json](runtime-independent-review.json). No Actions issue or PR was changed.
