# Frozen private pack harness admission review

**Reviewed source SHA-256:** `0f30a335f9a16ccba1640de8bdeb4beb9379a02560d409a9bd1459c4f9c0786a` (`pack-e2e/source/main.go`). Verified against the requested final hash. Review was source-only: no compile, preparation, worker, or E2E execution was performed by this reviewer.

**Disposition: conditional GO for isolated build; NO-GO for fixture execution until the standard-index set cross-bind and final worker-root guard are closed.** This is a private Linux host-Python fixture only. It is not an Actions qualification, full-cold result, portability claim, or winner/default result. Keep the earlier strict-0700 baseline failure intact.

## Accepted source seams

- `mode700` checks Lstat directory, exact 0700, and current euid/egid. Scheduler prechecks the root, producer home, workers root, each worker root and home; after every worker it checks home, root, producer home, and workers root. Worker rejects an existing consumer `artifacts/v1/content` path before acquisition.
- Packed provider uses component-wise no-follow root traversal, a no-follow/nonblocking leaf open, regular-file `fstat`, bounded index and pack allocation, pinned index and whole-pack SHA, offset coverage, per-frame SHA, and independent buffers. It resolves the manifest and retrieves/verifies the standard object index, checks identity/count/encoding/media, and rejects wrong media for known packed digests. Normal cold acquisition, actual `ExecuteWithStreams`, the real host CPython wrapper, Acquirer compatibility preflight, materialized compatibility verification, lease-absence proof, and warm acquisition with zero provider calls are used.
- CPU profiling starts before packed provider construction. Provider setup, acquire, execute, warm, and total pipeline stages are recorded. Worker failure receipt writes are retried by the deferred failure recorder. Scheduler preserves stdout/stderr to a log, copies incremental worker JSONL, records guard/read/write errors, and stops at the first error.
- The frozen-source build diagnostic in `pack-e2e/build-1/result.json` is for an earlier source and failed; it is not evidence this SHA builds. Coordinator says that diagnostic was corrected and no build/run was done on this final SHA.

## Execution blockers

1. `newPackedProvider` populates `standard` as digest→size, then tests `standard[x.Digest] != x.Size`. It does not check map membership. Since `DecodeObjectIndex.Validate` permits `StoredSize == 0`, an unknown 64-character pack digest with size 0 can compare equal to a missing map key's zero value. Equal entry counts do not prove equal descriptor sets. Use `want, ok := standard[x.Digest]` and require `ok && want == x.Size`, validate canonical digest syntax, and add a mismatched-set negative with a rehashed candidate index/pack. This is a specific gap in the required standard-index cross-bind.
2. Scheduler does not postcheck the immediate `workerRoot` directory. It prechecks it, but a worker may change its mode/owner while home/root/workers-root checks still pass. Add `workerRoot` to the post-worker record and fail-closed guard set. Ancestors above the configured scratch root are not guarded by source; the external owned-fixture launcher must preflight/postcheck those enclosing boundaries and the provider/pack roots.

The first item is a runtime-integrity gate, not a reason to block an isolated compile. The second is a private-fixture boundary gap; ensure the external guard covers the worker root if execution is authorized without a source fix.

## Supervisor control

`run_bounded_v3.py` SHA-256 is `7f59d956b2f602060be81b59f29fba32941d35f763276a7d6a81989d6f3d55c6`; `supervisor-control.py` SHA-256 is `14c72e5182a6507cdc5aabe3ae5705f09d70c77f5ee8bdf11cc3363c031d191d`; retained control result SHA-256 is `814c91ede3f4febf2b09ac1c067ceae2df88480dfbae30ca1bfd932b741bbe51`. The intentional separate-process-group child reached the one-second deadline. Result: stop=`deadline`, launcher return code `-15`, sampled tree RSS maximum 25,559,040 bytes, and `final_observed_live_identities=[]`. The wrapper's exit status 1 is expected for the deadline control. It demonstrates sampled timeout cleanup for this control; it is not a hard quota or proof against sub-100ms/unobserved escaped children, and it is not a run of the RCC harness.

Documentation receipt: canonical guidance unchanged; durable learning is recorded in this independent review; evidence is the source hash, frozen API review, and retained supervisor-control result; stale guidance none; remaining uncertainty is the no-build/no-runtime status of this exact source and the two execution blockers above.
