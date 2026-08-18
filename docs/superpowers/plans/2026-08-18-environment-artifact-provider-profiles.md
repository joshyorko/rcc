# Environment Artifact Provider Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add deterministic, secret-safe named Environment Artifact provider profiles and diagnostics while preserving provider-independent identity, verified warm operation, and the unchanged v18 `rccremote` compatibility line.

**Architecture:** Extend the custom `settings.yaml` layer with provider references, resolve those references lazily into the existing `artifactprovider.Provider` interface, and harden the Artifact HTTP client boundary for URL, authentication, redirect, and capability policy. Keep the local provider store, verified local cache, lifecycle, and legacy remote protocol as separate trust boundaries.

**Tech Stack:** Go 1.26.5, Cobra, `gopkg.in/yaml.v2`, RCC settings/pathlib primitives, `net/http`, existing Environment Artifact packages, Robot Framework, and the contained `developer/toolkit.yaml`.

## Global Constraints

- Base implementation on `30ce09f0a63fdf70fdda9533d26ea6467ca2e5bb` and the approved specification at `docs/superpowers/specs/2026-08-18-environment-artifact-provider-profiles-design.md`.
- Do not change Manifest v1, Object Index v1, v12 catalog identity, portable materialization, leases, execution handles, or the `artifactprovider.Provider` method set.
- `authorization-env` stores only an environment-variable name; its runtime value is the complete HTTP `Authorization` header.
- Artifact HTTP URLs reject userinfo, query parameters, fragments, non-root paths, and non-loopback HTTP.
- Artifact HTTP clients do not follow redirects in v1.
- A local-ready warm acquisition performs no profile load, credential lookup, capability negotiation, provider call, or network request.
- Provider mutation reads and writes only the custom `settings.yaml` layer and preserves unrelated custom settings.
- `local` is reserved and maps to `$ROBOCORP_HOME/artifacts/v1/provider`, distinct from `artifacts/v1/content`.
- Keep `rccremote`, `/parts`, `/delta`, `/force`, `RCC_REMOTE_ORIGIN`, `RCC_REMOTE_AUTHORIZATION`, shared-Holotree behavior, and all eight release binaries unchanged.
- Use the checked-in RCC toolkit as the primary verification path. Direct Go commands are only the tight RED/GREEN loop.
- Every behavioral repair starts with a focused failing regression test.
- Do not begin Action Server, FUSE, OCI, object storage, zstd, packfiles, provider GC, production auth, tags, redirects, or TUI work.

---

## Planned file ownership

- `artifactprovider/http_policy.go`: canonical provider URL, auth transport, no-redirect client policy.
- `artifactprovider/deferred.go`: lazy one-time Provider resolution without changing the Provider interface.
- `artifactprovider/provider.go`: shared Environment Artifact v1 capability predicate.
- `settings/provider.go`: provider profile schema, canonical names, validation, and map operations.
- `settings/provider_store.go`: custom-layer-only locked read/modify/write transaction.
- `settings/provider_store_unix.go`, `settings/provider_store_other.go`: parent-directory sync and platform-specific atomic publication support.
- `cmd/provider.go`: provider command group registration.
- `cmd/providerResolve.go`: reference classification, lazy Provider construction, local roots, and redacted inspection.
- `cmd/providerAdd.go`, `providerList.go`, `providerInspect.go`, `providerTest.go`, `providerRemove.go`: thin command translations and typed output.
- Existing lifecycle and environment command files change only where the plan explicitly names them.

### Task 1: Harden the Artifact HTTP and capability boundary

**Files:**
- Create: `artifactprovider/http_policy.go`
- Create: `artifactprovider/deferred.go`
- Create: `artifactprovider/http_policy_test.go`
- Create: `artifactprovider/deferred_test.go`
- Modify: `artifactprovider/http.go`
- Modify: `artifactprovider/provider.go`
- Modify: `artifactprovider/http_test.go`
- Modify: `environmentlifecycle/publish.go`
- Test: `artifactprovider/http_policy_test.go`
- Test: `artifactprovider/deferred_test.go`

**Interfaces:**
- Produces: `NormalizeHTTPURL(raw string) (string, error)`
- Produces: `HTTPOptions{Client *http.Client, AuthorizationEnv string}`
- Produces: `NewHTTPWithOptions(raw string, options HTTPOptions) (*HTTP, error)`
- Preserves: `NewHTTP(raw string, client *http.Client) (*HTTP, error)`
- Produces: `ValidateV1Capabilities(Capabilities) error`
- Produces: `NewDeferred(resolve func() (Provider, error)) Provider`

