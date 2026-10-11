Parent: #82
Related: #83, #85, #90, #92, #129, #130, #132, #133, #134, #135, #136, #138, #142, #143, #144

## Outcome

Create one deterministic conformance/evaluation harness proving that Package Revisions, capabilities, Runtime Plans/adapters, shared admission, Deployment projections, workers/placements, providers, Runs, Tasks, Work Items, artifacts, and interactions preserve their declared contracts across local and distributed modes.

This is executable capability-runtime evidence, not primarily an LLM benchmark.

The harness must prevent:

1. support claims based on `--version`, health, mocked registration, generated metadata, simulation, or synthetic success instead of real capability execution;
2. RCC becoming mandatory generic schema by accident;
3. silent adapter fallback when the pinned backend fails;
4. duplicated admission gates drifting across UI/CLI/scheduler/workflows;
5. stale proof being reused for a changed Package, Runtime Plan, binding, policy, worker, or adapter generation;
6. every possible backend becoming a speculative parity roadmap without demand or promotion evidence.

RCC is the first/richest conformance implementation. At least one genuinely different adapter must pass the common subset before the adapter abstraction is called backend-neutral.

## Conformance doctrine

Apply the prior Hive/Andy proof and promotion rules:

- `runtime_kind`, Package/capability/Runtime Plan, placement, and policy generations are part of every scenario and receipt;
- admission/proof is bound to the exact subject fingerprint;
- complete proof dimensions invalidate selectively when their inputs change;
- unrelated valid proof survives;
- no global/history-latched judgment permanently blesses a later generation;
- adapter compatibility declaration is different from installed/authenticated/readiness state;
- no automatic fallback, simulation, or self-certification;
- adapters/providers/placements are additive independent slices;
- unsupported cells fail or skip explicitly according to the gate contract—never false-pass;
- real argv/environment/input/effect/result/cancellation/process-tree behavior is asserted;
- stronger backend guarantees stay visible instead of reducing everything to the weakest adapter;
- generic assertions never require RCC-specific fields;
- adapter-specific assertions remain namespaced;
- new enforcement begins default-off/shadow and promotes objectively to enrolled enforce mode;
- deferred adapters have explicit reopening criteria.

## Exact-subject proof identity

Every proof/evaluation is bound to a deterministic fingerprint over the relevant subset of:

- Package Revision/source digest;
- capability ID/schema;
- Runtime Plan ID/digest and adapter contract/implementation version;
- adapter required-feature set;
- platform/architecture/ABI;
- Deployment/policy/binding generations;
- Worker Profile and placement descriptor;
- provider/cache protocol versions where material;
- scenario/version and fault schedule;
- protected inputs/effect policy;
- harness/assertion version.

Do not include secrets, provider credentials, local paths, PIDs, container IDs, or transient capacity in durable proof identity.

Dynamic readiness/capacity observations are timestamped evidence linked to the static fingerprint, not permanent truth.

A changed RCC manifest reader invalidates RCC compatibility proof, not an unrelated Action schema proof. A changed binding invalidates binding/admission evidence, not source determinism. A changed selected Runtime Plan invalidates execution proof and requires a new Run/admission decision.

## Shared admission conformance

#143 owns one canonical AdmissionSnapshot consumed by UI, CLI, Run creation, scheduler, schedules, workflows, and internal invocation.

Scenarios assert:

- equivalent immutable input yields equivalent static admission outcome across replicas;
- `ready`, missing input/binding, preparation needed, adapter unavailable/auth-required/incompatible, no worker, forbidden, unknown/partial, and invalid remain distinct;
- unknown/partial never becomes ready or latched failure silently;
- one mode applicator implements:
  - `off` transparent baseline;
  - `shadow` read-only evidence/diagnostics;
  - `enforce` only for explicitly enrolled boundaries;
- mode and generation are captured once per pass/Run decision;
- a moving global toggle cannot produce a mixed decision inside one pass;
- shadow and enforce compute the same canonical outcome; only policy effect differs;
- Run creation transactionally revalidates protected identity/authorization/freshness;
- clients do not implement independent Ready()/DependsOn gates.

## Promotion and deferral contract

A new adapter/provider/placement begins as an additive default-off/shadow slice.

Promotion to production-enforce eligibility requires an exact-subject receipt covering its claimed subset:

- deterministic plan/descriptor normalization;
- real capability execution and expected effect/result;
- argv/environment/input/file/binding correctness;
- host environment denial and secret redaction;
- cancellation and full process/container/session cleanup;
- bounded logs/events/results/artifacts;
- restart/reconciliation and stale-owner rejection;
- supported placement behavior;
- unavailable/auth-required/incompatible/unsupported classification;
- no fallback/simulation;
- mixed-version negative behavior;
- resource/time budget.

Promotion is not universal certification. It records the supported capability/platform/placement subset.

A deferred adapter is reopened only when one of these is true:

- an actual Package/user requires a capability unsupported by current adapters;
- current adapter cost/performance/isolation becomes unacceptable with evidence;
- a concrete provider/placement integration needs it;
- the second-adapter anti-lock-in proof exposes a missing generic seam;
- release/compatibility policy explicitly requires it.

