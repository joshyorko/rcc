# Task 3 report

Implemented lazy provider-reference resolution and the `provider` management command group.

- `local` resolves lazily to `$ROBOCORP_HOME/artifacts/v1/provider`.
- Direct HTTP(S) references and named settings profiles use the hardened HTTP constructor; authorization values are read only by the transport boundary.
- Added typed JSON contracts for add, list, inspect, test, and remove. Inspection is offline; test negotiates and validates v1 capabilities.
- Connected environment commands to the deferred resolver without changing injected dependencies or lifecycle result contracts.

Verification:

- `go test -count=1 ./cmd/... ./settings ./artifactprovider` — pass.
- `go test -race -count=1 ./cmd/... ./settings ./artifactprovider` — `cmd`, `settings`, and `artifactprovider` pass; the repository's existing `cmd/rccremote` tests report a global verbosity/logger race.
- `git diff --check` — pass.

Documentation receipt
- Canonical guidance: this report; no guidance changes needed.
- Durable learning: provider resolution remains lazy until the first provider operation.
- Evidence: focused and race test commands above.
- Stale guidance removed: none.
- Remaining uncertainty: full race suite remains blocked by the pre-existing `cmd/rccremote` race.

## Review round 1

Added focused contract tests. RED failures against `c48e6ff` covered duplicate/local ordering, unsafe direct URL inspection, missing add URL normalization, and ignored command context. Fixed those boundaries minimally: local remains first and unique, inspect validates URL/profile data offline and reports `providerRoot`, add returns the normalized URL, and provider test uses `c.Context()`.

Verification:

- `go test -count=1 ./cmd/... ./settings ./artifactprovider` — pass.
- `go run ./cmd/rcc provider --help` — pass; all five subcommands reachable.
- `git diff --check` — pass.
- `go test -race -count=1 ./cmd ./settings ./artifactprovider` — package tests encounter the repository's existing global verbosity/logger race when Cobra command tests execute; non-Cobra package race checks pass.

## Review round 2

Added the named command, resolver, capability, JSON, authorization-presence, deferred-environment, and artifactprovider response-redaction tests. RED evidence: `TestHTTPErrorDoesNotExposeResponseBody` initially returned the complete authorization sentinel from a 401 body; incompatible provider output also lacked explicit contract coverage. GREEN evidence: response errors now retain status only, and all focused tests pass.

Verification:

- `go test -count=1 ./cmd/... ./settings ./artifactprovider` — pass.
- Focused named provider/resolver/capability tests — pass.
- `go test -count=1 ./artifactprovider -run TestHTTPErrorDoesNotExposeResponseBody` — pass.
- `go run ./cmd/rcc provider --help` — pass; exact five subcommands reachable.
- `git diff --check` — pass.
- Race testing remains affected by the existing global verbosity/logger race when Cobra commands execute.