- [ ] **Step 1: Write failing URL-policy tests**

Add table-driven cases asserting that canonical HTTPS, `http://localhost`,
`http://127.42.0.1`, and `http://[::1]` pass, while these fail:

```go
[]string{
    "https://user@example.test",
    "https://user:secret@example.test",
    "https://example.test?x=1",
    "https://example.test#fragment",
    "https://example.test/v1",
    "http://example.test",
    "http://localhost.example.test",
}
```

Also assert the accepted return value has no trailing root slash.

- [ ] **Step 2: Run the URL tests and verify RED**

Run:

```sh
go test -count=1 ./artifactprovider -run 'TestNormalizeHTTPURL'
```

Expected: compile failure because `NormalizeHTTPURL` does not exist.

- [ ] **Step 3: Implement canonical URL validation**

Implement `NormalizeHTTPURL` with `net/url`, explicit `parsed.User == nil`,
empty query/fragment, root-only path, and literal loopback checks. Do not use DNS
resolution. Replace the constructor's private validation with this function so
raw URLs cannot bypass it.

- [ ] **Step 4: Write failing redirect and authorization tests**

Use two `httptest.Server` instances. The first returns a redirect to the
second. Set the named environment variable to `Bearer test-secret-value`.
Assert:

```go
if redirected.Load() {
    t.Fatal("artifact client followed redirect")
}
if strings.Contains(resultError.Error(), "test-secret-value") {
    t.Fatal("credential leaked in error")
}
```

Add a direct request test proving the server receives the complete header value,
and an absent-variable test proving the error names only the variable.

- [ ] **Step 5: Run the HTTP-policy tests and verify RED**

Run:

```sh
go test -count=1 ./artifactprovider -run 'TestHTTP(Authorization|RejectsRedirect)'
```

Expected: failure because options/auth/no-redirect policy is absent.

- [ ] **Step 6: Implement HTTP options, lazy auth, and no redirects**

Clone the supplied `http.Client` value, wrap its transport without mutating the
original, and set:

```go
client.CheckRedirect = func(*http.Request, []*http.Request) error {
    return http.ErrUseLastResponse
}
```

The auth RoundTripper reads `os.LookupEnv(options.AuthorizationEnv)` only from
`RoundTrip`, clones the request, and sets the complete `Authorization`
value. Keep `NewHTTP` as a compatibility wrapper around
`NewHTTPWithOptions`.

- [ ] **Step 7: Write and implement deferred-provider tests**

Use a resolver counter and an inert Provider. Constructing the deferred
Provider must leave the counter at zero. The first Provider method resolves
once; concurrent Provider methods resolve at most once; a resolver error is
stable and contains no secret.

Implement `deferredProvider` with `sync.Once` and forwarding methods for all
six existing Provider operations.

- [ ] **Step 8: Write and implement the capability predicate**

Test exact success for v1/SHA-256/gzip and explicit failures for each missing
capability. Add:

```go
func ValidateV1Capabilities(capabilities Capabilities) error
```

Replace the private capability condition in `environmentlifecycle.Publish`
with this shared predicate.

- [ ] **Step 9: Run focused tests and commit**

Run:

```sh
go test -count=1 ./artifactprovider ./environmentlifecycle
go test -race -count=1 ./artifactprovider
git diff --check
```

Commit:

```sh
git add artifactprovider environmentlifecycle/publish.go
git commit -m "feat(provider): harden artifact HTTP policy"
```

### Task 2: Add deterministic custom-layer provider profiles

**Files:**
- Create: `settings/provider.go`
- Create: `settings/provider_test.go`
- Create: `settings/provider_store.go`
- Create: `settings/provider_store_unix.go`
- Create: `settings/provider_store_other.go`
- Create: `settings/provider_store_test.go`
- Modify: `settings/data.go`
- Modify: `settings/settings.go`

**Interfaces:**
- Consumes: `artifactprovider.NormalizeHTTPURL`
- Produces: `ProviderProfile{Type, URL, AuthorizationEnv string}`
- Produces: `ProviderProfiles map[string]ProviderProfile`
- Produces: `ValidateProviderName(name string) error`
- Produces: `(ProviderProfile).Validate() (ProviderProfile, error)`
- Produces: `LoadCustomSettingsForMutation() (*Settings, error)`
- Produces: `UpdateCustomProvider(name string, profile *ProviderProfile, replace bool) error`

- [ ] **Step 1: Write failing profile schema and name tests**

