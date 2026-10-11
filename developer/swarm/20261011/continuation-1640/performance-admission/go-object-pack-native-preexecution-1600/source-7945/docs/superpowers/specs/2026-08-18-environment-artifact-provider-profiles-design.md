# Environment Artifact Provider Profiles Design

## Status

Approved in chat on 2026-08-18 after archaeology of the promoted Environment
Artifacts v1 implementation, the legacy `rccremote` line, RCC settings and
diagnostics, bundle/export/import behavior, issue #118, and the operational
`rccremote-docker` exploration. The approval included ten security and
compatibility guardrails recorded below.

This design extends the promoted release-compatibility baseline
`816a8f63b55dd87811449d73f2c86c9e5f9c933e`. It does not reopen the accepted
Manifest, Object Index, v12 materialization, lease, or execution-handle
contracts.

## Objective

Give normal users and future trusted Action worker adapters a coherent way to
name, inspect, validate, and use Environment Artifact providers without
memorizing URLs or secret-bearing environment variables.

The result must preserve:

- provider-independent Artifact identity;
- fully local and warm operation without a service;
- the existing `artifactprovider.Provider` interface;
- direct HTTP(S) URL compatibility for current Environment Artifact commands;
- existing `rcc run`, v12 Holotree, export/import, and bundle behavior; and
- the separately shipped legacy `rccremote` binary and protocol.

## Non-goals

This slice does not implement Action Server or Action Runtime integration,
FUSE, OCI, object-storage providers, zstd, packfiles, provider GC, production
server authentication, tags, mutable aliases, a TUI, or a new Artifact schema.

It does not merge the legacy and Environment Artifact servers, change legacy
catalog or Hololib identities, or persist provider credential values.

## Archaeology conclusions

### Environment Artifact provider

`artifactprovider.Provider` already owns the correct transport-neutral
operations: capabilities, manifest resolution, missing-object negotiation,
object put/get, and atomic manifest commit. Profiles are command/configuration
references that select an implementation of that interface. They do not
belong in the interface and do not enter Manifest identity.

The current HTTP client already accepts an injected `http.Client`; this is the
auth, TLS, proxy, and redirect policy seam. The current lifecycle verifies a
local-ready materialization before it needs a remote provider. The profile
resolver must preserve and strengthen that ordering.

### RCC configuration

`settings.yaml` already owns effective network, certificate, endpoint, and
configuration-profile behavior. Provider profiles are network configuration
and therefore extend `settings.Settings` with a `providers` map. Provider
mutation commands operate only on the custom `settings.yaml` layer. They must
never serialize `SummonSettings()` or any other merged effective view.

### Legacy rccremote decision

The release classification is:

> **A. KEEP LEGACY BINARY UNCHANGED FOR v18**

`rccremote` serves the legacy shared-Holotree `/parts`, `/delta`, and
`/force` protocol. It uses legacy catalog/object identifiers and ZIP import,
while the Environment Artifact server uses cryptographic descriptors and
atomic Manifest closure. There is no material implementation duplication whose
removal justifies coupling these trust boundaries in v18.

The following remain unchanged:

- `rccremote` executable names, flags, defaults, and release assets;
- the shared-Holotree startup requirement;
- `/parts`, `/delta`, and `/force`;
- `RCC_REMOTE_ORIGIN`;
- `RCC_REMOTE_AUTHORIZATION` as the optional complete legacy Authorization
  header value; and
- legacy ZIP download and `ProtectedImport` behavior.

The `rccremote-docker` work confirms that deployment, health, Rails/Kamal,
Cloudflare, catalog-management, and shared-volume concerns are an optional
operational layer. None enters this provider-profile contract.

## User-facing commands

The new command group is:

```text
rcc provider add <name> --type http --url <url>
    [--authorization-env <variable>] [--replace] [--json]
rcc provider list [--json]
rcc provider inspect <reference> [--json]
rcc provider test <reference> [--json]
rcc provider remove <name> [--json]
```

Environment commands continue to accept `--provider`, now as a provider
reference:

```text
rcc env publish --provider office --robot robot.yaml --json
rcc env acquire --provider office --artifact sha256:... --json
rcc env exec --provider office --artifact sha256:... --json -- <command>
```

