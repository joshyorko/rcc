---
name: verify-lease-reclamation
description: Verify RCC lease identity, conservative collection, stale reconciliation and ordered reclamation.
---

# Verify lease and reclamation

Use after changes to leases, PID identity classification, GC, reference roots or
reconciliation. Read the [feature map](features/README.md) and its source bindings.
This skill owns only its guidance/models; report product failures separately.

## Launch and Doctor

Run from the RCC repository root. Follow `rcc-development` for the checked-in
toolkit and generated assets. Doctor: confirm `go version` matches the toolkit,
`blobs/` is prepared, Python is available and the feature's source symbols still
exist. Check free disk space before building. Use offline fixture tests; never
point collection at the workstation's RCC home.

For the model, use Java and checksum-pinned TLC through the shared checker:

```sh
python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-lease-reclamation/models/LeaseReclamation.tla --out /absolute/new-dir
```

Fixed and restored configurations must pass. The broken configuration must
violate `NoProtectedDeletion`; it tests the removed lease-protection guard.
Use `$statechart` on the model with its fixed config and `models/view.mjs`, then
inspect the broken configuration. Keep generated checker state outside the repo.

## Drive

Use a fresh test process and fixture-owned home for each invocation. Capture full
output and exit status before inspecting summaries:

```sh
go test ./environmentlifecycle -count=1 -timeout=90s -run '^(TestLeaseAndExecutionHandleHaveExplicitTypedLifecycle|TestLeaseFailsClosedWhenStrongOwnerIdentityIsUnavailable|TestLeaseFailsClosedWhenStrongOwnerIdentityIsAmbiguous|TestReconcileTreatsPIDReuseAsStale|TestGCDryRunPreservesProvisionalRecordsAndReferenceRoot|TestGCSeparateProcessSkipsActiveChildThenReclaimsAfterRelease|TestCrashedLeaseIsReconciledThenReclaimed|TestActiveLeaseEmbeddedClosureSurvivesDamagedReferenceRoot|TestAmbiguousIdentityLeaseProtectsEmbeddedClosureWithDamagedRoot|TestMaterializationEvictionRetiresRootBeforeCASReclamation|TestRetiredClosureReclaimsAfterRetentionUnderPressureAndMaxBytes)$' -v
```

The subprocess fixtures have readiness deadlines and cleanup callbacks. Inspect
them before running if changed. On failure, preserve evidence, diagnose and
repeat Doctor before another drive. A skip is missing coverage, not success.

## Teardown and limits

Let tests run cleanup; verify their child processes have exited. Keep useful
receipts outside disposable fixture homes until the final report, then remove
task scratch. Do not delete existing user caches or containers.

The model proves a bounded safety contract, not the Go implementation, native
filesystem behavior, every retention policy or eventual collection. GC's
ambiguous-owner protection does not describe admin repair. Linux subprocess
evidence is not Windows/macOS or packaged CLI acceptance. Update source bindings
only after reinspection and fixed/broken/restored checks plus affected drives.
