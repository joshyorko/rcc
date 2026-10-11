Parent: #82
Grounded by: #134, `joshyorko/rcc#118`, `joshyorko/rcc#120`
Related: #83, #87, #90, #129, #130, #132, #133, #138, #139, #142, #144, #145, #146, #147

## Outcome

Define one typed **Capability Runtime Adapter** contract, one immutable **Resolved Runtime Plan**, and one shared **execution-admission/preflight contract** so Actions Runtime can use RCC deeply as the first proving backend without making RCC the permanent architecture.

Supported adapter kinds may include:

- `rcc` — first/default and richest implementation, including Task Package semantics, Holotree Environment Artifacts, provider profiles, `rcc env ...`, and `rcc cache serve`;
- `uv` — locked Python/project execution using `pyproject.toml`, `uv.lock`, managed Python, and uv semantics;
- `python` — explicitly selected trusted system/managed/venv Python with deliberately weaker portability guarantees;
- `robot-framework` — direct Robot Framework suite/task execution under an explicitly resolved runtime environment;
- future operator-installed adapters added as independent conformance slices.

RCC is neither a fieldless implementation detail nor the only legal runtime forever.

## Three independent axes

```text
Capability kind                Runtime adapter               Execution placement
Action                         rcc                           collapsed local process
Robot Task        +            uv               +           container-backed worker
Workflow                       python                        independent worker / VM
Dev Task                       robot-framework               desktop/session/future
       \                           |                              /
        + Package Revision + Deployment + bindings + inputs +
                                   |
                                   v
                         Resolved Runtime Plan
                                   |
                                   v
                     shared admission / preflight
                                   |
                                   v
                          Run -> fenced Attempt
```

Keep distinct:

1. **Capability kind** — what is invoked.
2. **Runtime adapter** — how source/runtime requirements are inspected, prepared, launched, and collected.
3. **Execution placement** — where the fenced Attempt runs.

A Robot Task is not synonymous with RCC. A container is not a runtime adapter. RCC's cache/provider server is not Run or worker authority.

## Corrections from current and historical implementations

Eliminate these patterns:

- guessing producer/consumer or Work Item behavior from task names, imports, or source scans;
- guessing worker capabilities from words such as `website`, `email`, or `document`;
- simulating or synthesizing success when RCC, another adapter, or worker infrastructure is unavailable;
- parsing package/runtime declarations independently in API, frontend, importer, worker, and CLI;
- duplicating RCC/Holotree catalog, provider, cache-server, environment, and lease logic in Actions Runtime;
- treating absolute package paths, interpreter/Holotree/venv paths, container IDs, or provider URLs as portable identity;
- accepting `--version`, health, registration, or generated metadata as execution proof;
- creating adapter-specific Ready()/DependsOn-style gates that drift from one another.

Requirements, admission, compatibility, availability, and outcomes must be explicit and shared.

## Adapter doctrine from Hive

Apply the prior Hive/Andy backend discipline directly:

- the first backend proves the contract rather than becoming the architecture;
- `runtime_kind` is explicit and persisted before authoritative execution;
- adapters advertise versioned descriptors with truthful limitations;
- adapters are installed/authorized by the Runtime/operator, never loaded from untrusted package source merely because a package names one;
- no silent fallback, substitution, simulation, or self-certification;
- auth-required, unavailable, incompatible, unsupported, blocked, and execution-failed are different outcomes;
- every adapter is an additive vertical slice with real execution receipts;
- promotion requires objective evidence for the exact package/runtime/adapter subject;
- speculative adapters are deferred with explicit reopening criteria rather than promised as parity work;
- one shared admission decision is consumed by UI, CLI, Run creation, schedules, workflows, internal invocation, and workers.

If a Run is pinned to `runtime_kind=rcc` and RCC is unavailable or incompatible, it remains an RCC Run and reports the corresponding state. It does not become uv, Python, Robot Framework, a fake Robot, or a no-op.

## Shared admission contract