Existing explicit HTTP(S) URLs remain accepted. `--provider` remains optional
for acquire and exec only when the requested Artifact is already locally
ready. Publish still requires a provider reference; `local` satisfies that
requirement without a service.

Provider-management commands have concise human output by default and stable
JSON with `--json`. Diagnostics go to stderr; JSON results go to stdout.

## Provider reference resolution

References resolve deterministically in this order:

1. the reserved exact reference `local`;
2. an explicit canonical HTTP(S) URL; or
3. a canonical provider profile name.

No profile name may contain `:`, `/`, `?`, `#`, `@`, or another
character that could make it ambiguous with an HTTP(S) URL. Names are lowercase
ASCII and match:

```text
^[a-z0-9][a-z0-9._-]{0,62}$
```

`local` is reserved and cannot be added, replaced, or removed. Profile names
are case-sensitive at the parser boundary; non-lowercase input is rejected, not
silently normalized.

A provider reference, URL, tag, or profile name is a location/configuration
reference only. It is never an Artifact identity and is not added to Manifest
identity-bearing content.

## Local provider

`local` is an RCC-owned zero-configuration filesystem
`artifactprovider.Provider` rooted at:

```text
$ROBOCORP_HOME/artifacts/v1/provider
```

The path is derived through the active RCC product-home abstraction before the
provider is initialized. It is intentionally distinct from:

```text
$ROBOCORP_HOME/artifacts/v1/content
```

which is the worker-local verified acquisition cache, and from Holotree
materialization paths. Publishing to `local` therefore creates an explicit
local provider store rather than treating derived cache state as a publication
contract.

`rcc cache serve --root ...` remains the explicit way to expose a filesystem
provider over loopback HTTP. It is not an alias for `local`, and neither is an
alias for `rccremote`.

## Settings schema and mutation

The custom settings layer gains:

```yaml
providers:
  office:
    type: http
    url: https://cache.example
    authorization-env: RCC_PROVIDER_OFFICE_AUTHORIZATION
```

V1 supports only `type: http`; that type covers the existing HTTP(S) Artifact
protocol. Unknown types and empty or unknown fields fail validation.

`authorization-env` stores only an environment-variable name. The variable's
runtime value, when present, is the complete HTTP `Authorization` header value,
for example `Bearer ...`. RCC does not prepend a scheme, parse the credential,
persist the value, or include it in JSON, diagnostics, errors, traces, or logs.

Provider mutation follows this exact sequence:

1. open the custom `settings.yaml` layer, or create an empty custom
   `Settings` value when it does not exist;
2. reject a symlink or non-regular custom settings destination;
3. validate the provider map and requested mutation;
4. modify only `Settings.Providers`;
5. preserve every unrelated custom setting semantically;
6. serialize deterministically;
7. write an owner-only temporary regular file in the destination directory;
8. flush and fsync the file;
9. atomically replace the custom settings file; and
10. fsync the parent directory where supported.

The mutation holds the existing settings/configuration lock discipline for the
whole read-modify-write transaction. It never writes the default settings layer,
environment override layer, or effective value returned by `SummonSettings()`.

Adding an identical profile is idempotent. A conflicting existing profile
requires `--replace`. Removing an unknown profile fails explicitly.

## URL policy

The Artifact HTTP provider constructor and profile validation enforce the same
URL invariant so a raw URL cannot bypass profile policy.

All provider URLs must:

- use `http` or `https`;
- contain a host;
- contain no URL userinfo, username, or password;
- contain no query parameters;
- contain no fragment;
- contain no non-root path; and
- canonicalize a trailing root slash consistently.

`http` is accepted only for explicit loopback hosts:

- `localhost`;
- IPv4 addresses in `127.0.0.0/8`; and
- IPv6 `::1`.

All other provider hosts require HTTPS. Hostname strings that merely end in or
contain `localhost` are not loopback. DNS resolution is not used to upgrade a
non-loopback hostname into an HTTP exception.

These rules apply to named profiles and direct URL compatibility inputs.

## HTTP authentication and redirect policy

Named profiles may select an `authorization-env`. Credential resolution is
lazy and occurs for each actual HTTP request. If the variable is absent or
empty when a provider operation is required, the operation fails with an error
that names the variable but does not expose a value.

