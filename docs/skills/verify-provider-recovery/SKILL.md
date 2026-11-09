---
name: verify-provider-recovery
description: Check RCC provider transactional restore, commit-gated restart recovery, and stable deferred provider resolution.
---

# Verify provider recovery

Use for provider journal restore/replay or lazy provider selection changes. Read
the [feature map](features/README.md). This skill owns provider persistence and
resolution, not artifact trust or consumer environment materialization.

## Launch

Use the prepared repository development runtime from
[rcc-development](../rcc-development/SKILL.md). Each Go invocation is a fresh
isolated short-lived session using test-owned journals and sidecars. Never run
restore, repair, or cleanup against an operator's real provider store.

## Doctor

Check `go version`, `python3 --version`, generated assets, and the pinned TLC/Java
prerequisites in [execution admission](../verify-execution-lifecycle/SKILL.md).
Before each fresh Drive invocation run `go test ./artifactprovider -run '^$'`.
After a failure inspect the retained full diagnostic, repeat Doctor, and restart
only the affected isolated test session after a relevant correction.

## Drive

Run from the repository root, using a new task-owned output directory:

```sh
python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-provider-recovery/models/ProviderRecovery.tla --out /absolute/new-dir
go test ./artifactprovider -run '^TestJournal|^TestDeferredProvider|^TestValidateV1Capabilities$|^TestValidateCapabilityIntersectionFailsClosed$' -count=1
```

Capture full logs and exit codes in task scratch outside the repository. Require
fixed and restored TLC configurations to pass and the broken configuration to
violate `OnlyCommittedRestoreVisible`: a newly restored transaction may become
visible only after its commit record, with a valid preflighted archive.
`BreakReplay` deliberately exposes uncommitted pending records after restart.
This negative control changes only the model, not source, and is not a product
bug report. A real counterexample requires replay against the implementation.

Use `$statechart` with [ProviderRecovery](models/ProviderRecovery.tla), its sibling
fixed configuration and [view.mjs](models/view.mjs). Review the rejected backup,
precommit crash/discard, and postcommit recovery flows. The view hides only
label-preserving terminal stutters. Render the chart and report its bounds.

## Teardown and evidence

Tests own their temporary directories and child processes. On failed runs inspect
for test-owned residue before starting another session; stop only verified owned
children. Verify named logs exist before cleanup. Retain canonical models, views,
source mappings and reviewed chart outputs; delete disposable task scratch and
TLC state at task closure. Report model and Linux implementation evidence
separately from remote HTTP, native Windows/macOS, power-loss, and packaged CLI
gates. These offline journeys do not establish those other gates.

## Synchronization

After a source change inspect the [source mapping](features/recovery.md), update
its model or runtime recipes if needed, rerun fixed/broken/restored checks and
focused journeys, then advance the inspected source revision. Do not edit product
code to make this maintenance pass succeed.
