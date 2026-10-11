Parent: #82
Generic adapter contract: #143
Depends on: #129, #130
Informs: #83, #86, #87, #90, #134, #138, #139, #142, #144
Cross-project implementation: `joshyorko/rcc#118`, `joshyorko/rcc#120`

## Outcome

Use RCC/Holotree as the **first full Capability Runtime Adapter** for Actions workers, consuming RCC's own supported environment identity, provider, materialization, lease, and execution lifecycle rather than treating RCC as a one-time bootstrap command or rebuilding RCC services inside Actions Runtime.

RCC is the default bundled adapter for compatible Action Packages and Robot Task Packages because it already provides:

- deterministic environment specifications and Holotree reuse;
- Environment Artifact manifests and cryptographic provider identities;
- local, offline, and warm-cache operation;
- environment publication/acquisition;
- worker-local materialization and process-owned leases;
- bounded execution through a fresh handle/`env exec` contract;
- Task Package/`robot.yaml` execution semantics;
- `rcc cache serve` and provider profiles for sharing immutable environment content.

This issue owns the **RCC-specific adapter integration**. #143 owns the backend-neutral Runtime Adapter contract. A future uv, raw Python, direct Robot Framework, or other adapter must be possible without pretending it has RCC-specific Environment Artifact fields.

## Architectural boundary

```text
Actions Runtime
  durable Package / Deployment / Run / Attempt / bindings / policy
                         |
                         v
              Capability Runtime Adapter (#143)
                         |
                         v
                    RCC adapter
       resolve -> acquire -> verify -> materialize
            -> lease -> execute -> release
                         |
                 local or provider-backed
              Holotree / Environment Artifacts
```

RCC owns environment reconstruction, content integrity, provider behavior, local materialization, execution leases, and RCC task launch semantics.

Actions Runtime owns:

- capability/package/deployment selection;
- authorization and binding references;
- durable Run/Attempt state and fencing;
- worker eligibility and scheduling;
- Work Item business lifecycle;
- output artifact metadata/authorization;
- UI/CLI/control-plane behavior.

RCC does **not** become Run authority, Work Item authority, the worker scheduler, or a required long-running daemon.

`rcc cache serve` is an Environment Artifact provider/cache service. Legacy `rccremote` is a compatibility cache-export protocol. Neither is a distributed Run coordinator or remote worker control plane.

## Design correction and archaeology

The inherited Action Server path uses RCC mainly as:

- `rcc ht hash`;
- `rcc holotree variables`;
- a source of cached `CONDA_PREFIX`, `PYTHON_EXE`, PATH, and environment dictionaries;
- a process-global helper guarded by local mutexes.

After bootstrap, Action Server owns the process lifecycle and treats local activation variables as the effective environment contract.

The archived `yorko-io/control-room` backend went in the opposite direction and duplicated too much above RCC:

- a large `RCCService` for robot detection, pulling, environment creation, execution, logging, and simulation;
- a separate `HolotreeService`, catalog API, cache, S3 synchronization, and environment UI;
- custom Robot distribution and worker agents;
- simulated success when RCC or remote deployment was unavailable;
- package/task/runtime state tied to local paths;
- Work Item and worker-capability inference based on names/source scanning.

Preserve Control Room's product intent—environments, packages, workers, runs, artifacts, diagnostics—but do not recreate its parallel RCC/Holotree implementation.

The new RCC branch already supplies the missing lower-level seam. Actions should consume it.

## RCC product surface to consume

Target the supported contract released by `joshyorko/rcc#118/#120`, including machine-readable equivalents of:

- Environment Specification resolution;
- Environment Manifest/Object Index identity;
- `rcc env publish`;
- `rcc env acquire`;
- `rcc env exec`;
- local content/materialization records;
- process-owned execution leases;
- named provider profiles;
- filesystem and HTTP provider behavior;
- lazy warm reuse with unavailable provider/credentials;
- `rcc cache serve`;
- existing `rcc run`, `rcc holotree variables`, v12 catalog, bundle, export/import, and `rccremote` compatibility where required during migration.