Cover the exact name expression `^[a-z0-9][a-z0-9._-]{0,62}$`, rejection of
`local`, uppercase, whitespace, `:`, `/`, `?`, `#`, and `@`.
Validate `type: http`, the shared URL policy, and environment-variable names
matching `^[A-Za-z_][A-Za-z0-9_]*$`.

Assert marshaling stores:

```yaml
authorization-env: RCC_PROVIDER_OFFICE_AUTHORIZATION
```

and never the environment's runtime value.

- [ ] **Step 2: Run schema tests and verify RED**

Run:

```sh
go test -count=1 ./settings -run 'TestProvider(Profile|Name)'
```

Expected: compile failure for missing profile types.

- [ ] **Step 3: Implement schema, merge, and deterministic ordering**

Add `Providers ProviderProfiles` to `Settings`, initialize it in effective
settings, and merge profile names layer-by-layer. Validation returns a
normalized URL copy without mutating caller input. Expose a sorted-name helper
used by commands rather than relying on map iteration.

- [ ] **Step 4: Write failing custom-layer mutation tests**

Force `common.Product` to an isolated test home. Seed custom
`settings.yaml` with unrelated endpoint, certificate, option, and metadata
values plus a provider. Test:

- add preserves every unrelated decoded custom value;
- identical add is idempotent;
- conflicting add fails without `replace`;
- replace changes only the named provider;
- remove changes only Providers;
- remove missing and mutation of `local` fail;
- the default/effective layer is never serialized into the custom file;
- a symlink destination and non-regular destination are rejected;
- final mode is owner-only; and
- interrupted/failed mutation leaves the original file readable.

- [ ] **Step 5: Run mutation tests and verify RED**

Run:

```sh
go test -count=1 ./settings -run 'TestCustomProvider'
```

Expected: compile failure for missing custom mutation API.

- [ ] **Step 6: Implement locked atomic custom-settings mutation**

Read only `common.SettingsFile()` into an empty custom Settings when absent.
Hold `common.SettingsFile()+".lck"` through the transaction. Use `Lstat`,
an owner-only temporary file in the same directory, deterministic YAML,
`Sync`, `Close`, atomic rename, and parent-directory sync where supported.
Never call or serialize `SummonSettings()`.

- [ ] **Step 7: Run focused and race tests and commit**

Run:

```sh
go test -count=1 ./settings
go test -race -count=1 ./settings
git diff --check
```

Commit:

```sh
git add settings
git commit -m "feat(settings): persist provider profiles safely"
```

### Task 3: Add lazy provider resolution and management commands

**Files:**
- Create: `cmd/provider.go`
- Create: `cmd/providerResolve.go`
- Create: `cmd/providerAdd.go`
- Create: `cmd/providerList.go`
- Create: `cmd/providerInspect.go`
- Create: `cmd/providerTest.go`
- Create: `cmd/providerRemove.go`
- Create: `cmd/provider_test.go`
- Modify: `cmd/root.go` only if command registration requires it
- Modify: `cmd/environment.go`

**Interfaces:**
- Consumes: settings provider profile and mutation APIs
- Consumes: `artifactprovider.NewDeferred`, `NewHTTPWithOptions`, and `ValidateV1Capabilities`
- Produces: `newProviderReference(reference string) (artifactprovider.Provider, error)`
- Produces: typed JSON results for add/list/inspect/test/remove
- Preserves: `environmentCommandDependencies.newProvider func(string) (Provider, error)`

- [ ] **Step 1: Write failing reference-resolution tests**

Assert construction of `newProviderReference("missing-profile")` performs no
settings read and succeeds with a deferred Provider. The first method call must
then report the missing profile. Test exact classification of `local`, direct
URLs, and names.

Assert `local` resolves to:

```go
filepath.Join(common.Product.Home(), "artifacts", "v1", "provider")
```

and not `artifacts/v1/content`.

- [ ] **Step 2: Run resolver tests and verify RED**

Run:

```sh
go test -count=1 ./cmd -run 'TestProviderReference'
```

Expected: compile failure for the missing resolver.

- [ ] **Step 3: Implement the deferred resolver**

Return `artifactprovider.NewDeferred(func() (Provider, error) { ... })`.
Inside the closure:

- construct the built-in filesystem Provider for `local`;
- construct raw URL Providers with the configured RCC HTTP transport; or
- load effective settings, validate the named profile, and construct
  `NewHTTPWithOptions` with its authorization environment-variable name.

Do not inspect an auth variable outside the RoundTripper.

- [ ] **Step 4: Write failing CLI contract and redaction tests**