The runtime value is installed as the request's complete `Authorization`
header immediately before transport. It is never stored on a reusable request
template or copied into result metadata.

Artifact Provider HTTP clients do not follow redirects in v1. Any 3xx response
is returned as an explicit provider error. This applies whether or not auth is
configured and therefore prevents both credential forwarding and semantic
drift across origins. No existing Environment Artifact compatibility contract
requires redirects.

The HTTP client uses RCC's configured transport so existing TLS, certificate,
proxy, and endpoint controls remain effective. The auth wrapper does not mutate
the shared transport.

`rcc cache serve` remains loopback-only and does not gain production server
authentication in this slice.

## Lazy warm-path invariant

Provider-reference resolution returns a deferred Provider implementation. It
captures only the reference and a resolver function. It does not read provider
settings, resolve credentials, negotiate capabilities, open sockets, or touch
the network until one of the `artifactprovider.Provider` methods is called.

The acquisition order remains:

```text
validate canonical Artifact digest
  -> verify local Manifest/cache state
  -> verify local-ready materialization
  -> return local-materialization
  -> only on local miss: invoke deferred Provider
```

A verified local-ready Artifact must succeed even when:

- the named profile is missing, malformed, or unavailable;
- the profile's authorization environment variable is absent;
- the endpoint is unreachable;
- capability discovery would fail; or
- the deferred Provider would otherwise return an error.

Publish necessarily invokes the Provider. Cold acquire invokes it only after a
verified local miss. Provider inspect may read profile configuration and report
credential availability; provider test performs an actual capability request.

## Capability negotiation

Cold remote acquisition performs capability discovery before manifest
resolution and requires:

```text
schemaVersions contains 1
digestAlgorithms contains sha256
encodings contains gzip
```

Failure is explicit and identifies the unsupported requirement without falling
back to a build or package network. Publish retains the same requirements.

The compatibility predicate is owned once and reused by publish, acquire, and
`provider test`. It does not change the `Provider` interface.

## Diagnostics and JSON contracts

### List

JSON shape:

```json
{
  "providers": [
    {"name":"local","type":"filesystem","source":"builtin"},
    {"name":"office","type":"http","source":"settings","url":"https://cache.example"}
  ]
}
```

Entries are sorted by canonical name, with `local` included exactly once.

### Inspect

Inspection is offline. It reports:

```json
{
  "reference":"office",
  "source":"settings",
  "type":"http",
  "url":"https://cache.example",
  "authorization":{"source":"environment","variable":"RCC_PROVIDER_OFFICE_AUTHORIZATION","present":true},
  "localCache":{"root":".../artifacts/v1/content","state":"ready"}
}
```

For `local`, it reports the distinct provider root. For a direct URL, source is
`url` and no stored profile is implied. Inspection may report the authorization
environment-variable name and whether it is present, but never its value.

Local cache state is derived without trusting or exposing manifest content. It
describes path/readiness only; it does not claim every cached object has been
reverified by inspection.

### Test

Provider test performs capability discovery and returns:

```json
{
  "reference":"office",
  "reachable":true,
  "compatible":true,
  "capabilities":{"schemaVersions":[1],"digestAlgorithms":["sha256"],"encodings":["gzip"]}
}
```

Transport, credential, redirect, and capability incompatibility errors are
explicit, exit nonzero, remain on stderr, and emit no misleading success JSON.

JSON ordering is deterministic through typed result structures. Secret values
are excluded by type, not redacted after serialization.

## Package ownership

### `settings`

Owns the provider profile schema, canonical name and profile validation,
effective-layer merge, custom-layer-only mutation, deterministic serialization,
and atomic settings replacement.

### `artifactprovider`

Retains the Provider interface and owns strict HTTP URL validation, redirect
policy, auth transport wrapper, local filesystem provider construction, and the
reusable v1 capability predicate.

### `cmd`

Owns provider reference parsing, deferred Provider construction, the thin
`provider` command group, human/JSON rendering, and translation into existing
lifecycle requests. It contains no Manifest, materialization, or lease logic.

### `environmentlifecycle`

Retains publish/acquire/materialize behavior and adds only cold-path capability
validation through the shared predicate. Warm-path ordering remains unchanged.