Preflight is the canonical read-only admission computation, not a collection of client-side checks.

Conceptually:

```text
admit(
  capability,
  package_revision,
  deployment_revision,
  selected_runtime_plan,
  normalized_inputs,
  binding_snapshot,
  worker_profile,
  policy_revision,
  observed_adapter_and_worker_state,
) -> AdmissionSnapshot
```

The snapshot has:

- stable admission schema/version;
- exact subject identity/fingerprint;
- Package, capability, Deployment, Runtime Plan, policy, and binding generations;
- selected `runtime_kind` and adapter features/version;
- static requirements and dynamic observations kept distinguishable;
- eligible placements/workers and preparation readiness;
- blockers, unknowns, warnings, and partial evidence;
- canonical outcome and reason codes;
- observed time/source and bounded expiry where dynamic;
- no secret values or local paths as portable identity.

### Canonical outcomes

At minimum distinguish:

- `ready`;
- `needs_input`;
- `needs_binding`;
- `runtime_preparation_needed`;
- `runtime_unavailable`;
- `runtime_auth_required`;
- `runtime_incompatible`;
- `no_eligible_worker`;
- `forbidden`;
- `unknown` or `partial_evidence` where a required observer has not produced complete evidence;
- `invalid` package/plan/declaration.

Do not collapse unknown/partial into ready or permanent failure.

### Admission modes

Roll admission behavior out through one shared mode applicator:

- **off** — transparent compatibility baseline; compute only what is required for existing correctness and do not gate newly enrolled boundaries;
- **shadow** — compute/record the canonical admission snapshot and diagnostics but do not change execution decisions;
- **enforce** — gate only explicitly enrolled boundaries/capabilities on the shared snapshot.

Mode is default-off for new enforcement and captured with the decision generation. One pass/run does not read a moving global toggle repeatedly and produce mixed behavior.

UI, CLI, scheduler, internal invocation, schedules, and workflows do not each implement their own mode logic.

### Revalidation and authority

Preflight/admission is advisory until Run creation transactionally revalidates immutable identity, authorization, and freshness-sensitive evidence.

- the snapshot is bound to the exact subject fingerprint;
- a change to Package, capability schema, Runtime Plan, bindings, policy, or protected input invalidates the relevant proof;
- unrelated proof dimensions remain reusable where their fingerprints still match;
- no global/history-latched judgment permanently blesses or blocks future generations;
- dynamic capacity/readiness may change without changing static plan identity;
- Run creation records the exact admission generation/mode/outcome used.

## Runtime adapter descriptor

Each adapter exposes a machine-readable descriptor including:

- stable `runtime_kind` and adapter contract version;
- implementation/build version and provenance;
- supported capability kinds and declaration formats;
- platforms/architectures/ABI classes;
- supported preparation modes: local build, immutable artifact acquire, offline/warm reuse, provider/cache;
- supported execution placements;
- cancellation/process-tree guarantees;
- stdout/stderr/event/structured-result behavior;
- artifact collection behavior;
- config/secret/Work Item injection capabilities;
- required tools/services/authentication;
- optional/required feature flags;
- truthful weaker guarantees and unsupported features.

Unknown required features fail closed. An adapter may support a useful subset without pretending parity.

## Adapter operation contract

Exact names require design approval, but provide semantics equivalent to:

```text
describe() -> RuntimeAdapterDescriptor
inspect(package_snapshot) -> capabilities + declarations + diagnostics
resolve(capability, package_revision, target, policy) -> RuntimePlan
preflight(plan, inputs, bindings, worker_profile) -> AdmissionEvidence
prepare(plan, attempt_context) -> PreparedRuntime
start(prepared, fenced_attempt, inputs, grants) -> AdapterExecutionHandle
events(handle, cursor) -> bounded ordered events
cancel(handle, reason) -> cancellation acknowledgement/evidence
collect(handle) -> result + artifact/log references + execution receipt
release(prepared/handle) -> release after full reap/publication
```

