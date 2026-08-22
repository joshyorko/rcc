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
