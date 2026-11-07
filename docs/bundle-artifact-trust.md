# Embedded Environment Artifact trust for robot bundles

## Contract

`bundleTrustOptions` is the reusable policy boundary for source+Artifact bundles. Acquisition uses strict remote verification by default, deployment-supplied trust roots, and an explicitly selected deployment-owned carrier. A missing signature, invalid evidence, unknown signer, or revoked signer must fail closed before task execution. `--permissive-local` is an explicit migration escape hatch for controlled unsigned local artifacts; it is not the strict policy.

The acquired verification receipt and the policy, request, and carrier are copied into the materialization. `environmentlifecycle.ExecuteInDirectory` creates a lease and refreshes trust evidence before starting the task. The execution handle carries that refreshed receipt. Callers must retain this acquisition-to-lease path; acquiring with one policy and leasing without its context bypasses the refresh boundary.

This contract does not authenticate policy-relevant detached metadata. Per #253, provenance and revocation attachment bytes remain unsigned/digest-bound, and the current policy has no authenticated monotonic rollback guard. Therefore a deployment must treat the carrier and its delivery as trusted input, and claims depending on provenance assertions or revocation freshness/rollback remain **NO-GO** until #253 is resolved. This change makes no source sandboxing claim and adds no trust envelope. Roots are never inferred from the bundle, provider, or embedded Artifact.

Source-only and v12 compatibility stays on the existing execution path. The helpers in this lane are intended only for the embedded Environment Artifact branch.

Deployment invocations requiring signed environments must explicitly select
`--strict-remote`. With that flag, source-only bundles must be rejected before
task execution, so removing the embedded Artifact cannot downgrade the selected
contract. Without an explicit strict requirement, legacy source-only execution
retains its existing behavior. Artifact bundles still default to strict even
when no policy flag is supplied.

## Coordinator wiring after #256

Do not edit `cmd/robotRunFromBundle.go` in this lane. After #256 is complete, apply this minimal wiring there:

1. Add `var bundleTrust bundleTrustOptions` alongside the command's other flag-backed options, then call `bundleTrust.addFlags(robotRunFromBundleCmd)` in `init()`.
2. In the embedded Artifact branch, replace the bare `NewAcquirer().Acquire(...)` call with `bundleTrust.acquire(cmd.Context(), artifactManifest.ArtifactDigest)`.
3. Convert the acquired result with `materializationForBundledArtifact(acquired)` and pass it to the existing `environmentlifecycle.ExecuteInDirectory` call. Do not reconstruct a materialization that drops `Verification`, `TrustPolicy`, `TrustRequest`, or `TrustCarrier`.
4. Preserve the existing manifest digest, archive identity, and platform-index checks. Do not change source-only execution.

After wiring, build RCC and export a real native environment archive. Run the
opt-in `TestBundleTrustRealCLI` with both inputs explicitly selected:

```sh
RCC_ACCEPTANCE_BINARY="$RCC_BINARY" RCC_BUNDLE_TEST_ARCHIVE="$RCC_ARCHIVE" \
  go test ./cmd -run '^TestBundleTrustRealCLI$' -count=1 -v
```

`RCC_BINARY` must be an absolute built executable and `RCC_ARCHIVE` a real
exported `.rcca` for the native platform. Each case constructs a source+Artifact
bundle, ephemeral deployment signing key and private RCC home. It checks signed
allow, strict unsigned denial, revocation, signature tampering, unknown signer,
missing/invalid evidence, explicit unsigned migration, and lease-time revocation.
An external task sentinel detects execution independently of copy-back; denied
cases also require no caller artifacts or child streams. Successful cases check
file bytes, stream separation, acquisition/lease receipts and released leases.
The loopback carrier serves a valid snapshot for acquisition and a newly revoked
snapshot for lease creation. A plain model or mocked acquirer cannot satisfy
this gate. `RCC_TRUST_BASELINE=1` instead requires the real pre-fix CLI to execute
the unsigned bundle without policy flags, proving the original bypass.

## Baseline reproduction

Environment: Linux x86_64 (`Linux 7.2.11 x86_64`), Go `go1.26.10 linux/amd64`, source at `f7960076843500d15835b476c07e60d7264027c1`, installed CLI `v18.19.8`.

| Executable | Binary SHA-256 | Command | Exit | stdout SHA-256 | stderr SHA-256 and diagnostic |
| --- | --- | --- | ---: | --- | --- |
| Source-built unchanged main | `bb623bb07bdbca384bdf407fb72aa3bf2f2f007597372ce5d337585bce5ba283` | `build/rcc robot run-from-bundle /does-not-exist.rcca --trust-roots /does-not-exist.json` | `current-main-trust-roots.exit` = 1 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` (empty) | `27713fbb92477577ce60ed32a5fe9e117d2eab0837476976626facbc40c2140f`; `Error: unknown flag: --trust-roots`; `Error: [rcc v18.19.8] unknown flag: --trust-roots`; usage printed |
| Installed published v18.19.8 | `ea8dc96e66d7a81536b03a344b4eaf44bbd94122585954e70290bd466f5de4b6` | `rcc robot run-from-bundle /does-not-exist.rcca --trust-roots /does-not-exist.json` | `installed-v18.19.8-trust-roots.exit` = 1 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` (empty) | `27713fbb92477577ce60ed32a5fe9e117d2eab0837476976626facbc40c2140f`; same diagnostic and usage as source-built main |

The attempted policy option is rejected by both real CLIs before bundle open; no output or task artifact was created. Captures were `current-main-trust-roots.{stdout,stderr,exit}` and `installed-v18.19.8-trust-roots.{stdout,stderr,exit}` under `/tmp/codex-rcc-bundletrust-20261106-EHu5un/baseline/`. The directory is temporary and is removed at handoff. This reproduces the missing policy-control surface, **not** the unsigned-artifact execution path. No valid unsigned `.rcca` fixture was available at baseline, so runtime bypass reproduction remains pending central acceptance.

## Verification receipt

- Focused helper tests: `go test ./cmd -run 'TestBundleTrust|TestBundleTrustFlags' -count=1` — passed.
- Package tests: `GOARCH=amd64 CGO_ENABLED=0 go test ./cmd/... -count=1` — passed.
- Broad Go tests: `GOARCH=amd64 CGO_ENABLED=0 go test ./...` — passed.
- CLI runner syntax/help: `PYTHONPYCACHEPREFIX=<task-scratch>/pycache python3 -m py_compile robot_tests/bundle_trust_cli_acceptance.py` and `python3 robot_tests/bundle_trust_cli_acceptance.py --help` — passed.
- Real bundle trust CLI acceptance: pending #256 wiring and real trust fixtures; not claimed passing.
- #253 authenticated detached metadata and rollback guarantees: unresolved, NO-GO for affected policy claims.
- Source build: `GOARCH=amd64 CGO_ENABLED=0 go build -ldflags '-s' -o build/rcc ./cmd/rcc` — passed; output version `v18.19.8`, SHA-256 `ab3e7275b416f0baf53bf7a692967ec03ef72a63e293dbfb35663cb792357e34`.
- Files generated by `inv assets` are ignored build inputs; no generated blob is hand-edited.
