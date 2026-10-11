# Independent retained RCC packed-provider run review

**Disposition: PASS for this private correctness/pipeline diagnostic.** This review is read-only and does not authorize or claim another run. No benchmark, build, or profile regeneration was performed.

## Identity and run status

- Frozen RCC base: `7945f956b428359928908a9f42e36417b113dafd`.
- Source: `pack-e2e/source/main.go`, SHA-256 `c1dd426967511e436bc408ad168704685e04359d6be2064e5ddb250d8c1f4b42`.
- Build binding: `build-binding-4.json` binds that exact source SHA to `/workspace/rcc-successor-tools/pack-e2e`, SHA-256 `e2c0cca73d176c90f585011855286c92057345ce7448868afdb02e84fb272979`. `build-4/result.json` reports Go 1.26.9 build exit 0. The bounded runtime record invokes that binary path.
- `run-1/result.json` reports return code 0, no stop condition, 12.04 seconds elapsed, 46,342,144 bytes maximum sampled process-tree RSS, and no final observed live identities. The bounded runner result stdout is empty and launcher stderr is empty; `launcher.stdout` contains the JSON summary with the same run result.
- Retained measurements contain six successful arm rows and a final PASS row. All 12 listed controls pass, followed by `negative-controls-complete` count 12. The 12 are one independent-buffer behavior check plus 11 rejection checks; they are not 12 distinct malformed-input rejection cases.

## Independent content and lifecycle checks

All six worker rows use artifact `sha256:f5b34a80482d73d38928c05d93c2df799b6f9bec00ec94395b16e1963cf627a1` and materialization `c2fd1aa_b35c539_42e7bd8c`. Their child results each report 512 workload files and aggregate SHA-256 `cacb33eadcd7c2abb40d6930a6943327b54223af3b51833c5f67a13b6521dce8`. I independently hashed all 513 fixture files, including the `bin/python` wrapper, under each materialized output: 513/513 matched size and SHA in every arm (3,078 file checks, zero mismatches). Each worker row records `leaseReleased: true`; the six lease directories remain present and empty.

I independently recomputed the immutable source/provider/pack/producer/fixture tree hash as `8578cc809d886db90c078a406d6b73c112dc9ce2b773412185d837547ac3d27c`, matching the harness completion row. I also independently checked the manifest and object-index hashes against their prepared identities and verified all 513 packed frames against the standard object index: exact digest set, sizes, contiguous offsets, per-frame hashes, whole-pack size/hash, and pack-index hash all match.

The retained preflight and postflight records preserve the configured outer root and coordinator home at 0700 owner/group 1000:1000 and global diversion at 0750 owner/group 1000:1000. Postflight also lists the producer home, workers root, all six worker roots, and all six RCC homes at 0700 owner/group 1000:1000. Current retained filesystem inspection shows filesystem-provider, packed-sidecar, and catalog-root directories also at 0700 owner/group 1000:1000. Original strict-0700 baseline failure remains preserved.

## Timing interpretation

Cold pipeline here is the recorded provider setup (including packed-provider construction), cold `Acquire`, and `ExecuteWithStreams`. It excludes preparation and is not a full machine-cold or OS-cache-cold benchmark.

| Arm | Cold pipeline median | Three-run range | Provider setup median | Warm Acquire median |
| --- | ---: | ---: | ---: | ---: |
| Filesystem provider | 1,610.1 ms | 1,604.8–1,701.6 ms | 2.18 ms | 105.8 ms |
| Packed provider | 1,642.7 ms | 1,503.4–1,750.2 ms | 25.33 ms | 113.1 ms |

Paired filesystem-minus-packed cold-pipeline differences are −32.6 ms, +198.1 ms, and −145.4 ms. The direction changes across the three pairs and the spread is much larger than the medians' difference. These observations do not establish a winner. Packed setup is consistently higher in this run; `Acquire` dominates both pipelines. Warm timing measures only a second `Acquire` against the already materialized local cache with an untouched provider. It does not include a warm `ExecuteWithStreams` and must not be described as end-to-end warm execution or persistent-worker reuse.

## Audit notes and limits

The completion JSON serializes its schedule as six empty objects because the local schedule struct's fields are unexported; the six arm rows and six scheduler-arm rows retain actual pair/arm/order, so the sequence is recoverable from raw JSONL. The process supervisor is sampled and explicitly does not provide a hard memory/disk quota or detect every sub-100 ms/unobserved escaped child. The retained no-live result is specific to this run.

This is a synthetic Linux/amd64 host-Python fixture using the real RCC acquisition, materialization, compatibility, and execution APIs. It is not evidence for Actions issue/PR acceptance, shipping behavior, portability, the historical 4,649-object corpus, full cold-cache performance, or a default/winner decision. The already retained profiles can be analyzed separately; none were regenerated here.
