# RCC Documentation

Welcome to the documentation for **`joshyorko/rcc`** (current release: **`v18.19.11`**).
This index is organized by task and maturity so you can distinguish current
released behavior from legacy compatibility interfaces and historical design
notes.

Looking for the full auto-generated heading index across all `docs/*.md` files?
See [docs/toc.md](toc.md).

---

## Start Here

- **[Quickstart](quickstart.md)** — Verify your `v18.19.11` binary, run a credential-free Python task, and walk through a local Environment Artifact publish/acquire/exec cycle.
- **[Capability & Documentation Inventory](capability-inventory.md)** — Source-backed truth table separating **Released (`v18.19.11`)**, **Experimental/Unmerged**, **Legacy Compatibility**, and **Proposed** work, plus verified security/platform/archive boundaries.
- **[Changelog](changelog.md)** — Release history through `v18.19.11` (also readable via `rcc docs changelog`).

## Build & Run Environments

- **[Recipes & Configuration Reference](recipes.md)** — Practical patterns for `robot.yaml`, `conda.yaml`, freezing dependencies, `ROBOCORP_HOME`, shared vs. private Holotrees, and CLI argument passing (also via `rcc docs recipes`).
- **[Environment Caching (Holotree Concept)](environment-caching.md)** — Conceptual explanation of Holotree deduplication, spaces, and relocation (includes historical Q3 2021 wording).
- **[Embedded Micromamba](micromamba.md)** — Pinned `2.10.0` release provenance, digest verification, and native acceptance boundaries.
- **[Executable Resolution in Acquired Environments](environment-executable-resolution.md)** — How `rcc env exec` resolves bare commands strictly inside the materialized environment and how explicit absolute-path delegation works.
- **[Robot Bundle Output Contract](bundle-output-contract.md)** — Stream separation, `artifactsDir` copy-back, exit codes, and cancellation behavior for `rcc robot run-from-bundle`.
- **[Virtual Environments (`rcc venv`)](venv.md)** — Experimental unmanaged local venv wrapper on top of Holotree.

## Distribute & Coordinate

- **[Environment Artifact Providers & Endpoints](providers.md)** — Named provider profiles (`rcc provider`), loopback hosting (`rcc cache serve`), TLS/auth reverse-proxy topology, signed strict-remote setup, and `RCC_ENDPOINT_*` overrides.
- **[Environment Artifact Compatibility](environment-artifact-compatibility.md)** — Supported four-platform matrix, Linux `3.15` kernel floor, fail-closed ABI/library checks, and `RCC_HOLOTREE_MODE=private`.
- **[Build Coordination & Prewarming](environment-build-coordination.md)** — `rcc env coordinate` claim/heartbeat/wait/release/prewarm JSON v1 contract and failure semantics.
- **[v18 Compatibility Contract](v18-compatibility.md)** — How Environment Artifacts v1 coexist with local v12 Holotree and `rccremote`.

## Trust, Revocation & Offline Archives

- **[Environment Artifact Trust & Object Inventory](environment-artifact-trust.md)** — Verification policies (`strict-remote` default vs. `--permissive-local`), trust roots, receipts, and the distinction between RCC's immutable object inventory (`application/vnd.rcc.environment.inventory.v1+json`) and a package SBOM.
- **[Authenticated Detached Metadata v2](authenticated-metadata-v2.md)** — Threat boundary, domain-separated Ed25519 metadata envelopes, independent metadata roots, and locked rollback sequence journals.
- **[Revocation Operator Lifecycle](revocation-lifecycle.md)** — Issuing, renewing, revoking, and distributing v2 metadata snapshots (`rcc env trust revocations`) without rebuilding Artifacts.
- **[Signed Offline Environment Archives](signed-offline-archives.md)** — Exporting `.rcca` archives with `--include-trust-attachments` and strictly acquiring offline with `--consume-archive-trust`.
- **[Embedded Artifact Trust for Robot Bundles](bundle-artifact-trust.md)** — Strict trust admission for source+`.rcca` robot bundles (`#252`; see [authenticated-metadata-v2.md](authenticated-metadata-v2.md) for subsequent `#253` v2 metadata enforcement).

## Operate & Troubleshoot

- **[Profile Configuration](profile_configuration.md)** — Managing multi-network proxies, certificates, and `settings.yaml` profiles via `rcc configuration`.
- **[Holotree Maintenance](maintenance.md)** — Inspecting, checking (`rcc holotree check`), and cleaning up Holotree spaces and catalogs.
- **[Troubleshooting](troubleshooting.md)** — Diagnostics commands (`rcc configuration diagnostics`, `netdiagnostics`, `tlsprobe`, `speedtest`) and known failure fixes.
- **[Vocabulary](vocabulary.md)** — Glossary of core RCC terms (blueprint, catalog, Hololib, Holotree, space).

## Architecture, Internals & Releases

- **[Holotree Deep Dive](holotree.md)** — Current v18 provider reference (top section) followed by the historical Holotree storage/relocation architecture essay and pre-v18.19 design notes.
- **[Release Process](release-process.md)** — Exact-source preparation, four-platform native acceptance, protected `rcc-production` publication, and binary signing limits.
- **[AI & CI Observability Runbook](ai-ops-runbook.md)** and **[Quality Metrics](metrics.md)** — Repository CI quality gates and coverage baselines (no user telemetry).

## Legacy Compatibility

- **[Cloud Commands](cloud-commands.md)** — Legacy Control Room (`rcc cloud`, `rcc robocorp`, `rcc assistant`) command reference. Cloud endpoints are empty by default in this fork unless explicitly configured.
- **[Features](features.md)** and **[Use Cases](usecases.md)** — Legacy overview lists (embedded in `rcc docs features` / `rcc docs usecases`).

## Historical & Proposals

- **[History of RCC](history.md)** — Lineage from Robocorp's 2020 open-source release through the community fork.
- **[Roadmap](roadmap.md)** — Historical `v18.12.0`-era decoupling roadmap and early proposals (preserved for context; see [capability-inventory.md](capability-inventory.md) for current `v18.19.11` state).
- **[Holotree Research Specs](holotree_specs/STARTING_POINT.md)** — Exploratory filesystem copy, reflink, and compression research notes (not shipped runtime behavior).

## Contribute & Verify

- **[Contributing Guide](../CONTRIBUTING.md)** and **[Build Notes](BUILD.md)** — Contained developer toolkit (`developer/toolkit.yaml`), Go `1.26.9` requirements, Dagger CI, and test gates.
- **[Agent Boundaries](agent-boundaries.md)**, **[Change Classification](change-classification.md)**, **[Review Rubric](review-rubric.md)**, and **[Repository Skills](skills/README.md)** — Verification and governance contracts for human and agent contributors.