An adapter may delegate to a local binary, library, child service, or worker-side implementation. Actions Runtime owns the semantic contract and durable Run/Attempt authority; it does not reimplement adapter internals.

Every supported adapter defines/tests:

- exact argv/environment/working directory;
- input and file delivery;
- secret redaction before child output;
- bounded stdout/stderr/event ordering;
- structured result extraction and outcome classification;
- timeout/cancellation escalation;
- complete process-tree/container/session cleanup;
- artifact discovery/publication;
- restart/reconciliation evidence;
- stale Attempt publication rejection.

## Resolved Runtime Plan

Normalize package declarations and Deployment policy into an immutable plan containing:

- Package Revision and capability identity;
- selected `runtime_kind`;
- adapter contract/version/required features;
- platform/architecture/ABI;
- adapter-specific runtime specification identity;
- optional immutable runtime artifact/reference and compatibility policy;
- placement/Worker Profile requirements;
- declared inputs/files/config/bindings;
- Work Item mode;
- timeout/cancellation/effect/retry policy;
- artifact/log/result limits;
- plan digest and normalization version.

Adapter-specific fields remain typed/namespaced.

### RCC plan

May include:

- Environment Specification digest;
- Environment Artifact digest;
- platform/builder compatibility;
- provider profile/reference under policy;
- required RCC features/version;
- Task Package/task/PATH/PYTHONPATH/artifact/pre-run metadata.

### Other plans

- uv: normalized project/lock/Python/runtime specification;
- Python: explicit interpreter/dependency/trust/placement restrictions and weaker reproducibility;
- Robot Framework: suite/task/runner plus explicit environment contract.

Do not persist absolute source/interpreter paths, `PYTHON_EXE`, `CONDA_PREFIX`, `VIRTUAL_ENV`, `ROBOCORP_HOME`, local materialization/venv paths, provider URLs, PIDs/container IDs, or raw secret/env values as portable identity.

## Common execution requirements

### Inputs and files

- typed Action or Robot/Workflow payload;
- artifact/file/resource handles;
- inline-size limits;
- explicitly supported stdin/interaction;
- package root and writable temp/output rules.

### Configuration and secrets

Distinguish:

- package-declared non-secret names/descriptions/defaults;
- Deployment-bound non-secret config;
- authorized per-Run non-secret overrides;
- secret/OAuth provider references;
- Runtime-owned Run/Attempt/artifact/Work Item values;
- denied inherited host environment.

### Work Items

Work Items are optional context and never inferred from names/source.

Declare modes such as:

- none;
- optional input/output context;
- one input Work Item required;
- output creation allowed;
- queue claim/consumer;
- producer;
- workflow-managed context.

#132 owns durable queue/business semantics.

### Bindings/providers

- secret/OAuth;
- data/knowledge;
- output artifact/result store;
- Work Item provider/queue;
- network grants;
- package source and adapter runtime-artifact providers.

### Worker/isolation

- browser/Chromium;
- native desktop/interactive Windows session;
- private network/VPC;
- container isolation;
- GPU/devices;
- filesystem/mount classes;
- CPU/memory/concurrency;
- trust/dedicated-worker policy;
- platform/architecture/ABI.

## RCC as the first proving adapter

Use the supported RCC surface from `joshyorko/rcc#118/#120`, not a parallel Actions implementation:

- current task/action compatibility where required;
- Environment Manifest/Object Index identity;
- `rcc env publish`, `rcc env acquire`, and `rcc env exec` or supported machine equivalents;
- process-owned execution leases;
- named provider profiles;
- lazy verified warm operation without provider/network dependency;
- `rcc cache serve` for Environment Artifact distribution;
- local/offline Holotree behavior;
- standard Task Package semantics.

`rcc cache serve` and legacy `rccremote` distribute environment/cache content. They are not #83 Run authority or #90/#138 worker scheduling/fencing.

