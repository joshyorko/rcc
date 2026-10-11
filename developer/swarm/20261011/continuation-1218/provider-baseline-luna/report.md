# RCC filesystem-provider streaming baseline

## Scope and source identity

This was an actual native RCC v18.19.6 filesystem-provider run, using the accepted main-727 executable and historical Actions-produced provider fixture. It measures only stored-object GET plus integrity-checking and response streaming over loopback. It does not measure holotree materialization, environment acquisition, an RCC consumer, Actions concurrency, TLS/authentication, or end-to-end action performance.

- RCC source commit/tree: `727c1ff8b679cdbbe12836e53c87734a1edb0071` / `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`.
- Existing executable: `/workspace/work/rcc-swarm/evidence/continuation-20261011/strict-consumer/rcc-main727`, SHA-256 `be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e`; it was reused in place, not copied or modified.
- Source digest checks for the fixture’s named server/provider source files passed against `/workspace/work/rcc-swarm/final-main-727`.
- Fixture: artifact `sha256:ec3795182e7261439a8b9a9a9b7bd0834b7a7d65202067b17e5589e06f41b83e`; index SHA-256 `e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6`; 5,537 unique indexed stored objects totaling 69,466,848 bytes. The full CAS closure contains 5,541 objects / 71,664,362 bytes, plus one manifest and the provider `.audit` file.

The server command was the admitted exact argv: `rcc-main727 cache serve --root <provider-content> --listen 127.0.0.1:0 --backend filesystem --json`. The runner required the returned canonical root and a `127.0.0.1` HTTP URL. Client requests used only `GET /v1/objects/sha256/<digest>`, `Accept-Encoding: identity`, one persistent HTTP/1.1 connection per client worker, no redirects/retries/ranges, and a 64 KiB read buffer. Every response passed status 200, octet-stream type, exact Content-Length, EOF size, and SHA-256 checks. Each timed control completed 5,537 GETs and returned exactly 69,466,848 verified bytes.

## Measurements

The schedule was 1, 4, 4, 1, 1, 4 workers, with three controls at each setting. The full closure was hashed once before timing, warming the OS page cache; no cache eviction was attempted. The timer covers from just before concurrent GET dispatch through all EOF and digest checks. Preflight, startup/shutdown, and report writing are outside the control timer.

| Control | Workers | Verified GETs | Verified bytes | Wall seconds | Client CPU seconds | Server CPU ticks (100 ticks/s) | Server `rchar` | Server `read_bytes` | Client peak RSS | Server sampled peak RSS |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 01 | 1 | 5,537 | 69,466,848 | 1.125366 | 0.581228 | 101 | 139,775,118 | 0 | 40,980,480 B | 21,635,072 B |
| 02 | 4 | 5,537 | 69,466,848 | 0.805296 | 0.868065 | 88 | 139,771,030 | 0 | 40,980,480 B | 20,889,600 B |
| 03 | 4 | 5,537 | 69,466,848 | 0.832378 | 0.926733 | 90 | 139,770,942 | 0 | 40,980,480 B | 21,037,056 B |
| 04 | 1 | 5,537 | 69,466,848 | 1.044370 | 0.548047 | 92 | 139,774,102 | 0 | 40,980,480 B | 20,688,896 B |
| 05 | 1 | 5,537 | 69,466,848 | 1.052747 | 0.551067 | 93 | 139,773,630 | 0 | 40,980,480 B | 20,705,280 B |
| 06 | 4 | 5,537 | 69,466,848 | 0.797699 | 0.887038 | 86 | 139,771,582 | 0 | 40,980,480 B | 21,381,120 B |

Median wall time was 1.052747 s at one worker and 0.805296 s at four workers (1.307x in this warm loopback component). This is an observed result for this specific client/server/fixture setup, not a production worker recommendation or end-to-end performance claim. Client CPU increased with four workers. `rchar` was about twice the returned body bytes while `read_bytes` remained zero; source inspection shows `Filesystem.GetObjectByDigest` hashes the stored object and seeks back before streaming it, so the result includes validation plus response streaming on a warm filesystem cache.

