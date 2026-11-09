---
name: verify-bundle-flow
description: Check RCC bundled task execution and output copy-back ordering with a bounded user-flow model.
---

# Verify bundle flow

Use for bundled task setup, stream capture, artifact-directory validation, or finalization changes. The feature map is [features/README.md](features/README.md). Trust admission and child supervision have separate verification skills.

## Launch and Doctor

Use [rcc-development](../rcc-development/SKILL.md) to prepare the pinned Go toolchain and generated assets. Before each fresh test/CLI drive, confirm the active checkout, `python3 --version`, generated `blobs/`, and task-owned writable scratch. Tests use temporary workareas; do not run imported bundles against an operator cache. After failure, save the diagnostic and health-check fixture state before retrying.

## Drive

Run from the repository root with complete logs and exit status captured in task scratch:

```sh
python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-bundle-flow/models/BundleFlow.tla --out /absolute/new-dir
go test ./cmd -run '^Test(PrepareBundleOutputWriters|FinalizeBundleArtifacts|CopyBundleArtifacts|CopyBundleFileFailure)' -count=1 -v -timeout=30s
```

Read fixed and broken outputs with `$statechart`, using [models/view.mjs](models/view.mjs). The synthetic early-success fault must violate `NoFalseSuccess`; fixed and restored configurations must pass. Synthetic model counterexamples establish checker sensitivity, not an RCC defect. Replay any claimed real bug against the implementation.

These focused tests exercise real filesystem/output helpers, not whole-command bundle acceptance. For whole-CLI coverage use `cmd/robot_bundle_trust_real_cli_test.go:TestBundleTrustRealCLI` with a built candidate and native archive as documented in `docs/bundle-artifact-trust.md`; report missing prerequisites/skips rather than promoting helper coverage to CLI acceptance.

## Properties and bounds

- `NoFalseSuccess`: command success requires successful task completion, closed output, and successful copy-back.
- `FinalizeAfterTask`: output copy occurs after task completion and stream close.
- `TaskFailureStillFinalizes`: task failure still attempts copy-back before returning.

The finite model has one bundle/task, setup refusal, task success/failure, and output copy success/failure. It abstracts import/trust/materialization/extraction into preparation and execution/cancellation into task completion. Stream close is modeled as successful; close errors are implementation-test gates. It does not prove archive parsing, trust, atomic multi-file copy, byte preservation, deferred-panic mechanics, crash cleanup, or process supervision. Capture-disabled operation uses the same ordering but is not independently modeled. These are safety properties without eventual-completion guarantees.

## Teardown and synchronization

Verify fixture processes exited and workareas were removed; retain requested charts/reports and remove only task scratch. Source bindings are checked at `cf7a47e7b9430db4abebae68776f65f3e309624e`; see the feature file. After covered changes, inspect failures and deferred cleanup, update map/model, rerun fixed/broken/restored and focused runtime checks, then advance the binding. The model is an abstract contract rather than a proof of Go source.
