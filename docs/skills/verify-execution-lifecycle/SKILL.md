---
name: verify-execution-lifecycle
description: Check changes to RCC artifact trust admission before materialization, lease creation, and process start.
---

# Verify execution admission

Use this skill when changing artifact trust-policy selection, detached evidence, acquisition/materialization, lease admission, or a caller that starts a process from an acquired artifact. Read every model result with the pstack `/statechart` skill.

## Feature map, launch and doctor

Read [features/README.md](features/README.md). This is a short-lived CLI/model
workflow, not a server session. Before each drive, check `go version`, generated
assets via `rcc-development`, and pinned TLC/Java availability. After a failed
drive inspect its complete log and restore a known fixture state before retrying.
Go tests own isolated temporary homes and cleanup; never target the user cache.
Keep evidence outside disposable test homes and clean task-owned scratch after
retaining the intended reports. No process should outlive a useful drive.

The shared subsystem model runner is
`python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py MODEL.tla --out /absolute/new-task-directory`. It checks
pinned TLC bytes and expects fixed-pass, broken-invariant-failure, restored-pass,
retaining logs and a JSON receipt. New subsystem models live with their skills;
the original models remain at their established paths. Read each with `$statechart`.
Model fault configurations demonstrate sensitivity, not source-code mutation or
current product failures. Never replay a deliberate model fault as a product bug.

Authenticated metadata is owned by the
[metadata skill](../verify-authenticated-metadata/SKILL.md); the commands below
remain available for compatibility with the original combined verification gate.

The [chart gallery and evidence](evidence/README.md) describe retained results and
regeneration, including the Pele renderer owned by this skill.

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

For authenticated metadata or rollback-state changes, also run
`python3 docs/formal/execution-lifecycle/check.py --model AuthenticatedMetadata`
and read it with `$pstack:statechart`, using `metadata-view.mjs`. Run the
corresponding Go tests (`go test ./artifacttrust ./environmentlifecycle -run
'^TestMetadata|^TestStrictPolicyCommitsAuthenticatedRevocationBeforeDenying'`)
and `TestMetadataSecurityRealCLI` with `RCC_ACCEPTANCE_BINARY` set to the built
candidate. Its optional N-1 subtest requires `RCC_N_MINUS_ONE_BINARY`; report a
skip honestly. The CLI test retains commands and public receipts when
`RCC_METADATA_EVIDENCE` names a task-owned directory.

To falsify the runtime checks in an exclusively owned checkout, stop concurrent
builds and run `python3 docs/formal/execution-lifecycle/falsify_metadata.py
--evidence /absolute/new/task-evidence-directory`. It removes one actual guard
at a time, requires the matching test to fail, restores the original file bytes,
and reruns the restored tests. It refuses a reused evidence directory or a
changed guard. Never run this mutation exercise concurrently with other work.

Run `/statechart` on `docs/formal/execution-lifecycle/ExecutionLifecycle.tla` with its fixed configuration and view. Confirm the bounded model passes both invariants and review the generated state chart. The model has two finite inputs: a deployment carrier with a valid signature and authenticated metadata, and an attacker carrier with an unsigned artifact and unsigned metadata. It does not enumerate arbitrary bytes, keys, revocation history, or platforms.

For the falsifiability check, run `/statechart` with `ExecutionLifecycle-broken.cfg` and the same model and view. It must report policy loss at the caller boundary (`NoTrustDowngrade`, two steps), and unchecked process admission after materialization and leasing (`NoUntrustedExecution`, five steps). This configuration is intentionally broken; do not use it as acceptance evidence.

## Properties

- **NoTrustDowngrade:** Once strict policy is selected, a caller must not lose it and silently select unchecked/local policy. A downgrade could bypass the deployment's signature requirement.
- **NoUntrustedExecution:** A process can be permitted only after strict verification of a valid signature and authenticated metadata, or after an explicit permissive-local opt-in, and only with a materialization lease. Otherwise attacker-controlled bytes could reach execution.

The original model treats metadata authentication as a trusted input. The
`AuthenticatedMetadata` model separately checks authenticated admission and
retention across restarts under a carrier that can substitute/replay snapshots.
It has two sequences, empty/revoked authenticated snapshots and a forged one.
Fixed and restored configurations must pass; removing authentication must break
`NoUntrustedExecution`, and losing state on restart must break `NoTrustDowngrade`.
Independent deployment roots/floors, an unforgeable signature predicate and a
trusted clock/filesystem are assumptions. These checks do not prove Ed25519,
parsing, Go locking, filesystem power-loss behavior or global freshness under
withholding. Read the exact bounds and runtime bindings in
`docs/formal/execution-lifecycle/evidence/metadata-v2/README.md`.

Evidence and Artifact identity remain fixed between acquisition and lease in
this model. Lease-time refresh against changed revocations requires real-code
tests. These are safety checks, not liveness or eventual-completion proofs.

## Source coverage and synchronization

Coverage last checked against combined RCC source commit `96151d5406c3c543fd66ea8849d0a5bf2e045574`:

- `environmentlifecycle/materialize.go`: `Acquirer.acquireLocked` selects trust policy verification before it returns acquired content; a nil policy currently produces a valid receipt without detached verification. See issue #252.
- `environmentlifecycle/lease.go`: `LocalMaterializer.Lease` refreshes trust and rejects an invalid receipt before publishing a lease.
- `environmentlifecycle/execute.go`: `executeWithSignalsAndStreamsInDirectory` obtains the lease and execution handle before `process.Start`.
- `cmd/robotRunFromBundle.go`: `robotRunFromBundleCmd` now calls the trust helper, preserves its lease inputs and refuses source-only bundles when strict mode is explicitly required.
- `cmd/robotBundleTrust.go`: `bundleTrustOptions.acquireWith` selects policy and deployment-owned keys/carrier; `materializationForBundledArtifact` preserves that context for lease refresh. The low-level nil-policy acquisition API still exists for controlled legacy callers, so caller context matters.

The injected fault models loss of the caller's selected policy (including the
legacy nil-policy bypass) as unchecked/permissive admission. It does not claim
that detached metadata can rewrite Go policy fields. Downstream materialization
and lease creation trust the selected policy, as Go does; they do not independently
re-read the operator's opt-in flag. The external flag remains in the model only
to check the invariant.

Metadata source coverage is synchronized to commit
`6e7c84052e5ef09ee9aec47d22f639dfda76bf20`: `artifacttrust/metadata.go:VerifyMetadata`,
`artifacttrust/metadata_state.go:MetadataState.Admit`,
`artifacttrust/trust.go:Policy.Verify`, and
`environmentlifecycle/trust.go:refreshMaterializationTrust`. The lock-protected
comparison/flush is one abstract transition. `highestCommitted` is a model
history variable, not a second implementation store. Key rotation, same-sequence
equivocation, process concurrency and partial-journal failures require the Go
and CLI regressions in addition to the model.

The models are abstract admission contracts, not proofs of these Go functions.
Keep source-sync commits at the last revisions whose functions were checked.
After a covered change, inspect it, update the mapping/model if needed, rerun
fixed/broken/restored checks and runtime regressions, then advance the record.
Output copy-back, child supervision/lease cleanup and executable resolution
are outside these original admission models. The [bundle flow](../verify-bundle-flow/SKILL.md)
and [process execution](../verify-process-execution/SKILL.md) skills add separate
bounded contracts; executable path resolution remains a runtime-test gate.
