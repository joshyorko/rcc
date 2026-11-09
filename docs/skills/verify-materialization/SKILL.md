---
name: verify-materialization
description: Verify RCC verified-content restoration, readiness publication, and interrupted-materialization recovery.
---

# Verify materialization

Use when acquisition, portable restoration, readiness records, or reconciliation
changes. [Feature map](features/README.md) binds model actions to implementation.
Artifact trust admission remains owned by
[verify-execution-lifecycle](../verify-execution-lifecycle/SKILL.md).

## Launch and doctor

From the repository root, select the checked-in development toolchain using
[rcc-development](../rcc-development/SKILL.md). Prepare generated assets if absent.
Run `go test ./environmentlifecycle -run '^$'` before the first drive and after
unexpected failure. This compile-only doctor does not establish runtime behavior.
Each test uses its own temporary RCC home and local provider, with no shared server.

## Drive

1. Run `python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-materialization/models/Materialization.tla --out /absolute/new-task-directory`.
   Fixed and restored must pass. The broken configuration must fail `ReadyOnlyAfterValidation`.
   Use `$statechart` with `models/view.mjs` for both configurations.
2. Run `go test ./environmentlifecycle -run '^(TestMaterializationFailureNeverPublishesReadyRecord|TestWarmAcquireDoesNotTouchProviderOrBuilder|TestWarmAcquireRejectsMaterializedPythonABIMismatch|TestWarmAcquireAndExecutionRejectSymlinkedPythonParent|TestPublishStagesTrustBeforeManifestCommitAndRetryRecovers)$' -count=1`.
3. For crash/recovery changes, also run `go test ./environmentlifecycle -run '^(TestSubprocessCrashRestartMatrixCoversEveryLifecycleHook|TestIndependentProcessRepairAcquire|TestAcquireAndContentGCRaceSharesGlobalContentTransaction)$' -count=1`.

Capture complete output and exit status in task scratch. After any failed drive,
repeat doctor and diagnose before retrying. The broken model permits readiness
before restoration/validation; it is deliberate model falsification, not a proven
product bug. Replay reported fault paths using real rejection tests; an absent bug
in Go remains a model fault rather than an implementation finding.

## Rule and bounds

`ReadyOnlyAfterValidation`: readiness requires restoration plus executable and runtime compatibility
checks. One artifact, one worker, one restoration at a time, at most one crash and
one restart are modeled. Reconciliation discards provisional intent and retries;
it never promotes intent to readiness. ReadyOnlyAfterValidation is bounded, without fairness.
Trust, arbitrary content corruption, warm-cache identity checks, active leases,
GC, provider publication, storage durability, and platform-specific filesystem
semantics remain source/runtime gates outside this model. Content can enter local
CAS before detached trust verification; this model does not claim otherwise.

## Teardown

Tests own and clean temporary homes/providers. Stop only task-owned test children
left by timeout. Confirm evidence exists before cleanup. Keep reusable models/charts
in this skill, remove task scratch, and report platform and CLI acceptance separately.

## Source synchronization

Source inspected at `cf7a47e7b9430db4abebae68776f65f3e309624e`.
After covered changes, update [source bindings](features/readiness.md), rerun
fixed/broken/restored and relevant executable recipes, then advance this record.
