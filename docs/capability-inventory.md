# RCC Capability & Documentation Inventory

This page is the source-backed status inventory for `joshyorko/rcc`. It separates
what is shipped in the current release from unmerged experiments, legacy
compatibility interfaces, and open proposals.

- **Audited baseline:** `main` commit `953eda18d3d1f4a286c17709f282315a2ed16a16` (tag **`v18.19.11`**, published `2026-11-10`)
- **Supported release targets:** `linux_amd64`, `darwin_amd64`, `darwin_arm64`, `windows_amd64` (no Linux `arm64` release or Environment Artifact lifecycle claim)
- **Embedded micromamba:** `2.10.0` (upstream build `2.10.0-0`, verified by SHA-256 at build time; see [micromamba.md](micromamba.md))

---

## Status Classification

### 1. RELEASED (`v18.19.11`)

Every item below is implemented in current `main` and shipped in the published
`v18.19.11` binaries.

| Capability | CLI / Interface | Scope & Evidence |
| --- | --- | --- |
| Local declarative environments & execution | `rcc run`, `rcc task run`, `rcc task testrun`, `rcc task shell`, `rcc task script` | Builds isolated Python/Conda environments from `robot.yaml` + `conda.yaml` (or `package.yaml`), caches layers in Hololib, and runs tasks without host Python (`cmd/`, `operations/`, `conda/`, `htfs/`). |
| Embedded micromamba & `uv`-native fast path | `conda.yaml` with `channels:` (micromamba `2.10.0`) or without `channels:` (`uv` on-demand) | Micromamba `2.10.0` is embedded in the binary and extracted locally; `uv`-native mode downloads and caches `uv` at runtime (`blobs/embedded.go`, [micromamba.md](micromamba.md)). |
| Portable Environment Artifacts v1 | `rcc env publish`, `rcc env acquire`, `rcc env exec`, `rcc env lifecycle inspect\|repair\|verify` | Content-addressed immutable environment manifests (`sha256:`) separate from code/source, with atomic materialization, warm local reuse, and process-scoped leases (`environmentartifact/`, `environmentlifecycle/`, [v18-compatibility.md](v18-compatibility.md)). |
| Local HTTP artifact provider & named profiles | `rcc cache serve`, `rcc provider add\|list\|inspect\|test\|remove` | Loopback-only immutable HTTP provider (`127.0.0.1` / `::1`); named client profiles store only the `--authorization-env` variable name (never the secret), reject redirects, and require HTTPS for non-loopback endpoints (`artifactprovider/`, `cmd/provider*.go`, [providers.md](providers.md)). |
| Strict Artifact trust & authenticated detached metadata v2 | `--strict-remote` (default), `--trust-roots`, `--metadata-trust-roots`, `--metadata-state`, `rcc env trust verify\|sign-metadata` | Shipped in `v18.19.9` (`#253`, PR `#268`). Strict admission requires Ed25519 Artifact signatures plus domain-separated Ed25519-signed v2 metadata envelopes and a locked local rollback sequence journal (`artifacttrust/trust.go`, [authenticated-metadata-v2.md](authenticated-metadata-v2.md)). |
| Operator revocation lifecycle | `rcc env trust revocations issue\|renew\|revoke\|distribute` | Shipped in `v18.19.10` (`#254`, PR `#270`). Issues, renews, revokes (by Artifact digest or Artifact signer key ID), and distributes v2 metadata snapshots without rebuilding or republishing the Artifact (`cmd/environmentRevocation.go`, [revocation-lifecycle.md](revocation-lifecycle.md)). |
| Signed offline `.rcca` archives | `rcc env export --include-trust-attachments`, `rcc env acquire --archive ... --consume-archive-trust` | Shipped in `v18.19.10` (`#255`, PR `#275`). Exports bounded canonical `.rcca` archives with detached trust evidence and consumes archive-scoped evidence under deployment-owned roots and sequence journals ([signed-offline-archives.md](signed-offline-archives.md)). |
| Truthful RCC object inventory | `rcc env publish --inventory` (`--sbom` deprecated alias) | Shipped in `v18.19.10` (`#257`, PR `#271`). Emits `application/vnd.rcc.environment.inventory.v1+json` for stored Hololib objects; explicitly **not** an SPDX/CycloneDX installed-package SBOM ([environment-artifact-trust.md](environment-artifact-trust.md)). |
| Strict executable resolution inside acquired environments | `rcc env exec -- <cmd> [args...]` | Shipped in `v18.19.9` (`#258`, PR `#264`). Bare non-Python commands resolve only inside `conda.CondaPaths(materialization.Path)`; host `PATH` cannot shadow environment executables; explicit absolute paths remain the host-delegation escape hatch ([environment-executable-resolution.md](environment-executable-resolution.md)). |
| Embedded Artifact robot bundles & output contract | `rcc robot bundle`, `rcc robot run-from-bundle` | Shipped in `v18.19.9` (`#252` PR `#266`, `#256` PR `#265`). Embedded `.rcca` bundles default to strict trust, reject source-only downgrade when `--strict-remote` is explicit, and preserve flushed `artifactsDir` outputs and separated stdout/stderr before workarea cleanup ([bundle-artifact-trust.md](bundle-artifact-trust.md), [bundle-output-contract.md](bundle-output-contract.md)). |
| Optional build coordination & prewarming CLI | `rcc env coordinate claim\|heartbeat\|wait\|release\|prewarm` | Shipped in `v18.19.5` with lock-contention hardening in `v18.19.10` (`#273`, PR `#274`). Advisory build claims with monotonic epochs; verified Artifacts remain the sole authority (`buildcoord/`, [environment-build-coordination.md](environment-build-coordination.md)). |
| Vendor-neutral defaults & disabled telemetry | `RCC_ENDPOINT_*`, `RCC_AUTOUPDATES_*`, `$ROBOCORP_HOME/settings.yaml`, `rcc configuration diagnostics` | Telemetry is disabled across product modes; cloud endpoints are empty by default unless explicitly configured (`assets/robocorp_settings.yaml`, [providers.md](providers.md)). |
| Two-stage exact-source release preparation & publication | `Rcc` + `Publish RCC Release` + `Export RCC Preparation Review Evidence` workflows | Manual exact-`main` preparation with four-platform native acceptance, protected `rcc-production` approval gate, and optional read-only readiness receipts ([release-process.md](release-process.md)). |

