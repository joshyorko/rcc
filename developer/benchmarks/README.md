# Holotree benchmark suite

`holotree.py` is an offline baseline harness for the many-small-file fixture.
It emits raw JSON with wall time, CPU time, file counts, byte counts, a stable
inventory digest, and host context. Retain every output with the RCC commit SHA.

```sh
python3 developer/benchmarks/holotree.py --files 1000 --repetitions 5 \
  --output tmp/holotree-baseline.json
```

The harness measures filesystem inventory/hash, Python copytree, and content
verification only. Its copy phase records file payload bytes and excludes
inventory work. End-to-end RCC timings must be measured separately; they must
not be inferred from this baseline.

No materializer or storage optimization is selected by this change. FUSE,
packfiles, reflinks/hardlinks, and encoding changes remain experiments until a
named workload shows repeated material improvement while preserving identity,
verification, fallback, and rollback behavior.

## Lifecycle evidence (schema v3)

`lifecycle.py` requires an exact executable RCC candidate and records raw,
sorted-key JSON for the pinned `python-package-v1` fixture. It invokes
`cache serve`, `env publish`, clean `env acquire`, lifecycle verification,
`env exec` startup/import, warm acquire, and provider-dead acquire in isolated
producer/consumer `ROBOCORP_HOME` directories. It captures RCC JSON receipts,
digests, cache classes, per-command wall/CPU/peak RSS metrics where supported,
stored and materialized file/byte inventories, platform/filesystem context, and
correctness gates. The raw output includes the fixture files, source digest,
candidate binary SHA-256, and benchmark harness SHA-256. Python and PyYAML
versions are pinned in the source fixture; the transitive Conda package build
set remains resolver-dependent. The report records package name, version,
build, optional archive SHA-256, and metadata SHA-256 from the producer's
`conda-meta` directory when present. Five isolated clean
consumer runs are the default; publication is measured once and shared across
those runs. The producer environment may reuse package download caches, so
publication is not a cold network-resolution measurement.

Run a loopback-provider fixture baseline from the repository root (the initial
Conda package build may require network access):

```sh
python3 developer/benchmarks/lifecycle.py \
  --binary ./build/rcc \
  --rcc-sha "$(git rev-parse HEAD)" \
  --repetitions 5 \
  --output tmp/lifecycle-baseline.json
```

The contained equivalent is `rcc run -r developer/toolkit.yaml --dev -t
lifecycleBenchmark` after building `build/rcc` from the recorded SHA. Keep the
output with that binary. The contained task reads the checkout SHA directly
with `git rev-parse HEAD`; RCC task shell commands do not expand `$(...)`.
`wait4` supplies per-RCC-child CPU and peak RSS on
Unix; peak RSS is not a sum of the complete process tree. Windows records these
fields as unavailable. `env exec` is an aggregate RCC plus child measurement;
the harness cannot separately time Python startup, import, Drop, lease creation,
transfer, build, or Lift. It does not exercise Actions calls, reload, a warm
worker pool, or GC. Its `uploadedBytes` receipt measures publication, while
network download bytes are unavailable. The synthetic `holotree.py` copy is
Python filesystem behavior, not RCC Drop. Run platform-native fixtures before
making any default materializer or storage recommendation; the old PR #64
speedup figures are not current evidence.