Build the provider command with injected dependencies. Assert exact subcommands
and stable JSON. Seed an auth value containing a unique sentinel and prove the
sentinel is absent from stdout, stderr, persisted settings, inspect JSON, list
JSON, and errors.

Assert list ordering is `local` followed by canonical sorted names. Inspect
reports only the variable name and presence boolean. Test output includes exact
capabilities and fails explicitly for incompatible capabilities.

- [ ] **Step 5: Run command tests and verify RED**

Run:

```sh
go test -count=1 ./cmd -run 'TestProvider(Add|List|Inspect|Test|Remove|Redaction)'
```

Expected: failures because the command group is absent.

- [ ] **Step 6: Implement thin provider commands**

Use typed result structs with JSON field names from the specification. Human
output must not include secret values. `provider test` calls Capabilities and
`ValidateV1Capabilities`; inspect remains offline. Add uses
`UpdateCustomProvider`, and remove passes a nil profile mutation.

- [ ] **Step 7: Connect environment command construction**

Change only the default `newProvider` implementation in
`cmd/environment.go` to return the deferred reference. Preserve all injected
test seams and existing publish/acquire/exec result shapes.

- [ ] **Step 8: Run focused tests and commit**

Run:

```sh
go test -count=1 ./cmd/... ./settings ./artifactprovider
go test -race -count=1 ./cmd/... ./settings ./artifactprovider
git diff --check
```

Commit:

```sh
git add cmd settings artifactprovider
git commit -m "feat(cmd): add environment provider profiles"
```

### Task 4: Enforce cold capability negotiation and strong warm independence

**Files:**
- Modify: `environmentlifecycle/acquire.go`
- Modify: `environmentlifecycle/acquire_test.go`
- Modify: `environmentlifecycle/materialize_test.go`
- Modify: `cmd/environment_test.go`

**Interfaces:**
- Consumes: `artifactprovider.ValidateV1Capabilities`
- Preserves: `Acquirer.Acquire(context.Context, AcquireRequest) (AcquireResult, error)`

- [ ] **Step 1: Write the failing cold capability test**

Use a recording Provider whose Capabilities omit one requirement and whose
ResolveManifest/GetObject methods fail the test immediately. Start from an
empty isolated RCC home. Assert the returned error names the capability and no
manifest/object method was called.

- [ ] **Step 2: Run the cold test and verify RED**

Run:

```sh
go test -count=1 ./environmentlifecycle -run 'TestAcquireRejectsIncompatibleProviderBeforeResolve'
```

Expected: failure because acquire currently resolves the manifest first.

- [ ] **Step 3: Add capability negotiation only after local miss**

In `Acquirer.Acquire`, keep local manifest and warm materialization checks
first. Only after the verified local cache returns `os.ErrNotExist`, require a
Provider, call Capabilities, validate v1 compatibility, and continue remote
acquisition.

- [ ] **Step 4: Write strong warm-path regressions**

Prepare a real local-ready fixture and pass deferred references whose resolver:

- returns a missing-profile error;
- would require an absent authorization environment variable;
- targets an unreachable endpoint; and
- panics if any Provider method is called.

Assert every warm acquisition reports `local-materialization` and no resolver,
credential, capability, or network operation occurred.

At the command layer, pass `--provider malformed-or-missing` and prove the
lifecycle receives a deferred Provider without resolving it.

- [ ] **Step 5: Run focused and race tests and commit**

Run:

```sh
go test -count=1 ./environmentlifecycle ./cmd/...
go test -race -count=1 ./environmentlifecycle ./cmd/...
git diff --check
```

Commit:

```sh
git add environmentlifecycle cmd/environment_test.go
git commit -m "feat(provider): preserve lazy warm acquisition"
```

### Task 5: Add black-box profile acceptance and durable documentation

**Files:**
- Modify: `robot_tests/environment_artifacts.robot`
- Modify: `robot_tests/environment_artifacts/library.py` only if its command helper needs provider-profile arguments
- Modify: `docs/holotree.md`
- Modify: `README.md`
- Modify: `docs/changelog.md`
- Modify: `developer/README.md` only if a new focused task is required
- Modify: `tasks.py` only if the existing Artifact tasks do not already cover the new packages

**Interfaces:**
- Consumes: public `rcc provider` and existing `rcc env` commands
- Produces: black-box evidence that named profiles work cold and remain irrelevant warm

- [ ] **Step 1: Extend the bounded Robot with a named profile**

For process A and B homes, call:

```text
rcc provider add office --type http --url <loopback-provider-url>
    --authorization-env RCC_TEST_PROVIDER_AUTHORIZATION --json
```

