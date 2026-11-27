# Micromamba loopback channel benchmark

Run the deterministic local channel probe with real shipped executables:

```sh
python3 scripts/micromamba_channel_benchmark.py --runs 5 \
  --binary micromamba-2.10.0=/path/to/micromamba-2.10.0 \
  --binary micromamba-2.5.0=/path/to/micromamba-2.5.0 \
  --receipt /tmp/micromamba-benchmark.json
```

The harness starts a temporary loopback HTTP channel with valid empty flat
`repodata.json`, records request paths and wall time, and gives every run a new
temporary Mamba root prefix. It makes no external requests and does not reuse
package caches. `--binary` may be repeated; each entry is `label=absolute-path`.
The JSON receipt records binary digests, command results, request logs and
median/min/max durations. These timings cover an empty metadata search only;
they do not measure package installation or establish relative performance for
real environments.

## Local receipt, 2026-11-26

The Linux x86_64 run used five isolated invocations per executable. Shipped
binary digests matched `tasks.py`'s embedded Linux amd64 pins:

| Binary | SHA-256 | Median | Range |
| --- | --- | ---: | ---: |
| micromamba 2.10.0 | `701f39c06f8e6a670a0e186c8479b9e5c0502a2f0cda6cef65d83bdd2875e8bd` | 13.34 ms | 12.59–15.03 ms |
| micromamba 2.5.0 | `c04571cfb0750e5432d530a3068b8fcd232ebed3133358e056e59a90b9852b00` | 11.66 ms | 10.54–12.48 ms |

Both versions requested `/linux-64/repodata.json` and `/noarch/repodata.json`
and returned success. The checked-in machine-readable evidence is
[`micromamba-loopback-2026-11-26.json`](micromamba-loopback-2026-11-26.json).

This fixture qualifies flat metadata requests and execution isolation only. It
does **not** serve valid shard metadata, demonstrate shard selection, profile
opt-out, sharded-to-flat fallback, custom RCC profile transport, or establish
real-channel performance. Those remain separate acceptance gates; do not infer
them from this receipt.
