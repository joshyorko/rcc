# Environment Artifacts Program Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drive RCC issues #121–#127 and #183 to a coherent code-complete acceptance boundary on `feature/environment-artifacts-v1`, while leaving release publication and the Actions consumer handoff in #120.

**Architecture:** Preserve Manifest v1 as the portable identity over existing v12 catalogs and Hololib bytes. Complete local lifecycle safety, carrier convergence, provider behavior, trust receipts, build coordination, and evidence collection through narrow packages; keep Actions scheduling and distributed Attempt ownership outside RCC.

**Tech Stack:** Go 1.26, Cobra, RCC developer toolkit, Robot Framework, GitHub Actions platform runners.

## Global Constraints

- Work only on `feature/environment-artifacts-v1` / PR #119; do not create replacement PRs or merge to `main`.
- Preserve v12 catalogs, legacy `rccremote`, all eight release assets, zero-config local behavior, and `RCC_ENDPOINT_*` / `RCC_AUTOUPDATES_*` overrides.
- Portable identity is cryptographic and independent of paths, provider locations, process IDs, and Actions objects.
- Active leases prevent GC; process crashes are recoverable without accepting PID reuse; expiry alone never deletes state used by a live foreign process.
- Existing verified content wins over partial, stale, or conflicting publication; no incomplete Manifest becomes authoritative.
- Tests are offline by default. Platform claims require native GitHub Actions evidence.
- Use failing tests before production changes, bounded commits, DCO, exact-head pushes, and no weakened tests or broad exclusions.

## Reconciled Acceptance Matrix

| Issue | Acceptance area | State at `58878863a3da758c38a2290f375d6f46305da426` | Evidence / owner |
| --- | --- | --- | --- |
| #121 | Linux amd64 private-home A-to-B, offline execution, warm provider-dead reuse | Partial: implemented but opt-in and external-binary dependent | `environmentlifecycle/real_vertical_test.go`; `5318bc82`, `d73d0754` |
| #121 | macOS amd64/arm64 and Windows amd64 A-to-B runtime | Missing | no native runtime receipts; #168 only adds declared platform validation |
| #121 | Platform tuple and pre-acquisition rejection | Partial | `environmentartifact/platform.go`, `manifest.go`; #168 / `92150bdb` |
| #121 | Python ABI, libc/system requirements, minimum OS, CPU features, filesystem capabilities, override receipt | Missing | no typed compatibility fields or tests |
| #122/#183 | Strong process-start lease identity and safe stale reconciliation | Missing | PID fallback in `environmentlifecycle/lease.go`; no reconciliation |
| #122 | Repair, invalidation, crash-boundary recovery, lease-aware GC and retention | Missing | verification exists in `acquire.go`; no repair/GC API |
| #122 | Independent-process acquire/repair/GC races and observability | Missing | no race suite or machine-readable lifecycle status |
| #123 | Bounded verified HTTP streaming and idempotent publication | Partial | `artifactprovider/http.go`; `b3659694`, `cd7b3ee8` |
| #123 | Enterprise proxy/CA/no-proxy, interruption/restart policy, quotas/rate/retention, abuse suite | Missing | no protocol or real-request tests |
| #123 | Second provider satisfying the same contract | Missing | filesystem store plus HTTP adapter are one implementation family |
| #124 | Canonical archive names and basic traversal bounds | Partial | `environmentartifact/archive.go`; `9ddcd041` |
| #124 | v12 wrap/export/import, cross-carrier identity, bundle modes, corrupt/bomb/interruption tests | Missing | no archive writer or lifecycle importer |
| #124 | Upgrade/rollback and legacy Hololib/bundle compatibility | Partial | legacy tests/docs exist; no Manifest carrier migration proof |
| #125 | Post-v18.19 materializer/storage R&D; reproducible benchmark, real workload, and ranked decision | Non-blocking | retained open under the authoritative scope correction; not a v18.19 release gate |
| #126 | Deterministic provenance/SBOM identity binding and policy model | Partial | `artifacttrust/trust.go`; `535ebbf2` |
| #126 | HTTP/archive integration, tamper/unknown/expired/revoked signer, execution enforcement, receipts/redaction | Missing | unit-only trust package |
| #127 | Claim/heartbeat/epoch/takeover coordination and conflict policy | Missing | `docs/environment-build-coordination.md` only |
| #127 | Prewarm plan/execution, local fallback, N-worker reuse, Actions-neutral request | Missing | documentation only |
| #120 | Release, N-1/self-host, eight assets, RC, Homebrew and Actions handoff | Release-only after code completion | issue #120; must remain open |

---

### Task 1: Strong Lease Identity, Reconciliation, Repair, and Lease-Aware GC