### 2. PROVEN ON MAIN (Post-`v18.19.11` Delta)

At baseline commit `953eda18d3d1f4a286c17709f282315a2ed16a16`, `main` is identical
to tag `v18.19.11`. There are no unreleased runtime changes on `main` beyond
`v18.19.11`.

> **Note on earlier issue snapshots:** Issue `#259` was drafted on `2026-10-16`
> against `v18.19.7`. Releases `v18.19.8` through `v18.19.11` subsequently shipped
> fixes for `#252` (strict bundle trust), `#253` (authenticated metadata v2),
> `#254` (revocation lifecycle), `#255` (signed offline archive export/consume CLI),
> `#256` (bundle output preservation), `#257` (truthful object inventory media type),
> `#258` (environment-scoped command resolution), and micromamba `2.10.0`.

### 3. EXPERIMENTAL / UNMERGED

These items exist in open pull requests and have **not** merged or shipped:

| Item | Location | Status |
| --- | --- | --- |
| S3 / generic object-storage CAS backend | `joshyorko/rcc` PR `#224` (`feat/rcc-object-storage-cas`) | Open PR; not on `main`. |
| Generic Room tree artifact engine (`treeartifact/`) | `joshyorko/rcc` PR `#225` (`experiment/room-artifact-v2`) | Open experimental PR; not on `main`. |
| Evidence-backed Go deadcode review workflow | `joshyorko/rcc` PR `#283` (`patchraptor/implement-issue-278`) | Open dev-tooling PR; not on `main`. |
| Release metadata GitHub App manifest setup helper | `joshyorko/rcc` PR `#290` (`feat/286-app-manifest-setup`) | Draft release-tooling PR; not on `main`. |
| Clean Room cloud-native `.rcca` showcase template | `joshyorko/robot-templates` PR `#6` (`07-python-cloud-native/`) | Open PR in separate repo; not shipped in `robot-templates` `v15.0.0`. |

