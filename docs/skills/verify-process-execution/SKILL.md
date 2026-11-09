---
name: verify-process-execution
description: Check RCC command resolution, child supervision, and lease cleanup against a bounded process model.
---

# Verify process execution

Use for execution-handle resolution, streams, cancellation, supervisor, or lease-release changes. The feature map is [features/README.md](features/README.md).

## Launch and Doctor

Use the contained toolkit and asset preparation in [rcc-development](../rcc-development/SKILL.md). Before each fresh CLI/test drive, confirm the checkout and Go toolchain, `python3 --version`, generated `blobs/`, and writable task scratch. Tests create isolated temporary RCC homes; never point fixtures at the operator's cache. After a failed drive, inspect the complete saved diagnostic and confirm no fixture process remains before retrying.

## Drive

From the repository root, save complete output and exit status in task scratch:

```sh
python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-process-execution/models/ProcessExecution.tla --out /absolute/new-dir
go test ./environmentlifecycle -run '^TestExecute(RunsPythonAndReleasesProcessScopedLease|InDirectoryUsesMaterializedEnvironmentWithSourceCWD|ReturnsChildExitCodeAndStillReleasesLease|SpawnFailureStillReleasesLease|ForwardsTerminationSignalAndReleasesLease|WithStreamsPreservesRequestResponseAndReleasesAfterReap)$' -count=1 -v -timeout=60s
go test ./environmentlifecycle -run '^Test(ExecutableResolution258|ExecutionHandleRejectsStaleLeaseOwnerIdentity)' -count=1 -v -timeout=30s
```

Read the fixed and fault-model results with `$statechart`, using [models/view.mjs](models/view.mjs). The fault must break `LeaseUntilReaped`. It is a synthetic ordering mutation, not a discovered product vulnerability. Fixed and restored checks must pass. Every real bug trace must be replayed against code before reporting a product finding.

## Properties and bounds

- `LeaseUntilReaped`: a started, unreaped direct child retains its execution lease.
- `ReturnedChildReaped`: returning after a start requires child reap.
- `SuccessMatchesChild`: success requires a successful, reaped child.

The finite model contains one direct child and six outcomes: success, nonzero exit, cancellation, spawn/setup/attachment failure. Lease acquisition is assumed successful; refusal is covered by admission verification. Resolution and supervisor preparation are abstracted into one action. Cleanup may fail and leave a lease; the model deliberately does not promise successful release. No liveness, byte-level executable validation, filesystem races, process identity implementation, stream ordering, or platform-equivalent descendant termination is proved. Explicit absolute executables are allowed; containment applies to relative and bare-name resolution. Linux kills the direct child; Windows job behavior needs native acceptance.

## Teardown

Let the bounded tests finish and verify their fixture children exited. Preserve diagnostics until review; remove only the task-owned scratch directory after retaining requested artifacts. Never delete real leases or cache entries to force a pass.

## Synchronization

Source mapping checked at `cf7a47e7b9430db4abebae68776f65f3e309624e`; see the feature file for symbols. After covered changes, inspect transitions and failure branches, update the model and map, rerun fixed/broken/restored checks and focused runtime tests, then advance the binding. Models are abstract contracts, not proofs of Go source.