Actions stores references/receipts and delegates RCC lifecycle. It must not create another Holotree service, RCC object store, cache server, provider-profile store, or adapter-local lease database.

## Additional adapters and objective promotion

After RCC proves the interface, implement one genuinely different adapter—prefer uv or direct Robot Framework—as the anti-lock-in proof.

A raw Python adapter may be useful for trusted/local execution, but its weaker hermeticity must be visible and it is insufficient as the sole proof if it merely inherits host state.

A new adapter begins default-off/shadow-only and is promoted to enforced production eligibility only when an exact-subject conformance receipt demonstrates, for the supported subset:

- deterministic plan normalization;
- real capability execution and expected effect/result;
- argv/environment/input/binding correctness;
- cancellation and complete cleanup;
- artifact/log/result behavior;
- restart/reconciliation evidence;
- secret redaction;
- supported placement behavior;
- no fallback/simulation;
- mixed-version and negative compatibility behavior.

Deferred adapters require explicit reopening criteria such as a real package/user need, an unsupported capability class, or evidence that the current adapter imposes unacceptable cost or constraints. Do not create parity roadmaps merely because another runtime exists.

Do not block #134's RCC consumer vertical on all adapters. Do not freeze the generic interface until the second adapter exposes accidental RCC assumptions.

## Conformance

#139 owns the shared suite. At minimum prove:

1. real capability discovery/execution, not health-only evidence;
2. exact Runtime Plan selection and no fallback;
3. exact-subject admission fingerprint and selective invalidation;
4. off/shadow/enforce consistency through one mode applicator;
5. deterministic plan normalization;
6. input/config/binding construction;
7. standalone Robot Task with no Work Items;
8. optional/required Work Item modes;
9. bounded logs/results/artifacts;
10. timeout and full cleanup;
11. missing binary/service/auth classification;
12. restart/reconciliation;
13. secret redaction;
14. local/worker semantic equivalence where supported;
15. unsupported feature/placement rejection;
16. RCC Artifact/provider scenario;
17. non-RCC scenario proving generic identity is not RCC-shaped.

## Consumers

This contract is the source of truth for:

- Run Composer and diagnostics (#145);
- CLI discovery/preflight/run/doctor (#146);
- Package/Deployment compilation (#129/#130/#135);
- Robot Task capabilities (#144);
- internal invocation (#147);
- schedules/triggers/workflows;
- worker eligibility/placement (#90/#138);
- evidence/conformance (#137/#139).

Clients never independently reparse package files or guess backend behavior.

## Acceptance

1. RCC executes a real local Action and Robot Task through its Environment Artifact/lease/exec lifecycle.
2. The same RCC environment is served through `rcc cache serve`, acquired by a clean worker, and used without an Actions cache server.
3. A standalone no-Work-Item Robot preflights/runs.
4. Missing RCC/provider credentials/provider availability/platform/features are distinct and never simulate/fallback.
5. One second adapter executes through the same Run/Attempt/admission/result contract with no mandatory RCC field.
6. A Package may declare multiple compatible plans; one selected plan is pinned across retry.
7. One canonical admission snapshot drives UI/CLI/Run/scheduler behavior.
8. off/shadow/enforce modes are default-safe, generation-captured, and boundary-enrolled.
9. exact-subject fingerprints invalidate only affected proof dimensions.
10. Runtime/CLI/UI show truthful adapter/readiness evidence.
11. host environment is denied by default and secrets never enter durable plans/evidence.
12. equivalent replicas produce equivalent static plan/admission results; only explicit dynamic observations may differ.
13. adapter promotion requires objective real-execution conformance.

## Non-goals

- Replacing Run/Attempt authority (#83).
- Making RCC a daemon requirement or only adapter.
- Reimplementing adapter/provider internals in Actions.
- Loading package-provided adapters or granting arbitrary sockets/host access.
- Claiming feature parity across adapters.
- Making Work Items mandatory.
- Turning arbitrary shell commands into production capabilities.
- Building speculative adapters without reopening criteria.