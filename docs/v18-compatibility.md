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

## Stable boundary and deferred seams

The supported v18.19.0 integration boundary is the RCC executable and its
versioned machine-readable CLI/JSON output. The `artifacttrust` and
`buildcoord` Go packages are internal implementation seams, not a promise that
arbitrary host processes can embed RCC or that Actions can invoke a stable
fleet-coordination API in this release.

The following work is explicitly outside the v18.19.0 stable contract and does
not weaken the local/provider artifact lifecycle above:

- resumable/chunked multi-GB transfers, quotas, retention administration,
  provider GC, and a second shared/object provider implementation;
- enforced provenance, SBOM, signing, revocation, and verification receipts at
  acquisition or execution time;
- remote build claims, fleet prewarming, disk reservation, and deployment
  readiness APIs; and
- storage/materializer changes such as zstd, packfiles, FUSE, reflinks, or
  hardlinks. The v18.19.0 decision is to ship no such optimization without a
  representative multi-platform benchmark showing material benefit.

These are future additive contracts. Local zero-configuration use, v12
Holotree, the v1 artifact identity, and the separate `rccremote` protocol do
not depend on them.