## Failure behavior

- Invalid names, profiles, URLs, and auth-variable names fail before mutation.
- No failed settings mutation leaves a partial custom settings file visible.
- Missing credentials fail only when an HTTP request is required.
- Redirects fail rather than being followed.
- Capability mismatch fails before manifest or object transfer.
- Corrupt provider or local cache content continues to fail closed.
- A provider failure never triggers a package build during acquire.
- A provider failure never prevents a verified local-ready acquire.
- Provider management never edits legacy remote configuration or Holotree state.

## TDD and acceptance requirements

Focused RED/GREEN tests must prove:

1. canonical profile-name acceptance and rejection, including reserved `local`
   and URL-ambiguous names;
2. deterministic parsing, merge order, listing, and serialization;
3. add/idempotent add/conflicting add with replace/remove behavior;
4. custom-settings-only mutation with unrelated custom settings preserved;
5. no-follow, regular-file, owner-only, fsync, and atomic replacement behavior;
6. `authorization-env` persists only the variable name;
7. provider JSON and diagnostics contain no secret runtime value;
8. URL userinfo/username/password rejection;
9. URL query and fragment rejection;
10. non-loopback HTTP rejection and `localhost`, `127.0.0.0/8`, and `::1`
    acceptance;
11. redirects are not followed and Authorization cannot cross an origin;
12. direct URL and named-profile selection precedence;
13. explicit raw URLs cannot bypass the provider URL invariant;
14. built-in `local` uses its distinct deterministic provider root;
15. cold acquire capability negotiation failure is explicit and performs no
    manifest/object request;
16. warm acquire succeeds with a missing/malformed profile, absent auth
    variable, dead endpoint, and a Provider that fails immediately if touched;
17. provider test returns stable capabilities and incompatibility behavior;
18. existing Environment Artifact publish/acquire/exec JSON contracts remain
    stable;
19. legacy `rccremote` command, `/parts`, `/delta`, `RCC_REMOTE_ORIGIN`,
    `RCC_REMOTE_AUTHORIZATION`, and shared-Holotree tests remain green; and
20. bundle/export/import regressions remain green.

The bounded black-box Artifact Robot must use a named HTTP profile for the cold
publish/acquire path, stop the provider, then prove a fresh warm command with
the profile credential removed still succeeds locally. Existing exact-digest
A/B assertions remain.

Promotion gates are:

1. focused Go and settings/CLI tests;
2. focused race tests;
3. contained `artifactFocused`;
4. contained real A/B `artifactVertical`;
5. contained bounded `artifactRobot`;
6. contained `unitTests` and `local` build;
7. legacy `rccremote` and bundle/export/import compatibility tests;
8. full Robot regression;
9. two-generation `selfHost`;
10. binary inventory and Windows compile-only;
11. `go vet` documented baseline;
12. `git diff --check`; and
13. one independent GPT-5.6 Terra exact-SHA review.

## Deferred remote work

- keyring and command-based credential sources;
- auth challenges, refresh, and server authorization policy;
- redirects, mirrors, and multi-origin policies;
- tags or mutable Artifact references;
- provider GC and retention;
- resumable/range transfer;
- object-storage providers;
- shared server packages or an `rccremote` wrapper;
- legacy-to-v1 server translation;
- Action worker integration; and
- production deployment/management planes.

Any future redirect support must define origin equivalence and credential
forwarding explicitly. Any future `rccremote` refactor must first prove exact
binary, flag, endpoint, shared-Holotree, and release compatibility.

## Documentation receipt

- Canonical guidance: this specification defines provider-profile UX,
  credential references, URL/redirect safety, custom-settings mutation, local
  provider ownership, diagnostics, and the v18 `rccremote` classification.
- Durable learning: legacy `rccremote` and Environment Artifact HTTP providers
  have different identity and storage trust boundaries; configuration may be
  shared, protocol implementations should not be conflated in v18.
- Evidence: promoted baseline source, issue #118, current focused tests, release
  topology, and `rccremote-docker` operational source.
- Stale guidance removed: none in this design checkpoint.
- Remaining uncertainty: none for the bounded v1 profile slice; deferred
  production auth and server convergence require separate designs.
