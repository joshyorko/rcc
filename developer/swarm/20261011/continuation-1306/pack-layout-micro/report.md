# Pack-layout small-object component measurement

This is a local filesystem layout component measurement, not an RCC production-format proposal, remote HTTP engine result, or end-to-end materialization benchmark.

The frozen input was the published provider fixture from RCC source `727c1ff8b679cdbbe12836e53c87734a1edb0071`. The indexed object set had 4,649 unique stored digests below 4,096 bytes, totaling 4,484,936 stored bytes. The source index is 1,491,800 bytes with SHA-256 `e587078b0b8d8bce38d4e665999b9a3a0d4d9fb1de1b87a8c3a4b223e71e50f6`. The frozen offset index is 525,841 bytes with SHA-256 `e8dca8fa7d069883441bd8616e49234d338ab5707b3cc05d6b3a5693b2ba99e2`.

The exact original compressed object bytes were concatenated in digest order into an owned scratch file. The pack is 4,484,936 bytes, SHA-256 `44d2341ac7cecf8a407e881bd4514e1f4fb6dd59530a4d172049d5c7e40f39a9`. Before timing, all 4,649 provider objects were rehashed and compared by size and digest, the full pack was hashed, and every indexed pack slice was checked. The source inventory included digest, size, mode, mtime, and inode; its before/after digest is identical: `f4ada405b7c6e71a5f19f214d6ca8fa52b6b34b151300c6afae103eca0903f67`. No provider file was written.

The five counterbalanced repetitions used the frozen order individual, pack, pack, individual, individual, pack, pack, individual, individual, pack. Both paths checked the same per-object SHA-256 and exact length in the timed region. The individual path opened each file separately; the pack path opened one file and sought each indexed slice. Buffers were at most 65,536 bytes. The OS page cache was warm from preflight and full untimed verification; no cache eviction or concurrent benchmark was used.

| Method | Median wall time per 4,649 objects | Median process CPU | Application opens | Application reads | Application seeks |
| --- | ---: | ---: | ---: | ---: | ---: |
| Individual files | 85.647 ms | 85.642 ms | 4,649 | 9,298 | 0 |
| Concatenated pack | 10.153 ms | 10.151 ms | 1 | 4,649 | 4,649 |

The application-level read count includes the individual path's EOF check; it is not a kernel syscall count. `/proc/self/io` was recorded per timed pass; the median rows each reported 4,485,037 `rchar`, zero `read_bytes`, and zero writes. This reflects warm-cache reads on this host. RSS high-water was 24,813,568 bytes. The corrected run completed in 0.98 seconds total, below the 120-second total and 30-second per-phase limits. A one-object 99-byte scratch copy rejected both truncation by exact-size check and corruption by SHA-256 check; it was restored to its original digest.

The first completed measurement invocation failed final admission because its verbose receipt pushed the owned namespace to 11,427,840 allocated bytes, exceeding the 10,485,760-byte cap. Those measurements are retained only as diagnostics in `results-original.json.gz` and are not accepted. The compact corrected invocation passed the predictive and final guards: recursive allocated size including directories is 5,566,464 bytes in the final evidence directory against the 10,485,760-byte cap, with 2,778,439,680 free bytes against the 2,684,354,560-byte floor. Its command exit was 0. The exact stdout and exit are in `run-retry01.log`; structured metrics are in `results-retry01.json`.

Reproduce the accepted run with:

```sh
python3 /workspace/work/rcc-swarm/evidence/continuation-20261011/pack-layout-micro/run_experiment_retry01.py
```

The runner deliberately refuses to overwrite its receipt or create another primary pack. Root independently verified the archived first result byte-for-byte, and revalidated the primary pack, all 4,649 slices, index closure, original inventory, and restored negative copy before admitting the corrected run.