Do not create Kubernetes, WASM, Dagger, OCI-only, raw-Python, or other adapter projects merely because the technology exists.

## Conformance layers

### Package compiler/source

- deterministic source/capability/Runtime Plan digest;
- source timestamp/location independence;
- safe staging/path/link/archive behavior;
- Action/Robot Task/Workflow/Dev Task declarations;
- package-scoped IDs/collisions;
- logical bindings and explicit Work Item modes;
- effect/idempotency/timeout/cancellation/artifact metadata;
- passive versus executable inspection evidence;
- out-of-process discovery/import;
- no secret/path/provider/socket/plugin leakage;
- source-only versus runtime-plan-changing generations;
- current v2, Gallery, and `robot.yaml` fixtures;
- package-provided adapter executable/plugin rejection.

### Runtime Plan

- deterministic plan normalization/digest;
- explicit `runtime_kind` and required features;
- capability-to-plan compatibility;
- platform/architecture/ABI;
- namespaced runtime specification/reference;
- selected plan pinning across retry;
- no implicit fallback order;
- no fake RCC fields in non-RCC plans;
- no local interpreter/materialization path as portable identity.

### Generic Runtime Adapter (#143)

Exercise semantics equivalent to:

```text
describe -> inspect -> resolve -> preflight/admit
 -> prepare -> start -> events -> cancel -> collect -> release
```

Assert:

- exact argv/env/cwd;
- input/file/binding delivery;
- host environment denial;
- redaction before child output;
- bounded stdout/stderr/events and ordering/backpressure;
- structured outcome classification;
- timeout/cancellation escalation;
- complete process-tree/container/session cleanup;
- output publication;
- restart/reconciliation receipt;
- unavailable/auth-required/incompatible/unsupported/failed distinctions;
- no fallback/simulation;
- real capability effect/result, not health-only evidence.

### RCC adapter

Exercise #133/#134 and `joshyorko/rcc#118/#120`:

- Environment Specification/Artifact identity;
- `env publish/acquire/exec` or supported machine contracts;
- provider profiles;
- `rcc cache serve` A→B environment distribution;
- local/offline/provider-backed paths;
- verify/materialize/process-owned lease/release;
- source-only reload with zero build/provider calls;
- runtime-plan-changing reload beside old generation;
- provider-dead verified warm reuse;
- corrupt/tampered/platform/builder mismatch rejection;
- Action/Task Package import and execution;
- cancellation before/after preparation/spawn;
- full process cleanup;
- no Actions-owned duplicate cache/provider/lease authority;
- cache server never treated as Run/worker control.

The expected A→B release gate fails rather than skips when required prerequisites are missing.

### Second-adapter anti-lock-in

Prefer uv or direct Robot Framework.

Assert:

- Package compiler emits a non-RCC plan;
- local execution uses its own typed identity;
- no RCC manifest/catalog/provider/lease fields required;
- common input/result/artifact/cancellation receipt shape;
- truthful different/weaker guarantees;
- one additional placement where supported;
- an RCC-pinned Run never falls back despite the second adapter being installed;
- a deliberate new Run succeeds only when package/policy explicitly permit the second adapter.

Raw Python is useful but insufficient as the only proof when it simply inherits host state.

### Deployment/catalog

- deterministic projection/catalog revision;
- runtime selection independent of exposure;
- test→production binding/runtime policy mapping;
- Workspace authorization and negative references;
- update/rollback/restart-as-new versus retry pinning;
- old/new revisions coexist;
- no adapter fallback after outage/update;
- provider/profile references omit secret values and locator identity.

### Run/Attempt

- idempotent Run creation includes Runtime Plan/input compatibility;
- DB-time claim/lease/epoch fencing;
- adapter preparation/spawn/finish transitions;
- stale events/results/artifacts/effects rejected;
- retry preserves Run/Task/Runtime Plan;
- cancellation during package/runtime preparation and execution;
- crash before/after preparation/spawn/effect/result;
- ambiguous/manual-review outcome;
- cross-replica control;
- adapter change requires a new Run.

### Worker/protocol/placement

- descriptor and feature negotiation;
- safe decline for wrong adapter/platform/profile/provider;
- process/container/session receipts;
- local/container/independent-worker semantics;
- no package access to runtime socket;
- backpressure/drain/orphan cleanup;
- cache locality as optimization;
- mixed versions;
- provider-dead warm behavior where guaranteed;
- no synthetic registration/success.

### Artifacts/results

- provisional/final fenced publication;
- handles and authorization;
- bounded streaming/range reads;
- lineage/digest;
- stale Attempt cannot finalize;
- provider outage cannot falsely complete;
- cross-replica access;
- adapter-native outputs such as Robot Framework XML/screenshots/reports.

### Tasks/interactions

- durable MCP Task projection;
- Task survives Attempt replacement;
- interaction pause/disconnect/resume;
- approval bound to Package/Runtime Plan/protected inputs;
- duplicate/expired/cancelled response rejection;
- bulk evidence by handle.