Do not bind Actions to CLI presentation text. Prefer supported JSON/machine contracts and explicit RCC feature/version negotiation.

Do not invoke hidden/removed RCC commands merely because a historical Action Server or Control Room branch used them.

## RCC adapter descriptor

The RCC implementation of #143 advertises at least:

- adapter kind `rcc`;
- RCC version/build and adapter contract version;
- supported RCC feature set;
- supported package/task formats and capability kinds;
- supported platforms/architectures/catalog readers;
- builder and Environment Artifact compatibility;
- local/offline/provider-backed preparation modes;
- provider profile capability and authentication requirements;
- Task Package semantics;
- cancellation/process-tree behavior;
- artifact/log/result behavior;
- supported execution placements;
- truthful limitations.

RCC availability is not a boolean inferred from `rcc version` alone. Preflight verifies every required feature for the selected package/plan.

## RCC-specific object model

These concepts remain first-class inside the RCC adapter and its typed receipts.

### Environment Specification

Canonical normalized dependency/build input derived from the Package Revision, including relevant platform, architecture, RCC/builder compatibility, and build policy.

### Environment Artifact

Verified platform-specific manifest/object closure produced or acquired through RCC. It is identified independently of provider URL, local cache path, or mutable tag.

### Local Materialization

Worker-local Holotree/runtime state derived from the exact artifact. It is cache/placement state, not durable Run identity.

### Environment Lease

Process-owned usage reference protecting the materialization/content while a process generation or Attempt uses it.

### RCC Execution Handle / receipt

Fresh bounded execution context produced under the lease. It may contain derived interpreter/environment/path values, but those are not portable identities.

Actions may store/reference:

- Environment Specification digest;
- Environment Artifact digest;
- platform/builder/feature compatibility;
- provider profile/reference under Deployment policy;
- RCC adapter/version evidence;
- materialization/cache class as bounded Attempt evidence.

Actions must not store as durable identity:

- `PYTHON_EXE`;
- `CONDA_PREFIX`;
- `ROBOCORP_HOME`;
- Holotree space/materialization paths;
- provider URLs;
- RCC process IDs.

## Required RCC adapter lifecycle

Implement #143 semantics through RCC-owned operations:

1. **inspect/resolve** package/task and normalized environment input;
2. **select/acquire** an exact compatible Environment Artifact or build locally under policy;
3. **verify/materialize** using RCC;
4. **lease** for the complete process/process-generation lifetime;
5. **execute** through a fresh RCC-supported handle/exec contract;
6. **stream/collect** bounded events, logs, result, and declared artifacts;
7. **cancel/reap** the full process tree under Attempt fencing;
8. **release** only after process reaping and output publication are complete;
9. **repair/evict** through RCC policy without invalidating durable identity.

Actions may wrap these operations with Run state/evidence. It must not implement another authoritative lease table, object index, Holotree catalog store, or provider-profile database.

## Local and provider-backed modes

### Zero-configuration local

```text
Package/capability
  -> RCC adapter
  -> local resolve/acquire/build
  -> local Holotree materialization + lease
  -> local process
```

No server, registry, login, container engine, or Kubernetes installation is required.

### Provider-backed worker

```text
Environment Artifact identity
          |
    RCC provider profile
          |
   rcc cache serve / HTTP provider / local mirror
          |
      worker-local RCC
 acquire -> verify -> materialize -> lease -> exec
```

Workers keep local caches. Provider/cache warmth is an optimization. The Run remains observable/controlable through another Actions Runtime replica because Run/Attempt authority is not in RCC or the provider.

Provider failure must still allow verified warm execution when RCC's contract permits it. Missing required content must fail as preparation unavailable, not trigger a different runtime adapter.

## Autoreload and process generations

Preserve current developer behavior.

### Source-only change

When the normalized RCC Environment Specification is unchanged:

