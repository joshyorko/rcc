# RCC v18 compatibility contract

RCC v18.19.0 adds Environment Artifacts v1 without changing the existing v18
local or remote contracts.

| Contract | v18.19.0 guarantee |
| --- | --- |
| Local operation | `rcc run`, v12 catalogs, export/import, bundles, and local Holotree behavior remain supported and provider-free. |
| Environment Artifacts | `rcc env publish`, `rcc env acquire`, `rcc env exec`, and `rcc cache serve` use the v1 Manifest/Object Index contract. |
| Providers | Named profiles are additive; credentials are referenced by environment-variable name and local operation does not require provider configuration. |
| Legacy remote | `rccremote` remains a separate binary and protocol; `/parts`, `/delta`, `RCC_REMOTE_ORIGIN`, and `RCC_REMOTE_AUTHORIZATION` remain supported. |
| Home boundary | No mandatory `ROBOCORP_HOME` migration is introduced. |
| Runtime support | The proven artifact lifecycle is Linux-first; other platforms retain their existing v18 support, but are not implied to have v1 lifecycle parity. |
| Release assets | Each release retains `rcc` and `rccremote` for Linux amd64, macOS amd64, macOS arm64, and Windows amd64. |

Environment Artifact identity does not include provider references, provider
locations, credentials, materialization paths, or RCC process IDs.

### Consumer lifecycle contract

`rcc env publish` accepts exactly one source through `--environment` (a
`package.yaml` or `robot.yaml`) or the compatibility alias `--robot`. The
source kind is part of the semantic Specification identity; provider URLs and
source paths are not. `rcc env exec --json -- <command>` keeps child output
away from its single JSON receipt. Long-lived protocol consumers must opt into
`--inherit-streams --receipt-file <path>`: RCC connects stdin/stdout/stderr for
the child lifetime, keeps the artifact lease until the child is reaped, and
atomically installs the typed receipt at the caller-selected path after exit.
Receipts report `completed`, `failed`, or `cancelled` status with a reason and
exit code when available. Child protocol bytes therefore never contaminate the
machine receipt, including on non-zero exit or cancellation.

## Program completion boundary

The supported v18.19.0 integration boundary remains the RCC executable and its
versioned machine-readable CLI/JSON output; this release does not promise an
arbitrary embeddable-Go-library API. That boundary does not reduce the
Environment Artifacts program scope.

Before v18.19.0 can be released, the RCC-owned acceptance criteria in #121,
#122, #123, #124, #126, and #127 must be implemented and proven: full
compatibility rejection,
lease/crash/repair/GC, production providers and a second provider,
deterministic offline carrier convergence, executable trust and revocation,
and generic build coordination/prewarming. Internal `artifacttrust` or
`buildcoord` seams alone do not satisfy those contracts.

#125 remains open as post-v18.19 materializer/storage performance research. Its
comparative benchmark and optimization decision do not block this release.

Optional experiments remain conditional on evidence and explicit issue
language: OCI, FUSE, zstd, packfiles, reflinks, and hardlinks are not mandatory
for v18.19. Kubernetes, a
broker, a shared writable fleet Holotree, a TUI, and replacement of
`rccremote` remain non-goals.
