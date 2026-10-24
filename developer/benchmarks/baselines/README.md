# Linux overlayfs lifecycle baseline, 2026-10-10

`20261010-linux-overlayfs-mixed.json.gz` is the passed five-run RCC Environment
Artifact lifecycle report from `lifecycleBenchmark` (schema v3). It is gzip
compressed with `mtime=0` and no original filename. Decompressed JSON SHA-256:
`caa3bfc291665d637ae15ed9281354d1fed9e8e2b659f2f465a34ee6a129dfaa`.

- Measured RCC source: `e9a597c8a901e4c0e303acb606223ec2f0745b1b`
  (`v18.19.5`); `CGO_ENABLED=0`, Go 1.26.5, Linux amd64 candidate binary
  SHA-256: `fd50655095e6e74fc8f95bea9b16ae5bf83d4d53f4fed75f66a00136396348ce`.
- Harness SHA-256: `f4152df8495f7d75b91011d60d6b5805c4cefc5a13f8e536d48c74bb780c28f2`.
  The later commit adding this baseline does not change the measured candidate.
- Fixture source SHA-256 (sorted filename, NUL, content):
  `3485594108f512838aa8a5a2005fad7e1bf27e6c904b3c9e745e1c30f413f78b`.
  The JSON contains the exact `conda.yaml`, `robot.yaml`, and `task.py` inputs:
  Python 3.11.17 and PyYAML 6.0.2. Its `resolved_packages` records 30 Conda
  packages with name, version, build, archive SHA-256, and metadata SHA-256;
  transitive resolution is not locked.
- Host: Linux x86_64, kernel 6.18.44, overlayfs, five visible CPUs. Base Robot,
  native acceptance, and network acceptance processes ran concurrently.

One publication took 34.551 seconds and reported 5,077 objects and 71,498,447
uploaded bytes. The five clean provider acquisitions took 19.952, 25.222,
24.639, 24.209, and 22.901 seconds (median 24.209). Median verification was
4.582 seconds, aggregate `env exec` with Python import 0.887 seconds, warm
local acquisition 0.700 seconds, and provider-dead local acquisition 0.717
seconds. All recorded correctness gates passed. Full per-run CPU, peak RSS,
inventories, receipts, and unavailable evidence are in the JSON.

Inspect the archived report and verify its exact bytes:

```sh
gzip -dc developer/benchmarks/baselines/20261010-linux-overlayfs-mixed.json.gz > /tmp/rcc-125-baseline.json
sha256sum /tmp/rcc-125-baseline.json
```

To repeat the procedure, build `build/rcc` from the measured source commit with
Go 1.26.5 and `CGO_ENABLED=0`, then run:

```sh
rcc run -r developer/toolkit.yaml --dev -t lifecycleBenchmark
```

The toolkit writes `developer/tmp/lifecycle-baseline.json`. A new run will have
different timestamps, leases, and possibly Conda builds; compare its fixture,
binary, harness, and resolved-package provenance first. This shared-host
baseline supports no speedup comparison or storage/materializer selection.
It does not split build, Lift, transfer, Drop, startup, or import clocks, and
it does not cover Actions, macOS, Windows, or the candidate optimization matrix.