- reuse the same verified Environment Artifact/materialization class;
- acquire/retain leases for old and new process generations correctly;
- import/validate a new Action source generation;
- atomically route new calls/catalog discovery to it;
- let old calls drain;
- release old generation lease only after complete process reaping;
- perform zero dependency resolution, environment build, or provider calls.

### Environment-changing change

- resolve a new RCC Environment Specification;
- acquire/build/verify/materialize beside the old generation;
- keep the old leased while in-flight work drains;
- validate the new process generation before activation;
- retain the old generation on failure;
- atomically activate success;
- release old resources after drain.

No in-flight call silently changes environment.

## Worker integration

#90/#138 workers advertise RCC adapter compatibility and cache locality as scheduling input:

- RCC/adapter contract version;
- platform/architecture/catalog readers;
- supported builder/artifact features;
- provider reachability where required;
- verified Artifact/materialization locality hints;
- preparation capacity/disk pressure;
- Task Package execution capability.

Registration is not authorization. A warm local path cannot substitute a different Environment Artifact or Package Revision.

The RCC adapter may run through local process, container, or independent-worker placement. Placement is orthogonal to the runtime adapter and is proven in #142.

## No lock-in rule

RCC-specific capabilities are represented as namespaced adapter features/receipts behind #143.

Generic Run/Package/Deployment schemas must not require:

- an RCC blueprint;
- a Holotree catalog;
- an RCC provider profile;
- a Holotree lease;
- an RCC binary.

A uv or direct Robot Framework adapter can satisfy the same generic lifecycle with its own typed runtime plan and weaker/stronger guarantees. There is no automatic fallback between them.

The generic interface is not considered stable merely because RCC works. At least one second adapter must pass the common conformance subset before the abstraction is declared backend-neutral.

## Trust and security

RCC/Holotree provides reproducibility and content integrity, not malicious-code sandboxing.

- never share writable live environments across mutually untrusted executions by default;
- verify artifacts/content before execution;
- prevent package-controlled cache/provider/config paths;
- keep secrets/bindings out of Environment Artifacts and reusable caches;
- use provider references/auth environment-variable names, never persisted credential values;
- register redaction before child output begins;
- keep worker/container/VM isolation in Worker Profile and execution placement policy;
- reject stale Attempt result/artifact publication independently of RCC process state.

## Performance and evidence

Measure phases separately:

- specification resolution;
- local build;
- provider publication/acquisition;
- verification;
- materialization/restore;
- lease and execution-handle creation;
- process startup;
- warm reuse;
- release/cleanup.

Expose compact evidence without making local paths primary metrics dimensions.

## Conformance and acceptance

Use #139 and the #143 adapter suite.

1. A current package resolves, acquires/materializes, leases, and executes through the RCC adapter locally.
2. Actions no longer treats cached `PYTHON_EXE`/`CONDA_PREFIX` as authoritative environment identity.
3. One environment is published/served through `rcc cache serve`, acquired by a clean worker, verified, materialized, leased, and executed without Actions implementing the provider/cache protocol.
4. Warm execution succeeds with provider unavailable when RCC's contract says all required verified content is local.
5. Source-only reload makes zero build/provider calls and preserves in-flight generations.
6. Environment-changing reload activates atomically and rolls back safely on build/import failure.
7. RCC feature/version/platform mismatch fails clearly; no simulation or backend fallback occurs.
8. Cancellation reaps the full process tree before lease release.
9. Secret values are absent from manifests, plans, command lines, cache metadata, logs, and receipts.
10. Local source, wheel, and frozen Runtime distributions consume the same supported RCC contract.
11. Generic Run/Package schemas remain valid for a conforming non-RCC adapter.

## Non-goals

- Making RCC a required daemon or the only runtime adapter.
- Reimplementing RCC Environment Artifact/provider/cache/lease internals in Actions.
- Replacing #83 Run/Attempt ownership or #90 worker scheduling with RCC locks.
- Requiring Kubernetes, containers, OCI, NFS, or a central build service.
- Treating RCC as a security sandbox.
- Completing every RCC storage/materialization optimization before the adapter contract is proven.