# Subsystem state-machine evidence

Source inspected: `cf7a47e7b9430db4abebae68776f65f3e309624e`.
See the [skill router](../../README.md) for ownership and each skill's feature
map for exact source symbols, source-sync revision, assumptions and drive recipes.
The [interactive gallery](statecharts.html) includes nine Pele-rendered SVGs,
TLC reports, and expandable Mermaid text. SVGs and Mermaid sources also live
beside each owning skill's `evidence/report.md`.

## Coverage

| Model | Reachable states | Safety checked |
| --- | ---: | --- |
| ExecutionLifecycle | 17 | Strict policy retention and trusted leased process admission |
| AuthenticatedMetadata | 53 | Authentication and monotonic retained admission |
| MetadataDistribution | 28 | Matching authenticated envelope retained before confirmed distribution |
| BuildCoordination | 8 | Current live claim required for first publication |
| Materialization | 16 | Restore and runtime validation precede ready publication |
| LeaseReclamation | 12 | Active/ambiguous leases and dry runs preserve storage |
| ProcessExecution | 41 | Direct child reaped before lease release; result consistency |
| BundleFlow | 16 | Task success and output finalization both required for command success |
| ProviderRecovery | 14 | Only committed valid restore transactions become visible on replay |

All nine fixed configurations passed safety and deadlock checks with TLC 1.7.4.
Each new model passed fixed/broken/restored checks. The seven deliberate model
faults produced expected violations; `negative-report.md` gives each shortest
path. These are model sensitivity controls, not current product defects or
source mutation tests. Original admission and metadata fault/restoration checks
are retained in [admission.log](admission.log) and [metadata.log](metadata.log).
New checks retain configuration/model hashes, pinned checker digest, full logs,
and exit codes beside their owning skill. A timeout-path injection also verified
that the shared runner preserves partial diagnostics and records failure.

These finite contracts do not prove Go locking, signature algorithms, filesystem
power-loss atomicity, arbitrary process counts, external providers or liveness.
Provider deferred resolution and executable path rules have source/runtime
coverage but no separate finite model. Full conda solving, legacy holotree and
cloud flows remain outside formal coverage. The charts omit terminal stutters,
label-preserving steps, and the details each feature map explicitly abstracts;
individual reports count projection-hidden steps. No current-code bug path was
found to replay; the injected faults must not be reported as RCC vulnerabilities.

## Implementation evidence

[runtime-results.json](runtime-results.json) records 118 passing test/subtest
results and two skips, on Linux amd64 with Go 1.26.9 and `CGO_ENABLED=0`.
This includes actual subprocess lease/GC, crash-hook restart and execution
journeys. The fixtures were inspected for bounded child lifetimes and cleanup.
Generated assets were prepared using `uv run --no-project --with invoke invoke assets`;
no system Python packages or host OS packages were installed.

The optional v1 attack baseline needs a retained v1 binary and evidence directory;
metadata real CLI acceptance needs `RCC_ACCEPTANCE_BINARY`. Both skipped.
Full native bundle CLI acceptance was not run. No Windows/macOS, production,
release, hosted CI, source-mutation or full repository test acceptance is claimed.
Product source is unchanged. No commit, PR or publication was requested.

## Regenerate

From the repo root, prepare assets and use the selected Go toolchain as described
in `rcc-development`. Follow each skill's Doctor and Drive sections, capturing
results outside disposable fixture homes. For a new model:

```sh
task_dir=$(mktemp -d /tmp/codex-rcc-formal-XXXXXX)
python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-bundle-flow/models/BundleFlow.tla --out "$task_dir/check"
```

Use `$statechart` on each canonical model with its fixed configuration and view,
using a new `--out` directory. Copy the reviewed `chart.mmd`, `report.md`, and
`tlc.out` into its skill's evidence directory. For the original two models use
`evidence/ExecutionLifecycle` and `evidence/AuthenticatedMetadata` here. To render
with Pele 1.9.0 from a task-owned installation:

```sh
npm install --prefix "$task_dir/renderer" --no-audit --no-fund pele@1.9.0
node docs/skills/verify-execution-lifecycle/scripts/render_charts.mjs "$task_dir/renderer/node_modules/pele/dist/pele.js"
```

The renderer writes the nine SVGs and gallery at their established paths. Review
the generated files before replacing retained evidence. Remove task scratch when
the intended artifacts are retained. The feature recipes, not the gallery, decide
which real-code/platform gates are required after a source change.