### 4. LEGACY COMPATIBILITY

Supported for backward compatibility with existing v18 workflows, not the
recommended architecture for new self-hosted Environment Artifact deployments:

| Interface | Compatibility Status | Notes |
| --- | --- | --- |
| `rccremote` binary & `/parts`, `/delta`, `rcc ht pull` | Compatibility level A for v18 | Retained alongside `rcc cache serve` / `rcc env`; uses `RCC_REMOTE_ORIGIN` and `RCC_REMOTE_AUTHORIZATION` ([v18-compatibility.md](v18-compatibility.md)). |
| `rcc cloud` / `rcc robocorp` / `rcc assistant` | Compatibility only | Control Room API endpoints are empty by default in this fork; requires explicit `RCC_ENDPOINT_CLOUD_*` or `settings.yaml` ([cloud-commands.md](cloud-commands.md)). |
| `--permissive-local` trust policy | Controlled local dev / migration only | Explicitly opts into unsigned local artifacts; must not be used for remote or production admission ([environment-artifact-trust.md](environment-artifact-trust.md)). |
| Legacy v12 `hololib.zip` (`rcc ht export` / `rcc ht import`) | Supported v18 local/air-gapped format | Predates canonical `.rcca` Environment Artifacts and detached trust envelopes. |
| Legacy inventory media type `application/vnd.rcc.environment.sbom.spdx+json` | Read-compatible | Readers preserve historical signed bytes without relabeling; new records emit `application/vnd.rcc.environment.inventory.v1+json` ([environment-artifact-trust.md](environment-artifact-trust.md)). |
| `rcc pull` community shorthand | Supported in legacy product mode | Expands short GitHub repo paths (defaulting single-segment names to `robocorp/`); prefer explicit `https://` URLs or `joshyorko/robot-templates` (`cmd/communitypull.go`, `operations/community.go`). |

### 5. PROPOSED / OPEN ISSUES

| Issue | Scope |
| --- | --- |
| `joshyorko/rcc#272` | Generate a real, schema-validated SPDX or CycloneDX installed-package SBOM separately from RCC's immutable object inventory. |
| `joshyorko/rcc#118` | Connect named provider-profile settings with HTTP trust-carrier authentication/selection. |
| `joshyorko/rcc#125` | Post-v18.19 materializer/storage performance research (e.g., hardlinks, reflinks, packfiles — non-blocking, unshipped). |
| `joshyorko/rcc#259` (later phases) | Versioned GitHub Pages documentation site (Lane C), `robot-templates` root catalog & sanitized Clean Room evidence replay (Lane D), and Action Server robot execution vs. artifact-backed runtime guide (Lane E). |

---

## Verified Security, Platform, and Archive Boundaries

