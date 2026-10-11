# Independent outer-timing arithmetic check

I recomputed the medians directly from the accepted raw files in checkpoint `e70fe5531145836b79a386d48250e094d22e2c2c`: each pair's bounded-runner `result.json.seconds` and the matching `controller.jsonl` invocation `elapsed_seconds`. No rerun or evidence mutation was performed. All six runner return codes are zero with no stop reason or observed final live identities; the six controller records have matching eight-entry pre/post guard sets.

| Timing scope | Fresh six-child samples (ms) | Median | Resident six-request samples (ms) | Median |
| --- | --- | ---: | --- | ---: |
| Bounded runner `result.seconds` | 2688.864, 2547.324, 2355.866 | 2547.324 | 2018.862, 2075.290, 2208.656 | 2075.290 |
| Controller invocation elapsed | 2726.297, 2586.305, 2387.362 | 2586.305 | 2051.858, 2109.359, 2239.377 | 2109.359 |

These wall times include more than the `ExecuteWithStreams` batch metric: native diagnostic startup, cold acquire, repeated payload checks, execution/lease cleanup, source/guard checks, profile flushing and polling; controller elapsed also includes launcher and receipt/readback/guard work. They are not per-stage sums or an uninstrumented deployment estimate. This is accepted evidence for the finite synthetic prototype only, not Actions acceptance or a production persistent-worker claim.

The earlier cached-work comparison remains NO-GO and was not measured. The previously failed 60.253 ms versus 6.283 ms component wave remains unaccepted because its outer wrapper failed the 0700→0750 guard and its reader omitted production secure-path/stat/allocation work; that failure and the original 0750 home evidence were not rerun or repaired. See the [successor report](../successor-next-2035/report.md) for these preserved limitations.