The separate profiled run used the same provider with `--pprof`, a single sequential full-object GET pass, and the same verification gates. It completed 5,537 GETs / 69,466,848 bytes in 1.008822 s. The resulting `provider-cpu.pprof` is 10,528 bytes, SHA-256 `3e741e77e19750def5a05cd0acaf64b2f8d6c49771bd5edec24b7edd44f15e28`; gzip integrity check passed and decompressed content was 21,571 bytes. Profile timing is separate and is not compared to unprofiled controls. The profiled server’s `wait4` reported exit 0, no forced kill, 0.506267 s user CPU, 0.418967 s system CPU, and 32,712 KiB max RSS.

## Guards and integrity

- Each server process was stopped with SIGTERM and reaped with exit 0 within the 10-second bound; the runner confirmed each assigned loopback port was closed. No RCC/cache server remained after the run.
- Lowest sampled free bytes during controls: 2,804,604,928; final free bytes: 2,795,368,448. Both exceed the 2,684,354,560-byte floor.
- Peak sampled client RSS: 40,980,480 bytes, below the 536,870,912-byte abort limit.
- Final allocated bytes under the owned namespace: 2,347,008, below the 10,485,760-byte limit. The directory contains the runner, two full closure receipts, results/logs, small private RCC homes, and the profile.
- Full pre/post provider inventory comparison passed: 5,541 CAS files / 71,664,362 bytes, manifest SHA-256 `bcc37ca0f4a46ead52d6ec2f68b877a07579ba1563a75be991d52e63472fa0c0`, index SHA-256 above, `.audit` SHA-256 `79e0f5220a68be8f3d808e9c201832664c8d00581d22dc93ed6791ca7c02e5cd`, identical file path set, hashes, sizes, modes and mtimes, identical directory census/modes/mtimes, empty `tmp`, and no `.restore-state`. Atime was intentionally not compared because reads may update it. No symlinks or hardlinked files were found in the selected provider root.

## Evidence and limits

Raw evidence is in this directory: [results.json](results.json), [preflight.json](preflight.json), [postflight.json](postflight.json), [run.log](run.log), server stderr logs, [provider-cpu.pprof](provider-cpu.pprof), and the reproducible [stream_baseline.py](stream_baseline.py). The first harness invocation exited 1 before RCC launch because tee had created `run.log` before the owned-namespace guard; the untouched failure is retained in [prelaunch-failure.log](prelaunch-failure.log). No binary/server/client request ran during that setup failure. The corrected invocation reran the complete closure preflight before launch and finished with exit 0.

Receipt gaps: the successful run dynamically validated each ephemeral loopback startup URL and shutdown/port closure, but the runner did not serialize the returned random port or startup JSON. It serialized per-control unprofiled server CPU/IO/RSS counters, but overwrote the unprofiled server’s whole-session `wait4` tuple with the separate profiled server tuple; thus no unprofiled whole-session `wait4` number is claimed. Server CPU for timed controls is reported per control from `/proc` ticks (10 ms granularity). No per-request latency distribution or syscall-level tracing was collected.

The Python `http.client` workload differs from RCC’s Go HTTP consumer, and loopback has no remote transport, TLS, auth, or storage contention. The OS cache was warm and uncontrolled. These results are a component baseline only; they cannot rank archive formats, packing, reflinks/FUSE/images, or full lifecycle/Actions performance. No source, workflow, fixture, branch, issue, PR, or tag was changed.

## Reproduction receipt

- Binary/fixture/source hashes: `preflight.json`.
- Exact full file and directory closure after the run: `postflight.json` (equal to `preflight.json` inventory).
- Numeric exit from corrected full experiment and profile pass: 0 (`run.log`).
- First prelaunch harness guard exit: 1, preserved separately; no workload started (`prelaunch-failure.log`).
- The experimental runner code is SHA-256 `89d15715e7a50e66e9851469d3e04fa996e50fd0b2d277ffb973b85f9c1c0aaa`.
