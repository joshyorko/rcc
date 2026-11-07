---
name: verify-execution-lifecycle
description: Check changes to RCC artifact trust admission before materialization, lease creation, and process start.
---

# Verify execution admission

Use this skill when changing artifact trust-policy selection, detached evidence, acquisition/materialization, lease admission, or a caller that starts a process from an acquired artifact. Read every model result with the pstack `/statechart` skill.

## Run

Prepare generated assets using `rcc-development`, then run
`go test ./cmd -run '^TestBundleTrustDefaultsToStrictAndCarriesLeaseRefreshContext$' -count=1`.
This Go regression detects a caller-side policy downgrade or loss of lease
refresh context. It does not establish real bundle execution on its own. Also
run `TestBundleTrustRealCLI` with the built executable and a real native archive
as described in `docs/bundle-artifact-trust.md`; a skipped test is not acceptance.

From the repository root, run `python3 docs/formal/execution-lifecycle/check.py`.
It requires the pinned TLC 1.7.4 and Java selected by mise (or explicit
`TLA2TOOLS` and `JAVA` paths), validates the TLC checksum, and checks fixed,
deliberately broken, and restored configurations with a bounded JVM and timeout.
If the tools are absent, report FORMAL BLOCKED; follow `/statechart` to provision
and inspect the checker. No installed statechart command is embedded here.

Run `/statechart` on `docs/formal/execution-lifecycle/ExecutionLifecycle.tla` with its fixed configuration and view. Confirm the bounded model passes both invariants and review the generated state chart. The model has two finite inputs: a deployment carrier with a valid signature and authenticated metadata, and an attacker carrier with an unsigned artifact and unsigned metadata. It does not enumerate arbitrary bytes, keys, revocation history, or platforms.

For the falsifiability check, run `/statechart` with `ExecutionLifecycle-broken.cfg` and the same model and view. It must report the `NoTrustDowngrade` counterexample in which unsigned metadata changes a selected strict policy to permissive. This configuration is intentionally broken; do not use it as acceptance evidence.

## Properties

- **NoTrustDowngrade:** Once strict policy is selected, untrusted metadata cannot turn it into permissive policy. A downgrade could bypass the deployment's signature requirement.
- **NoUntrustedExecution:** A process can be permitted only after strict verification of a valid signature and authenticated metadata, or after an explicit permissive-local opt-in, and only with a materialization lease. Otherwise attacker-controlled bytes could reach execution.

The model treats the deployment carrier and its metadata-authentication decision as trusted inputs. Issue #253 identifies that current detached provenance and revocation attachment bytes are not cryptographically authenticated or protected against rollback; those source facts are an explicit unresolved assumption here, not proven by this model. The model does not claim provider-only attacks can forge signature verification.

Evidence and Artifact identity remain fixed between acquisition and lease in
this model. Lease-time refresh against changed revocations requires real-code
tests. These are safety checks, not liveness or eventual-completion proofs.

## Source coverage and synchronization

Coverage last checked against RCC source commit `f7960076843500d15835b476c07e60d7264027c1`:

- `environmentlifecycle/materialize.go`: `Acquirer.acquireLocked` selects trust policy verification before it returns acquired content; a nil policy currently produces a valid receipt without detached verification. See issue #252.
- `environmentlifecycle/lease.go`: `LocalMaterializer.Lease` refreshes trust and rejects an invalid receipt before publishing a lease.
- `environmentlifecycle/execute.go`: `executeWithSignalsAndStreamsInDirectory` obtains the lease and execution handle before `process.Start`.
- `cmd/robotRunFromBundle.go`: `robot run-from-bundle` currently calls `Acquire` with no trust policy. This is the specific caller boundary in #252.

The model is an abstract admission contract, not a proof of these Go functions. Keep this source-sync commit at the last commit whose functions you checked. After any covered source change, inspect the new code, update this mapping/model if needed, rerun the fixed and broken configurations, then advance the sync commit. Issue #253's unsigned metadata and revocation rollback exposure remains open. Output copy-back (#256), child supervision/lease cleanup, and environment-relative executable resolution (#258) remain implementation and runtime-test gates outside this formal model.