Set the environment variable to a harmless complete header value. Publish and
cold acquire with `--provider office`. Preserve the exact immutable Artifact
digest assertions and distinct A/B home checks.

- [ ] **Step 2: Add the warm failure proof**

Stop the provider, remove `RCC_TEST_PROVIDER_AUTHORIZATION`, and run a fresh B
process using `--provider office`. Assert `local-materialization`, unchanged
Artifact digest, successful Python import, and no provider/builder operation.

- [ ] **Step 3: Run bounded acceptance**

Run through the installed RCC toolkit:

```sh
rcc run -r developer/toolkit.yaml --dev -t artifactFocused
rcc run -r developer/toolkit.yaml --dev -t artifactVertical
rcc run -r developer/toolkit.yaml --dev -t artifactRobot
```

Expected: all three tasks pass; the real vertical cannot skip.

- [ ] **Step 4: Document exact UX and compatibility**

Document:

- add/list/inspect/test/remove examples;
- the `authorization-env` complete-header contract;
- strict URL and redirect policy;
- built-in local provider root versus local cache;
- warm independence;
- direct URL compatibility;
- provider references versus Artifact identity; and
- rccremote classification A for v18.

Do not document deferred auth/keyring/server convergence as shipped behavior.

- [ ] **Step 5: Run documentation and legacy-focused checks and commit**

Run:

```sh
rcc run -r developer/toolkit.yaml --dev -t agentDocs
go test -count=1 ./cmd/rccremote ./remotree ./cmd -run 'Test.*(Bundle|Export|Import|Remote|Provider)'
git diff --check
```

Commit:

```sh
git add robot_tests docs README.md developer tasks.py
git commit -m "test(provider): prove named remote lifecycle"
```

### Task 6: Promote the exact candidate

**Files:**
- No planned product changes
- Evidence: `tmp/` logs and receipts, intentionally untracked

**Interfaces:**
- Produces: final exact SHA and promotion receipt

- [ ] **Step 1: Run contained focused and race gates**

```sh
rcc run -r developer/toolkit.yaml --dev -t artifactFocused
rcc run -r developer/toolkit.yaml --dev -t artifactRace
```

- [ ] **Step 2: Run contained build and vertical gates**

```sh
rcc run -r developer/toolkit.yaml --dev -t local
rcc run -r developer/toolkit.yaml --dev -t artifactVertical
rcc run -r developer/toolkit.yaml --dev -t artifactRobot
```

- [ ] **Step 3: Run compatibility and self-host gates**

```sh
rcc run -r developer/toolkit.yaml --dev -t unitTests
rcc run -r developer/toolkit.yaml -t robot
rcc run -r developer/toolkit.yaml --dev -t selfHost
rcc run -r developer/toolkit.yaml --dev -t binaryInventory
rcc run -r developer/toolkit.yaml --dev -t goVet
```

Confirm the full Robot suite has no new failure, the exact eight-binary inventory
still contains both products, and the self-host receipt records distinct homes.

- [ ] **Step 4: Run compile and repository hygiene checks**

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -run '^$' ./artifactprovider ./settings ./environmentlifecycle ./cmd/...
git diff --check
git status --short
```

- [ ] **Step 5: Commit any evidence-driven repair**

Only a reproduced gate failure may create a repair commit. Write a focused
failing regression first, implement the smallest contract-preserving repair,
and rerun the affected contained gates.

- [ ] **Step 6: Request one Terra exact-SHA review**

Review only:

- provider reference versus Artifact identity;
- secret persistence and JSON/log/error exposure;
- URL, loopback HTTP, and redirect enforcement;
- lazy warm independence;
- cold capability negotiation;
- custom-settings-only mutation;
- legacy rccremote/v12/bundle regression;
- Action-worker consumability without a central service; and
- excluded-scope compliance.

- [ ] **Step 7: Fix only a validated blocker**

If Terra reports a blocker, reproduce it, add a focused RED regression, repair
it, rerun affected gates, commit, and request disposition on the new exact SHA.
Do not add opportunistic improvements.

- [ ] **Step 8: Push and read back**

```sh
git push origin feature/environment-artifacts-v1
git ls-remote --heads origin refs/heads/feature/environment-artifacts-v1
```

The returned remote SHA must equal `git rev-parse HEAD`.

- [ ] **Step 9: Return the final receipt**

Report exact SHA, rccremote classification A, implemented CLI/JSON/auth/local
contracts, focused/race/Robot/self-host/binary evidence, deferred work, Terra
disposition, and the repository documentation receipt. Do not release, tag,
version-bump, or begin the next architectural phase.

