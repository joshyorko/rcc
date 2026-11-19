# RCC — Own Your Execution Environments

[![Build (caching)](https://github.com/joshyorko/rcc/actions/workflows/rcc.yaml/badge.svg)](https://github.com/joshyorko/rcc/actions/workflows/rcc.yaml)
[![Lint](https://github.com/joshyorko/rcc/actions/workflows/lint.yml/badge.svg)](https://github.com/joshyorko/rcc/actions/workflows/lint.yml)
[![codecov](https://codecov.io/gh/joshyorko/rcc/branch/main/graph/badge.svg)](https://codecov.io/gh/joshyorko/rcc)
[![Release](https://img.shields.io/github/v/release/joshyorko/rcc)](https://github.com/joshyorko/rcc/releases)

![RCC](docs/rcc-logo.svg)

**RCC (Repeatable, Contained Code)** is a single-binary, vendor-neutral Go CLI
for **building, distributing, and verifying reproducible execution environments**.
Declare dependencies in `conda.yaml` (or `package.yaml`) and tasks in
`robot.yaml`; RCC resolves an isolated Python/Conda runtime without requiring
system Python, deduplicates files in its content-addressed **Holotree** store,
and can publish immutable `sha256:` **Environment Artifacts** with detached
Ed25519 signatures and revocation checks.

Actively maintained at [`joshyorko/rcc`](https://github.com/joshyorko/rcc) under
Apache 2.0, with **zero telemetry** and **no hard-coded cloud endpoints**.

---

## Why RCC?

1. **Build once, restore cleanly** — Embeds `micromamba 2.10.0` for Conda-Forge + PyPI environments and supports an on-demand `uv`-native fast path for pure Python specs. Every file is content-addressed in Hololib so identical dependencies are stored once and workspaces reset to a pristine state before runs.
2. **Distribute immutable environments separately from code** — Publish environments as canonical `sha256:` Environment Artifacts to a loopback/HTTPS provider (`rcc cache serve` + `rcc provider`) or export offline `.rcca` archives (`rcc env export`) up to `256 MiB` / `32,768` members—no shared writable filesystem required.
3. **Verify before execution** — `rcc env acquire` and `rcc env exec` default to fail-closed `--strict-remote` admission: deployment-owned Ed25519 Artifact trust roots (`--trust-roots`), independently signed v2 metadata envelopes (`--metadata-trust-roots`), retained rollback journals (`--metadata-state`), platform/ABI preflight, and process-scoped execution leases that resolve bare commands inside the acquired environment.

---

## Supported Platforms & Current Release

Current stable release: **[`v18.19.11`](https://github.com/joshyorko/rcc/releases/tag/v18.19.11)** (published `2026-11-10`, embedded `micromamba 2.10.0`).

| Platform | RCC Target | Release Binary | SHA-256 (`v18.19.11`) |
| --- | --- | --- | --- |
| Linux x86_64 | `linux_amd64` | `rcc-linux64` | `4d68609044d3a39a1728d571d54c77a6174215adb40eb9acd69ce5ad34a6db1f` |
| macOS Intel | `darwin_amd64` | `rcc-macos64` | `b3fe1fe2972136cfa58261f49cc4f5e9f12bed5041d49b2035561366b3d5ef6c` |
| macOS Apple Silicon | `darwin_arm64` | `rcc-macosarm64` | `23c665387951289b16a3d9be3103498598dce94d55327e103758df944ca067e7` |
| Windows x86_64 | `windows_amd64` | `rcc-windows64.exe` | `58f43a3b3c54032740f224cb4a8bbf68c47c865cf2652084fb139b8ecc01cfd0` |

> **Platform scope:** Environment Artifacts are platform-specific and natively
> qualified on the four targets above. Linux `arm64` is **not** a released target.
> Releases publish SHA-256 checksums and exact-source provenance receipts rather
> than vendor code-signing signatures (see [Release Process](docs/release-process.md#signing-limits)).

---

## Install RCC (`v18.19.11`)

### Linux x86_64 (user-scoped, no `sudo` required)

```sh
mkdir -p "$HOME/.local/bin"
curl --fail --show-error --location \
  https://github.com/joshyorko/rcc/releases/download/v18.19.11/rcc-linux64 \
  --output "$HOME/.local/bin/rcc"
printf '%s  %s\n' \
  '4d68609044d3a39a1728d571d54c77a6174215adb40eb9acd69ce5ad34a6db1f' \
  "$HOME/.local/bin/rcc" | sha256sum --check -
chmod +x "$HOME/.local/bin/rcc"
rcc --version
```

Ensure `$HOME/.local/bin` is on your `PATH` (or move the binary to `/usr/local/bin/rcc` for a system-wide install).

### macOS (user-scoped, no `sudo` required)

```sh
mkdir -p "$HOME/.local/bin"

# Apple Silicon (darwin_arm64):
ASSET="rcc-macosarm64"
SHA256="23c665387951289b16a3d9be3103498598dce94d55327e103758df944ca067e7"

# Intel (darwin_amd64) — uncomment to use instead:
# ASSET="rcc-macos64"
# SHA256="b3fe1fe2972136cfa58261f49cc4f5e9f12bed5041d49b2035561366b3d5ef6c"

curl --fail --show-error --location \
  "https://github.com/joshyorko/rcc/releases/download/v18.19.11/${ASSET}" \
  --output "$HOME/.local/bin/rcc"
printf '%s  %s\n' "$SHA256" "$HOME/.local/bin/rcc" | shasum -a 256 --check -
chmod +x "$HOME/.local/bin/rcc"
rcc --version
```

### Windows x86_64 (PowerShell, user-scoped)

```powershell
$Dir = "$env:LOCALAPPDATA\rcc\bin"
New-Item -ItemType Directory -Force -Path $Dir | Out-Null
$Out = Join-Path $Dir "rcc.exe"
Invoke-WebRequest -Uri "https://github.com/joshyorko/rcc/releases/download/v18.19.11/rcc-windows64.exe" -OutFile $Out
$Expected = "58f43a3b3c54032740f224cb4a8bbf68c47c865cf2652084fb139b8ecc01cfd0"
if ((Get-FileHash $Out -Algorithm SHA256).Hash.ToLower() -ne $Expected) { throw "SHA-256 mismatch" }
& $Out --version
```

Add `$env:LOCALAPPDATA\rcc\bin` to your user `PATH` to invoke `rcc` from any terminal.

### Homebrew (macOS & Linux x86_64)

```sh
brew tap joshyorko/tools
brew install --cask rcc
```

Brewfile entry:

```ruby
tap "joshyorko/tools"
cask "rcc"
```

---

## First Run (Credential-Free, Tested on `v18.19.11`)

Create a minimal three-file project and run it—no host Python installation or
cloud account required:

```sh
mkdir -p rcc-hello && cd rcc-hello

cat > conda.yaml <<'EOF'
channels:
  - conda-forge
dependencies:
  - python=3.12.11
EOF

cat > robot.yaml <<'EOF'
tasks:
  Hello:
    shell: python task.py
condaConfigFile: conda.yaml
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
EOF

cat > task.py <<'EOF'
import platform
import sys

print(f"Hello from RCC! Python {platform.python_version()} ({sys.executable})")
EOF

rcc run
```

Observed output (`rcc v18.19.11`, Linux `amd64`):

```text
####  Progress: 02/15  v18.19.11     0.000s  Holotree blueprint is "e64246d31bd41a89" [linux_amd64 ... from "conda.yaml"].
...
Note: Extracting micromamba binary from inside rcc.
####  Progress: 07/15  v18.19.11     0.174s  Running micromamba phase. (micromamba v2.10.0) [layer: e64246d31bd41a89]
...
####  Progress: 14/15  v18.19.11     0.212s  Restore space from library ...
...
Hello from RCC! Python 3.12.11 (<ROBOCORP_HOME>/holotree/.../bin/python3.12)
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@@@  From rcc "v18.19.11" (controller: "user") point of view, "actual main robot run" was SUCCESS. @@@
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
OK.
```

On the second `rcc run`, RCC skips the build steps and restores directly from
the cached Hololib layer. Want to publish, acquire, and execute this same
environment as an immutable `sha256:` Environment Artifact next? Continue in the
**[Quickstart](docs/quickstart.md#3-publish-acquire-and-execute-a-local-environment-artifact)**,
or run `rcc create` interactively to scaffold from community templates in
[`joshyorko/robot-templates`](https://github.com/joshyorko/robot-templates).

---

## Security & Operational Boundaries

- **Not an OS or process sandbox:** RCC isolates Python/Conda dependencies and resolves bare commands in `rcc env exec` inside the acquired environment ([executable resolution](docs/environment-executable-resolution.md)), but tasks run as normal host processes. Use a container or OS sandbox (Docker, Podman, `bwrap`) when you need filesystem, network, or syscall isolation.
- **Object inventory, not a package SBOM:** `rcc env publish` emits an immutable Hololib object inventory (`application/vnd.rcc.environment.inventory.v1+json`), not an SPDX or CycloneDX installed-package SBOM, license inventory, or CVE scan ([trust docs](docs/environment-artifact-trust.md#rcc-object-inventory-and-package-sbom)).
- **Separate transport, signing, and metadata authority:** Reverse-proxy HTTP `Authorization` headers authenticate transport only. Strict remote acquisition (`--strict-remote`, the default) requires deployment-owned Ed25519 Artifact roots (`--trust-roots`), independent v2 metadata roots (`--metadata-trust-roots`), and a retained rollback sequence journal (`--metadata-state`). See [Providers & Endpoints](docs/providers.md#signed-artifacts-for-remote-clients).
- **Telemetry disabled by default:** No background metrics are sent, the installation ID header is not attached to outbound HTTP requests, and `rcc configure identity` reports tracking disabled.

---

## Documentation Map

| Topic | Start Here |
| --- | --- |
| **Start & First Success** | [Quickstart](docs/quickstart.md) · [Documentation Hub](docs/README.md) · [Recipes (`robot.yaml` / `conda.yaml`)](docs/recipes.md) |
| **Capability & Status Truth** | [Capability & Documentation Inventory](docs/capability-inventory.md) · [v18 Compatibility](docs/v18-compatibility.md) · [Changelog](docs/changelog.md) |
| **Distribute & Host Providers** | [Providers, Reverse Proxy & Custom Endpoints](docs/providers.md) · [Platform Compatibility](docs/environment-artifact-compatibility.md) · [Build Coordination](docs/environment-build-coordination.md) |
| **Trust, Revocation & Offline** | [Artifact Trust & Object Inventory](docs/environment-artifact-trust.md) · [Authenticated Metadata v2](docs/authenticated-metadata-v2.md) · [Revocation Lifecycle](docs/revocation-lifecycle.md) · [Signed Offline `.rcca` Archives](docs/signed-offline-archives.md) |
| **Holotree & Troubleshooting** | [Holotree Deep Dive](docs/holotree.md) · [Embedded Micromamba](docs/micromamba.md) · [Profiles](docs/profile_configuration.md) · [Troubleshooting](docs/troubleshooting.md) |
| **Legacy Compatibility** | [Cloud / Control Room Commands](docs/cloud-commands.md) · [`rccremote` compatibility](docs/v18-compatibility.md) |
| **Releases & Contributing** | [Releases](https://github.com/joshyorko/rcc/releases) · [Release Process](docs/release-process.md) · [Contributing Guide](CONTRIBUTING.md) · [Build Notes](docs/BUILD.md) |

Built-in manuals are also embedded directly in the binary—run `rcc docs --help`
(for example `rcc docs changelog` or `rcc docs recipes`).

---

## Acknowledgements & License

RCC was originally created by the Robocorp team and released as open source
under the Apache 2.0 license. Historical open-source lineage through the last
upstream revision is preserved in [`admariner/rcc`](https://github.com/admariner/rcc);
this fork (`joshyorko/rcc`) is the active, independently maintained open-source
continuation.

Licensed under the [Apache License 2.0](LICENSE).
