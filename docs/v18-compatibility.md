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