### Work Items/workflows

- queue claim/lease/fence;
- explicit Work Item mode;
- standalone no-Work-Item Robot;
- optional Work Item both ways;
- producer/consumer only when declared;
- retries/backoff/dead-letter;
- business/application failures;
- review/manual intervention;
- schedule/not-before;
- fan-out/fan-in/lineage;
- no task-name/source inference or synthetic Work Items.

## Scenario format

A versioned scenario declares:

- source fixture/Package Revision;
- capability ID/kind/schema;
- compatible and selected Runtime Plan;
- no-fallback expectation;
- Deployment/Worker Profile/bindings;
- admission mode/enrollment and expected snapshot;
- invocation/input/artifact/Work Item/idempotency key;
- topology/placement/platform;
- adapter/provider/cache setup;
- fault schedule;
- expected Run/Attempt/Task/Work Item states;
- expected result/artifact/evidence;
- negative secret/path/socket/fallback/simulation assertions;
- timeout/resource budget;
- required versus optional prerequisites.

Use code where declarative format becomes unreadable. Do not invent a workflow language solely for tests.

## Deterministic fault injection

Cover:

- database loss/rollback;
- Package/runtime/output provider slow/unavailable/corrupt;
- RCC cache/provider loss;
- adapter binary/service/auth missing;
- adapter feature/version/platform mismatch;
- package/runtime tampering;
- worker crash before/after preparation/spawn/effect/result;
- lease expiry/stale owner;
- cancellation at every phase;
- artifact publication failure;
- interaction disconnect;
- Work Item claim loss;
- rolling coordinator/worker/adapter versions;
- DB/provider time boundaries;
- disk pressure/GC;
- container orphan/kill failure;
- compatible alternative adapter present while selected adapter fails;
- mode change between passes but not within a captured pass.

Do not rely only on sleeps or probabilistic races.

## Evidence receipt

Every evaluation emits:

- scenario/version and exact-subject fingerprint;
- source/repository SHA;
- harness/assertion version;
- Runtime/coordinator/worker/adapter versions/provenance;
- selected Runtime Plan and placement;
- Package/capability/Deployment/catalog identities;
- namespaced adapter runtime identity;
- provider/cache class;
- admission mode/generation/outcome and enrollment;
- topology/platform;
- seed/fault schedule;
- field-level assertions;
- process cleanup/cancellation evidence;
- timing/resource data;
- trace/audit references;
- artifact/log hashes/handles;
- explicit skip/unsupported reason;
- no-fallback/simulation result;
- redaction/truncation indicators.

Receipts are suitable for CI, release, promotion, support, and regression comparison and contain no secrets/private bulk data.

## Execution matrix

Reuse scenarios against:

- collapsed local Runtime + SQLite + filesystem + RCC;
- RCC `cache serve` clean consumer;
- collapsed local Runtime + second adapter;
- rootless container placement;
- multiple Runtime processes + PostgreSQL + independent workers;
- local/shared Package/output/data/Work Item providers;
- supported platform matrix.

Unsupported cells and stronger guarantees remain explicit.

## Golden scenarios

Include:

- read-only Action;
- standalone Robot Task without Work Items;
- RCC Robot with artifacts/cancellation;
- same RCC runtime acquired by clean consumer;
- declared uv/direct Robot Framework equivalent;
- consequential idempotent mutation with effect receipt;
- source-only/runtime-changing autoreload;
- long Task with worker retry;
- bounded connector projection;
- Work Item workflow with review/report artifacts;
- large data handle;
- Canvas/App interaction;
- package/runtime/provider revocation;
- selected adapter outage with compatible alternative installed—assert no fallback;
- adapter shadow→enforce promotion with exact receipt;
- deferred adapter reopening criteria evaluation.

Historical Control Room informs the domain, not compatibility.

## Acceptance

1. One scenario runs across collapsed and independent-worker placements with equivalent logical outcome/provenance.
2. Package/capability/Runtime Plan/Deployment identities are pinned/asserted.
3. shared admission is identical across consumers and off/shadow/enforce behavior is proven.
4. exact-subject fingerprints and selective invalidation are proven.
5. RCC A→B provider/cache and warm provider-dead paths use real RCC behavior.
6. one second adapter passes common conformance without RCC fields.
7. selected adapter failure never falls back/simulates.
8. real process/task effect/output is asserted beyond health.
9. fencing/cancellation/crash races are deterministic.
10. secrets/paths/private data/runtime sockets have negative assertions.
11. receipts are stable/versioned and gate promotion/releases.
12. unsupported cells skip explicitly.
13. current v2/Gallery/Robot fixtures remain covered.
14. adding an adapter/provider/placement requires its conformance slice and objective promotion.
15. harness works without LLM/cloud service.

## Non-goals

- General agent-intelligence/prompt evaluation.
- Full workflow engine.
- Replacing unit/property/security tests.
- Hiding nondeterminism behind retries.
- Hosted test service requirement.
- Weakest-common-denominator adapter semantics.
- Accepting simulation or health as execution evidence.
- Building speculative adapters without concrete reopening criteria.