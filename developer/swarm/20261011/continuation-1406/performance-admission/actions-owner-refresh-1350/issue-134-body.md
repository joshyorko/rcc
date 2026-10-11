Parent: #133
Grounds the generic contract: #143
Related: #82, #90, #130, #144, `joshyorko/rcc#118`, `joshyorko/rcc#120`
Informs: #83, #129, #135, #139

## Outcome

Land the **first collapsed/local Actions consumer proof** of RCC Environment Artifacts and RCC-owned execution lifecycle before building the distributed worker fleet or freezing a supposedly generic adapter abstraction.

Replace the inherited model:

```text
RCC materializes once
 -> Actions caches PYTHON_EXE / CONDA_PREFIX
 -> Actions treats local paths as the environment contract
 -> Actions independently owns every later preparation decision
```

with:

```text
current package environment input
        |
provisional RccRuntimeAdapter seam
        |
RCC resolve/acquire/verify/materialize/lease
        |
fresh RCC-supported execution handle / env exec
        |
existing local Action import/process generation
        |
reap -> publish outputs -> RCC release
```

Preserve the excellent local developer experience. Do not require PostgreSQL, remote workers, Kubernetes, a remote provider, a new Package v3 authoring format, or a second runtime adapter for this first releasable slice.

## Proof-before-abstraction sequencing

This issue deliberately proves the concrete RCC path first.

- Implement the narrowest local adapter seam needed to consume RCC's supported contract.
- Keep adapter-specific details namespaced and explicit.
- Record every accidental Actions assumption discovered during integration.
- #143 then stabilizes the backend-neutral Runtime Adapter contract and proves it through a second implementation.
- Do not design a large plugin framework before the first real RCC Action process generation works end to end.
- Do not allow the first implementation to leak RCC-only fields into generic Run/Package identity merely because it landed first.

RCC is the first adapter, not the architecture.

## Why this remains a separate issue

#133 owns the complete RCC adapter/substrate relationship. #143 owns the long-term adapter-neutral contract. #90 owns independent workers.

This issue is the bounded bridge from today's current-v2 Action Server to the new RCC consumer contract:

- one Runtime process;
- current package directory authoring;
- current Action import/process pool;
- local SQLite/filesystem behavior;
- supported bundled/external RCC;
- real Environment Artifact identity and execution lease;
- no distributed topology prerequisite.

It must be independently reviewable, testable, releasable, and useful even if all later distributed work pauses.

## Historical and Control Room correction

Legacy RCC/Holotree already supported local reuse, shared mode, ZIP export/import, and `rccremote` delta pull. The new work must not claim Actions invented environment transfer.

The archived Control Room later added its own Holotree catalog service, cache, S3 sync, environment selector, RCC service, worker runner, and simulation paths. That product work is useful evidence of what operators need to see, but it also demonstrates what Actions should not duplicate now that RCC exposes Environment Artifacts, providers, leases, `env exec`, and `cache serve` directly.

This issue must not add a parallel:

- Holotree catalog database;
- object-index implementation;
- Environment Artifact provider server;
- provider-profile store;
- authoritative lease database;
- RCC simulation mode;
- local-path-based environment registry.

## RCC contract consumed

Target the stable supported surface delivered by `joshyorko/rcc#120`, using machine-readable equivalents of:

- Environment Specification resolution;
- Environment Artifact/Manifest identity;
- local acquire/build and verification;
- worker/process-owned lease;
- fresh execution handle or `rcc env exec`;
- release after process lifecycle completion;
- provider profile resolution where configured;
- lazy verified warm reuse without provider/network access;
- provider-backed acquire through `rcc cache serve` or another supported RCC provider;
- exact RCC feature/version compatibility evidence.

Existing `rcc run`, `rcc holotree variables`, v12 catalogs, bundles, export/import, and legacy `rccremote` may remain compatibility paths inside RCC. Actions should call the new supported consumer seam rather than reconstructing their internals.

Do not parse human CLI output or invoke hidden RCC commands.

## Provisional RccRuntimeAdapter seam

Provide the smallest typed local interface with semantics equivalent to:

- inspect/normalize current package environment input;
- resolve the RCC Environment Specification;
- acquire/build and verify the exact Environment Artifact;
- materialize and acquire a process-generation-owned lease;
- obtain a fresh execution handle or execute through supported RCC machinery;
- expose bounded cache/provider/provenance diagnostics;
- release only after every child is reaped and required outputs are published;
- fail clearly on missing RCC, missing feature, provider auth, incompatible platform, corrupt artifact, or preparation failure.

The adapter must not merely rename an activation environment dictionary.

The first process pool integration may still spawn/manage Python Action workers where that remains the supported Actions execution path. RCC environment identity, verification, lease, and handle derivation remain authoritative through the complete process generation.

## Current v2 package compatibility

Derive the first RCC Environment Specification from current spec-v2 `package.yaml` directories.

- validate and normalize environment declarations deterministically;
- keep mutable source generation separate from Environment identity;
- do not call the mutable directory a published Package Revision;
- keep the receipt migratable to #130/#135 immutable Package Revisions;
- apply request bindings/secrets only at execution, never to the Environment Artifact;
- preserve current source layout and authoring commands;
- support current Action import/discovery out of process in the exact leased environment.

## Autoreload and process generations

Preserve the workflow where a developer or model edits Actions and the running Runtime reloads without a manual restart.

### Source-only change

When the normalized RCC Environment Specification is unchanged:

1. reuse the exact verified Environment Artifact/materialization class;
2. retain/acquire the correct process-generation lease;
3. create and validate a new Action source/import process generation;
4. atomically route new catalog/calls to it;
5. let old calls drain or cancel under policy;
6. fully reap old children;
7. release only the old generation's lease/reference;
8. perform **zero** environment build, solver, provider, or cache-server calls.

### Environment-changing change

When dependency/environment input changes:

1. resolve a new Environment Specification;
2. acquire/build/verify/materialize it beside the old generation;
3. keep the old environment leased while old calls drain;
4. import/validate the new Actions out of process;
5. leave the old generation active on preparation/import failure;
6. atomically switch new calls/catalog to the ready generation;
7. release the old lease only after all associated processes are reaped and outputs complete.

An in-flight call never silently switches runtime or environment.

## Process-pool integration

Adapt the existing local Action process pool as the first execution-placement consumer.

Required behavior:

- process generation is keyed by source generation + selected runtime kind `rcc` + Environment Artifact identity + safe binding class;
- a fresh RCC-supported execution context is obtained at every process/process-generation boundary;
- warm process reuse cannot cross incompatible package/environment/binding/Workspace identity;
- the RCC lease lives for the complete child/process-generation lifetime;
- cancellation, terminate/kill escalation, and child reaping happen before release;
- stdout/stderr remain bounded/redacted;
- no reusable record contains request secret values;
- Runtime restart reacquires by RCC identity rather than trusting a stale executable path;
- verified local warm behavior continues when a configured provider is temporarily unavailable;
- RCC unavailability never falls back to simulation, raw Python, or another adapter.

## Migration from `env-info/<hash>.json`

Define a finite migration path:

- legacy cached activation files are not authoritative after the new adapter activates;
- an optional compatibility reader may inspect a legacy entry only to locate/revalidate state through RCC;
- every reused entry must be converted to current RCC identity/evidence;
- stale/missing executable paths trigger RCC reacquisition, not false readiness;
- no indefinite dual authority exists between `env-info` and RCC lifecycle state;
- cleanup and rollback behavior are explicit;
- old Runtime binary compatibility follows the declared release window.

## Runtime distribution and RCC lookup

Prove the same flow in:

- source development;
- wheel installation;
- frozen/PyInstaller Runtime binary;
- bundled/downloaded supported RCC;
- explicit external RCC override for development/testing;
- offline provider-free local mode;
- named provider profile with prepublished Artifact;
- Linux-first accepted matrix, followed by RCC #121 platform evidence.

RCC binary/version/feature negotiation must be machine-readable and fail closed.

## Provider-backed local proof

Although a provider is not required for normal local use, include one bounded proof that Actions consumes rather than reimplements RCC's remote capability:

1. publish an Environment Artifact through RCC;
2. serve it with `rcc cache serve` or use another supported RCC provider;
3. clear the second consumer's local materialization/content as appropriate;
4. acquire/verify/materialize through RCC;
5. run the same Action generation;
6. demonstrate a later verified warm execution with the provider unavailable and zero provider calls.

This is environment distribution, not remote Run execution.

## Observability

For each package/process generation expose compact evidence:

- selected runtime kind `rcc`;
- RCC and adapter contract version/features;
- Environment Specification/Artifact digest;
- provider profile/reference without URL credentials;
- local warm/provider/build acquisition class;
- resolve/build/acquire/verify/materialize/lease/activation timings;
- source generation/catalog revision;
- process-generation/lease identity as bounded local evidence;
- failure phase and actionable diagnostic;
- redaction/truncation indicators.

Do not make local paths high-cardinality primary metrics or portable IDs.

## Required tests

- deterministic source-versus-environment change classification;
- source-only reload with zero build/provider calls;
- generation activation/drain race;
- environment preparation/import failure rollback;
- process spawn/cancel/reap/lease ordering;
- Runtime restart and stale legacy cache;
- provider-dead verified warm path;
- `rcc cache serve` A→B consumer proof;
- corrupt/tampered/incompatible artifact rejection;
- secrets absent from environment/artifact/cache/argv/logs;
- missing/unsupported RCC classification with no fallback/simulation;
- current v2 package compatibility;
- source/wheel/frozen binary acceptance.

## Acceptance

1. Start the current local Runtime against a normal v2 package and resolve/acquire/materialize its real RCC Environment Artifact.
2. Execute an Action through the existing process pool while holding the RCC-owned lease/handle contract for the full process generation.
3. The Runtime no longer treats persisted `PYTHON_EXE`, `CONDA_PREFIX`, or a local Holotree path as authoritative identity.
4. Source-only reload changes the catalog/process generation with zero environment/provider work.
5. Dependency change creates a new environment beside the old while an in-flight call stays on the old generation.
6. Preparation or import failure leaves the old generation active.
7. Successful new generation activates atomically and releases the old lease only after drain/reap.
8. Restart with provider unavailable reacquires and executes from verified local RCC state.
9. Missing local materialization reacquires/rebuilds the same Environment identity instead of trusting stale paths.
10. One `rcc cache serve`/provider A→B proof passes without an Actions-owned cache server.
11. Missing/incompatible RCC fails explicitly with no simulation or adapter substitution.
12. Frozen binary runs the same lifecycle.
13. The implementation exposes a narrow seam that #143 can test and generalize without making generic schemas RCC-only.

## Non-goals

- Independent remote worker scheduling.
- Durable Run/Attempt database fencing.
- Full Package v3 projection/registry.
- Implementing uv/Python/Robot Framework adapters in this issue.
- A general plugin framework before the RCC proof.
- Kubernetes or a broker.
- RCC FUSE/zstd/packfile optimization.
- Removing safe current process-pool performance optimizations.
- Making every local Action call asynchronous.