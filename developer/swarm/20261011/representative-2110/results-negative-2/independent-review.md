# Independent negative-2 controls readback

Read-only review of the retained controls. No rerun or build was performed.

## Result

The corrected error control passed: one Python process emitted the expected poison `ERROR` row and subsequent `PASS` probe with identical lease identity, matching before/after source-byte hashes, and `cleanState=true`; the worker receipt ended `PASS` and its ExecuteWithStreams lease was released.

The cancel control produced the expected diagnostic failure under the original direct-child-only supervisor. It emitted one durable `cancel-ready` row, then the Go worker failed because the recorded child remained live. The bounded runner completed with `stop=null` and no observed live identities, but its `known_identities` omitted the sleeping grandchild (PID 14882/start 581404); the post-run PIDFD fallback found that exact identity live, sent `SIGTERM`, and observed it as zombie. The direct driver identity (PID 14881/start 581379) was already absent. No lease JSON remained after the fallback, and the supervisor/controller path kept all recorded root guards at their required modes and owners.

This is evidence that the original RCC supervisor killed the direct child but did not kill that grandchild itself; the external controller fallback performed the cleanup. The cancellation worker receipt remains `FAILED` with `leasesReleased=false` because it exited at its live-identity assertion. Actual lease-directory enumeration after cleanup found zero lease JSON files. Keep both facts; do not relabel this as an RCC cancellation pass.

The controller JSONL contains only `invocation(error-control)`, `accepted(error-control)`, and `invocation(cancel-control)`. It contains no schedule or controls-complete record. The cancel invocation exited 1, so the controller stopped before timing. This does not admit the group-supervisor overlay or any workload measurement.

## Exact retained evidence

- Current negative-2 native admission: `native-admission.json`, SHA-256 `c2dd14abb61ada75d733066b85dd2a8dfeb1603dece1e305bfaa97fa96de3270`.
- Build binding: `build-binding.json`, SHA-256 `17454099720471745e81605c82876e4ae0f87e18dfcb7f79abca4a5ee600131c`; all 534 listed file sizes and hashes independently rechecked, 62,092,004 total bytes, zero mismatches.
- Controller JSONL SHA-256 `2437918f51f567534e3b996d568e7e22012b3ada62d647de669607dfa2103d1a`.
- Error worker receipt SHA-256 `641ae81b6d4978b49ae9f8b94db9fa6bd8ee2b7c8d832e2ac08218c9b1d6ecfc`.
- Error raw responses SHA-256 `bf32a19408a769fec75b2062907edd70bf4440d6ce29e6ca8f3bb895f29785b9`.
- Cancel worker receipt SHA-256 `277d9b99bd6a27fef047774b8ebe1bd06f404ce545b4bf7b7a38f06222e40207`.
- Cancel raw readiness response SHA-256 `a021964c9b9260c1f785290c4e3dab068cd48565312ad8e87f5af01e2139aa9c`.
- Cancel bounded-run result SHA-256 `1c26c37f912feb865a9a2a7f9f69339e2867340f4df8f634d565bd82ae84239b`.
- Exact PIDFD fallback receipt SHA-256 `a3bef66f8734167e1be2bb8b87623ab4a60ff98ee6a13bdd27a948a309ca75e7`.

The cancellation raw response records process group 14869 and exact starts for driver 14881/581379 and descendant 14882/581404. The wrapper sampled only 14869 and 14881; the retained fallback records driver absent and child 14882 in zombie state after signaling. This explains why the outer supervisor's sampled final list alone is insufficient for this case.