**Files:**
- Modify: `environmentlifecycle/lease.go`
- Create: `environmentlifecycle/reconcile.go`
- Create: `environmentlifecycle/gc.go`
- Modify: `environmentlifecycle/record.go`
- Modify: `environmentlifecycle/lease_test.go`
- Create: `environmentlifecycle/reconcile_test.go`
- Create: `environmentlifecycle/gc_test.go`

**Interfaces:**
- Produce `LeaseStatus` values `active`, `stale`, and `ambiguous`.
- Produce `Reconcile(ctx context.Context, digest environmentartifact.Digest) (ReconcileReport, error)`.
- Produce `Collect(ctx context.Context, policy GCPolicy) (GCReport, error)` with explicit retention and dry-run behavior.
- Preserve `Lease`, `Release`, `Execute`, and materialization record compatibility.

- [ ] Write tests that inject process identity lookup and prove missing/ambiguous start identity fails closed instead of using PID-only identity.
- [ ] Run the focused test and confirm it fails because strong identity/reconciliation is absent.
- [ ] Implement injectable strong owner identity and canonical lease classification.
- [ ] Write tests proving live leases survive GC, stale leases reconcile, ambiguous leases block deletion, released materializations become reclaimable, immutable objects are never modified in place, and repeated release/GC is idempotent.
- [ ] Run the tests and confirm the missing repair/GC behavior fails.
- [ ] Implement atomic reconciliation and lease-aware GC with explicit policy/report types.
- [ ] Run `GOARCH=amd64 CGO_ENABLED=0 go test -race ./environmentlifecycle` and `git diff --check`.
- [ ] Commit with DCO and push the bounded checkpoint.

### Task 2: Manifest-Aware Offline Archive and Legacy Carrier Convergence

**Files:**
- Modify: `environmentartifact/archive.go`
- Modify: `environmentartifact/archive_test.go`
- Create: `environmentlifecycle/archive.go`
- Create: `environmentlifecycle/archive_test.go`
- Modify only the existing bundle/export/import command files required to route through the lifecycle.
- Add bounded Robot coverage under `robot_tests/` for source-only and source-plus-Artifact carriers.

**Interfaces:**
- Produce deterministic archive writing from exact Manifest/Object Index/catalog/object bytes.
- Produce bounded streaming archive import into the existing verified local store.
- Preserve legacy `hololib.zip`, appended ZIP, and bundle readers.

- [ ] Write failing tests for deterministic archive bytes/order, v12 byte preservation, HTTP/archive identity equality, traversal, symlink, duplicate, truncated, corrupt, and size-bomb rejection.
- [ ] Implement deterministic writer and bounded reader without making ZIP metadata part of Artifact identity.
- [ ] Write failing lifecycle tests for export/import across separate RCC homes and provider-free execution.
- [ ] Implement lifecycle export/import and legacy v12 wrapping through Manifest v1.
- [ ] Add Robot fixtures for source-only and source-plus-Artifact modes while retaining existing bundle suites.
- [ ] Run focused Go tests, bundle Robot tests, `git diff --check`, then commit with DCO and push.

### Task 3: Production Provider Contract

**Files:**
- Modify: `artifactprovider/provider.go`, `artifactprovider/http.go`, `artifactprovider/http_test.go`
- Add focused provider-policy files/tests where separation is required.
- Modify: `cmd/cacheServe.go` and its tests only for exposed protocol controls.

**Interfaces:**
- Define explicit restart-from-zero or byte-range resume behavior for interrupted downloads and uploads.
- Define quota/rate/retention errors that cannot invalidate warm local operation.
- Exercise RCC-configured proxy, CA bundle, TLS, and no-proxy transport through real `httptest` requests.
- Add a second provider implementation with the same behavior contract, without adding a required external service.

- [ ] Add a reusable provider contract test suite covering capabilities, missing objects, idempotent puts, atomic commit, interruption, restart, quota, retention, abuse bounds, and credential redaction.
- [ ] Confirm the contract fails against current HTTP behavior and the missing second provider.
- [ ] Implement the smallest explicit interruption/restart policy, quota/retention controls, and second local provider adapter.
- [ ] Add enterprise transport tests using local TLS/proxy fixtures and bounded memory measurement for streamed objects.
- [ ] Run provider/settings/cmd focused tests and `go test -race ./artifactprovider`, then commit with DCO and push.

### Task 4: Artifact Trust Attachments and Verification Receipts

**Files:**
- Modify: `artifacttrust/trust.go`, `artifacttrust/trust_test.go`
- Add attachment integration under `environmentartifact/` and `environmentlifecycle/`.
- Extend archive and HTTP provider tests to carry the same trust attachment identity.

**Interfaces:**
- Produce deterministic provenance and SBOM attachment descriptors bound to Artifact digest.
- Produce a machine-readable `VerificationReceipt` that consumers can use without importing trust internals.
- Enforce deployment-owned policy before new execution while defining running-lease behavior.

