# Actions provider admission qualification

2026-11-26: **rejected; downstream changes required**. Actions default branch
`community` was at [7efb58eb1d28dc9ad66c46591f6d1710b6c84f0d](https://github.com/joshyorko/actions/commit/7efb58eb1d28dc9ad66c46591f6d1710b6c84f0d).

On Linux, the actual Python consumer functions from that commit were invoked
with the guarded candidate package and the supported `ACTIONS_RUNTIME_RCC_BINARY`
override in a private temporary directory. The two tested source files matched
their Git blob IDs. The candidate binary SHA-256 was
`88aab9fb1490bf33536ea5643f9222d856e9d59f516dbc971c78140108c244bf`;
the package SHA-256 was
`32be586f71cd5a93d95638458d314bf5ff6105727b08023ddf6eb590bdc826e8`.
The binary reports `v18.19.12`.

- [Package rewrite](https://github.com/joshyorko/actions/blob/7efb58eb1d28dc9ad66c46591f6d1710b6c84f0d/action_server/src/actions/server/_common/action_package_handling/__init__.py#L286) rejected the package: `dependencies is expected to be <class 'dict'>. Found: <class 'str'>`. It expects Actions' dependency mapping and rewrites it into a conda manifest, so the provider plan is not passed through.
- [Runtime override](https://github.com/joshyorko/actions/blob/7efb58eb1d28dc9ad66c46591f6d1710b6c84f0d/action_server/src/actions/server/_rcc_runtime_adapter.py#L697) rejected the binary: `RCC rcc failed: unsupported RCC version 'v18.19.12'`. The runtime requires `v18.19.8`; the [download pin](https://github.com/joshyorko/actions/blob/7efb58eb1d28dc9ad66c46591f6d1710b6c84f0d/action_server/src/actions/server/_download_rcc.py#L11) is also 18.19.8.
- The separate [managed compiler admission](https://github.com/joshyorko/actions/blob/7efb58eb1d28dc9ad66c46591f6d1710b6c84f0d/action_server/src/actions/server/deployments/package_revision_compiler.py#L975) statically requires exactly name, description and dependencies, and only conda-forge/pypi dependency keys. It was inspected, not executed.

Proposed downstream change: explicitly admit and preserve the provider-plan schema through package validation and rewriting, including the managed compiler if that path is intended; retain validation of allowed providers and inputs. Update the exact RCC pin and runtime version contract only when a compatible RCC release is authorized. Add consumer tests for both legacy packages and provider plans. No downstream files, release, or pin were changed. These failures occurred before environment construction; this is not runtime or production acceptance.