| Boundary | Actual Behavior in `v18.19.11` | Source / Doc Authority |
| --- | --- | --- |
| **Not an OS or process sandbox** | RCC isolates Python/Conda dependencies and restricts bare command lookup in `rcc env exec` to the acquired environment, but tasks run as normal host processes with host filesystem/network access unless wrapped in a container or OS sandbox (e.g., Docker, Podman, `bwrap`). | [environment-executable-resolution.md](environment-executable-resolution.md), [agent-boundaries.md](agent-boundaries.md) |
| **Object inventory != package SBOM** | Generated inventory (`application/vnd.rcc.environment.inventory.v1+json`) lists immutable Hololib storage objects and the RCC builder. It does **not** enumerate installed Conda/pip/uv packages, licenses, or CVE status. | [environment-artifact-trust.md](environment-artifact-trust.md), `#272` |
| **Three separate security layers** | (1) HTTP `--authorization-env` authenticates transport to a TLS reverse proxy only. (2) `--signing-key` / `--trust-roots` verifies Ed25519 Artifact identity signatures. (3) `--metadata-trust-roots` / `--metadata-state` verifies independent v2 metadata snapshots and monotonic rollback sequences. None substitutes for another. | [authenticated-metadata-v2.md](authenticated-metadata-v2.md), [revocation-lifecycle.md](revocation-lifecycle.md) |
| **Revocation & offline limits** | Revocations gate new acquisitions and new leases; an already-running child completes under its original lease decision. Offline freshness is bounded by signed snapshot expiry (max 24 hours) and the independently provisioned `minimumSequence` floor, not global real-time revocation. Host compromise, disk rollback, and power-loss durability are outside RCC's guarantees. | [revocation-lifecycle.md](revocation-lifecycle.md), [authenticated-metadata-v2.md](authenticated-metadata-v2.md) |
| **Platform matrix** | Native release binaries and A→B Environment Artifact runtime proofs cover `linux_amd64`, `darwin_amd64`, `darwin_arm64`, and `windows_amd64`. Artifacts are platform-specific; Linux `arm64` is not a released target. Linux compatibility uses kernel floor `3.15` (`renameat2` `RENAME_NOREPLACE`) plus fail-closed libc/ELF/CPU/Python ABI checks. | [environment-artifact-compatibility.md](environment-artifact-compatibility.md) |
| **`.rcca` archive limits** | Enforced fail-closed: max encoded archive `256 MiB`, max cumulative uncompressed `256 MiB`, max member `256 MiB`, max `32,768` members, max compression ratio `1000:1`. Metadata v2 limits: `2 MiB` payload, `3 MiB` envelope, `8 MiB` journal. | `environmentartifact/archive.go`, [authenticated-metadata-v2.md](authenticated-metadata-v2.md) |
| **Release binary signing** | Releases publish SHA-256 checksums and exact-source provenance receipts, not vendor code-signing signatures for Linux, Windows, or macOS Intel (macOS arm64 carries Go's linker ad hoc integrity signature only). | [release-process.md](release-process.md) |

---

## Known Documentation Contradictions & Tracker Drift

Audited on `2026-11-19` so maintainers and later documentation phases know which
historical notes lag behind `v18.19.11`:

1. **[roadmap.md](roadmap.md)** preserves the `v18.12.0`-era decoupling roadmap and still lists `v18.12.0` as "In Progress" and `rccremote` UX as Phase 2, predating Environment Artifacts v1 (`v18.19.0`–`v18.19.11`). Marked as historical/proposed.
2. **[holotree.md](holotree.md)** mixes current v18 Environment Artifact provider docs (top section) with the pre-v18.19 deep-dive essay and improvement proposals (Parts I–IV). The `--strict-remote` CLI snippets were updated to reflect v2 metadata requirements; Parts I–IV are preserved with a scope banner.
3. **[bundle-artifact-trust.md](bundle-artifact-trust.md)** still contains `#253` baseline notes stating detached metadata authentication is unresolved; `#253` subsequently shipped in `v18.19.9` (see [authenticated-metadata-v2.md](authenticated-metadata-v2.md)).
4. **[environment-build-coordination.md](environment-build-coordination.md)** retains an early `v18.19.0` banner saying `buildcoord` is only an internal seam, even though `rcc env coordinate` shipped in `v18.19.5` and is documented directly below that banner.
5. **Open tracker items `#255` and `#257`:** Both issues remain open on GitHub even though PR `#275` (public `.rcca` `--include-trust-attachments` / `--consume-archive-trust`) and PR `#271` (truthful `application/vnd.rcc.environment.inventory.v1+json`, splitting package SBOM work to `#272`) shipped in `v18.19.10`.