- [ ] Add failing tests for tamper, unknown signer, expiry, revocation, strict unsigned policy, package/Manifest policy downgrade attempts, redaction, and retained evidence.
- [ ] Implement deterministic attachment serialization, signer validity, policy enforcement, and receipt generation.
- [ ] Add HTTP/archive parity tests and execution-boundary enforcement tests.
- [ ] Run `go test -race ./artifacttrust ./environmentartifact ./environmentlifecycle ./artifactprovider`, then commit with DCO and push.

### Task 5: Build Claims and Prewarm APIs

**Files:**
- Create: `environmentlifecycle/coordination.go`, `environmentlifecycle/coordination_test.go`
- Add CLI/machine contract under `cmd/` with focused tests.
- Update `docs/environment-build-coordination.md` to match executable behavior.

**Interfaces:**
- Produce claim identity from specification/platform/builder compatibility.
- Support owner, heartbeat, epoch, expiry, deterministic takeover, completed-Artifact precedence, and divergent-result visibility.
- Produce an Actions-neutral prewarm request/result using only specification/Artifact identity and provider policy.

- [ ] Write fake-clock tests for two builders, owner crash, stale takeover, fencing, committed Artifact precedence, divergent outputs, and local no-coordinator fallback.
- [ ] Implement filesystem-backed coordination as optional local/provider capability; never make it Action Attempt ownership.
- [ ] Write tests proving N prewarm requests reuse one committed Artifact and do not force N cold builds/transfers.
- [ ] Add JSON CLI contracts and run focused/race tests, then commit with DCO and push.

### Task 6: Compatibility Contract and Native Platform Evidence

**Files:**
- Modify: `environmentartifact/manifest.go`, `environmentartifact/platform.go`, and tests.
- Modify: `.github/workflows/rcc.yaml` or the existing contained promotion workflow.
- Correct: `docs/environment-artifact-compatibility.md` based on actual evidence.

**Interfaces:**
- Represent Python ABI/implementation, OS/libc minimums, CPU features, filesystem requirements, relocation capabilities, and explicit operator override receipts.
- Reject incompatibility before provider object acquisition.

- [ ] Add failing table tests for every compatibility dimension and override receipt.
- [ ] Implement canonical compatibility fields without changing legacy blueprint/catalog identity.
- [ ] Add native Linux amd64, macOS amd64/arm64, and Windows amd64 A-to-B jobs that cannot silently skip.
- [ ] Run local Linux tests and dispatch platform workflows; record exact native receipts and correct unsupported claims.
- [ ] Commit with DCO and push.

### Task 7: Post-v18.19 Benchmark Evidence and Optimization Decision (Non-blocking)

This task remains valid R&D under #125 but is explicitly outside the v18.19
release-critical dependency graph.

**Files:**
- Extend: `developer/benchmarks/holotree.py`, `developer/benchmarks/README.md`, `developer/toolkit.yaml`
- Add committed raw, machine-readable evidence under `developer/benchmarks/results/`.

**Interfaces:**
- Measure specification, build, Lift, inventory, publish, download, verify/import, Drop, warm acquire, execution handle, startup/imports, and reload classes.
- Record exact commit, platform, filesystem, repetitions, cache class, bytes, files, wall/CPU, and peak memory.

- [ ] Add tests for deterministic result schema and toolkit invocation.
- [ ] Add representative tiny, uv-native, conda-heavy, many-file, relocation-heavy, and one real Actions workload fixtures.
- [ ] Run reproducible baselines, explicitly retire or reproduce PR #64 claims, and commit raw evidence.
- [ ] Write the ranked optimization decision by measured benefit, risk, platform reach, cost, compatibility, and rollback gate; choosing no optimization remains valid only if evidence supports it.
- [ ] Commit with DCO and push.

### Task 8: Stabilization, Program Receipts, and Release Handoff

**Files:**
- Update PR #119 body and issue comments only after exact evidence exists.
- Update release notes/compatibility documents without tagging or publishing.

- [ ] Run focused package/race tests, contained `unitTests`, local build, relevant Robot suites, real A-to-B, two-generation self-host, legacy bundle/rccremote regressions, and exact binary inventory.
- [ ] Run full `GOARCH=amd64 CGO_ENABLED=0 go test ./...`, lint/static analysis, and `git diff --check`.
- [ ] Push exact final #119 head and wait for native platform checks to reach terminal state.
- [ ] Post durable criterion-level receipts to #121–#127 and #183; identify truthful closure candidates.
- [ ] Update #118 with the code-complete milestone and leave #120 open for RC/stable publication, Homebrew credentials/handoff, N-1 proof, and Actions #133 adoption.
- [ ] Do not merge #119, tag, publish, close release-only issues, or mutate Actions.
