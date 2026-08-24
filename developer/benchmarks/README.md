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

`lifecycle.py` requires an exact executable RCC candidate and records raw,
sorted-key JSON for the deterministic `python-package-v1` fixture. It invokes
`cache serve`, `env publish`, clean `env acquire`, lifecycle verification,
`env exec` startup/import, warm acquire, and provider-dead acquire in isolated
producer/consumer `ROBOCORP_HOME` directories. It captures RCC JSON receipts,
digests, cache classes, wall/CPU/RSS metrics, platform/filesystem context, and
correctness gates. Only consumer reload evidence and the absent public artifact
GC command are marked unavailable; no optimization winner is inferred.

Run an offline fixture baseline from the repository root:

```sh
python3 developer/benchmarks/lifecycle.py \
  --binary ./build/rcc \
  --rcc-sha "$(git rev-parse HEAD)" \
  --output tmp/lifecycle-baseline.json
```

Keep the output with the exact RCC candidate. For a consumer run, pass
`--consumer-sha` and `--binary` as additional provenance. The output is raw
evidence, not a storage or materialization decision.
