# Embedded Environment Artifact trust for robot bundles

## Contract

`bundleTrustOptions` is the reusable policy boundary for source+Artifact bundles. Acquisition uses strict remote verification by default, deployment-supplied trust roots, and an explicitly selected deployment-owned carrier. A missing signature, invalid evidence, unknown signer, or revoked signer must fail closed before task execution. `--permissive-local` is an explicit migration escape hatch for controlled unsigned local artifacts; it is not the strict policy.

The acquired verification receipt and the policy, request, and carrier are copied into the materialization. `environmentlifecycle.ExecuteInDirectory` creates a lease and refreshes trust evidence before starting the task. The execution handle carries that refreshed receipt. Callers must retain this acquisition-to-lease path; acquiring with one policy and leasing without its context bypasses the refresh boundary.

This contract does not authenticate policy-relevant detached metadata. Per #253, provenance and revocation attachment bytes remain unsigned/digest-bound, and the current policy has no authenticated monotonic rollback guard. Therefore a deployment must treat the carrier and its delivery as trusted input, and claims depending on provenance assertions or revocation freshness/rollback remain **NO-GO** until #253 is resolved. This change makes no source sandboxing claim and adds no trust envelope. Roots are never inferred from the bundle, provider, or embedded Artifact.

Source-only and v12 compatibility stays on the existing execution path. The helpers in this lane are intended only for the embedded Environment Artifact branch.

## Coordinator wiring after #256

Do not edit `cmd/robotRunFromBundle.go` in this lane. After #256 is complete, apply this minimal wiring there:

1. Add `var bundleTrust bundleTrustOptions` alongside the command's other flag-backed options, then call `bundleTrust.addFlags(robotRunFromBundleCmd)` in `init()`.
2. In the embedded Artifact branch, replace the bare `NewAcquirer().Acquire(...)` call with `bundleTrust.acquire(cmd.Context(), artifactManifest.ArtifactDigest)`.
3. Convert the acquired result with `materializationForBundledArtifact(acquired)` and pass it to the existing `environmentlifecycle.ExecuteInDirectory` call. Do not reconstruct a materialization that drops `Verification`, `TrustPolicy`, `TrustRequest`, or `TrustCarrier`.
4. Preserve the existing manifest digest, archive identity, and platform-index checks. Do not change source-only execution.

The coordinator must run `robot_tests/bundle_trust_cli_acceptance.py` against real source+Artifact bundles after wiring. Its required JSON cases cover signed allow, unsigned strict deny, revoked, tampered, unknown signer, missing/invalid evidence, explicit permissive legacy allow, and lease-time revocation refresh deny. Every deny case must configure a task marker and an absolute escaped-artifact path; the runner rejects the case if either appears. The refresh-denial case uses `carrier_refresh.before` and `.after` directories: it serves the first `revocations.json` response from `before`, then all later requests from `after`, forcing the second trust read to see the new revocation. A test that does not execute the final wired CLI is not runtime acceptance.

Example case shape:

```json
[
  {
    "name": "unsigned-strict-deny",
    "bundle": "/path/to/unsigned-source-artifact.py",
    "task": "proof",
    "expect": "deny",
    "marker": "task-ran.txt",
    "escaped_artifact": "/tmp/rcc-bundle-trust-fixtures/unsigned/escaped.txt",
    "trust_args": ["--trust-roots", "/path/to/roots.json", "--trust-carrier", "/path/to/carrier", "--trust-carrier-type", "filesystem"]
  }
]
```

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
