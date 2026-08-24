# Holotree benchmark suite

`holotree.py` is an offline baseline harness for the many-small-file fixture.
It emits raw JSON with wall time, CPU time, file counts, byte counts, a stable
inventory digest, and host context. Retain every output with the RCC commit SHA.

```sh
python3 developer/benchmarks/holotree.py --files 1000 --repetitions 5 \
  --output tmp/holotree-baseline.json
```

The harness measures filesystem inventory, copy/materialization, and
verification only. End-to-end RCC timings must be added as fixture-specific
phases; they must not be inferred from this baseline.

No materializer or storage optimization is selected by this change. FUSE,
packfiles, reflinks/hardlinks, and encoding changes remain experiments until a
named workload shows repeated material improvement while preserving identity,
verification, fallback, and rollback behavior.

## Lifecycle evidence (schema v2)

`lifecycle.py` records raw, sorted-key JSON for representative fixture IDs
`many-small-files-v1` and `python-package-v1`. It includes candidate IDs,
platform/filesystem context, correctness gates, and phase IDs for publish,
acquire, verify, materialize, lease, startup, import, warm, provider-dead, and
GC. Consumer/provider phases are explicitly marked `unavailable` unless a real
consumer runner supplies evidence; no optimization winner is inferred.

Run an offline fixture baseline from the repository root:

```sh
python3 developer/benchmarks/lifecycle.py \
  --rcc-sha "$(git rev-parse HEAD)" \
  --output tmp/lifecycle-baseline.json
```

Keep the output with the exact RCC candidate. For a consumer run, pass
`--consumer-sha` and `--binary` as additional provenance. The output is raw
evidence, not a storage or materialization decision.
