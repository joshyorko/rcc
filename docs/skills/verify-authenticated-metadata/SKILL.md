---
name: verify-authenticated-metadata
description: Check RCC authenticated metadata admission, retained checkpoints, and operator distribution against bounded models and offline implementation journeys.
---

# Verify authenticated metadata

Use for independent metadata authority, sequence floors, journal durability,
operator renewal/revocation, or detached publication changes. Read the
[feature map](features/README.md) before driving. This skill owns metadata;
[execution admission](../verify-execution-lifecycle/SKILL.md) owns caller policy
selection and acquisition-to-lease receipt propagation.

## Launch

Run from the repository root in the existing development toolkit or prepared Go
runtime described by [rcc-development](../rcc-development/SKILL.md). Each Go test
is a fresh isolated CLI-like session with test-owned homes and carriers. Do not
start a provider server or use the operator's real metadata journals.

## Doctor

Check `go version`, `python3 --version`, prepared generated assets, and the pinned
TLC/Java prerequisites in the execution-admission skill. Before each fresh test
invocation, run `go test ./artifacttrust -run '^$'` to verify the runtime can load
the package. After a failed drive, inspect its complete log, repeat Doctor, and
recreate only that test session before retrying after a relevant correction.

## Drive

Use a new task-owned scratch directory outside the repository for checker output:

```sh
python3 docs/formal/execution-lifecycle/check.py --model AuthenticatedMetadata
python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-authenticated-metadata/models/MetadataDistribution.tla --out /absolute/new-dir

go test ./artifacttrust -run '^TestMetadata|^TestStrictPolicyCommitsAuthenticatedRevocationBeforeDenying' -count=1
go test ./environmentlifecycle -run '^TestMetadataRevocationDeniesNewLeaseWithoutTerminatingRunningProcess$|^TestNewLeaseRefreshesRevocationsWithoutChangingRunningLease$' -count=1
```

Capture full output and exit codes in task scratch. Both model harnesses must pass
fixed and restored configurations and reject the designated model faults. The
new distribution model's `DistributedOnlyAfterRetention` rule says publication
can be confirmed only for authenticated bytes retained at the offered checkpoint
and read back as a matching authenticated envelope. Its broken configuration deliberately skips retention;
this is a model-only negative control, not a source mutation or discovered bug.

Use `$statechart` with
[MetadataDistribution](models/MetadataDistribution.tla), its fixed configuration,
and [view.mjs](models/view.mjs). For the existing admission model use
[AuthenticatedMetadata](../../formal/execution-lifecycle/AuthenticatedMetadata.tla)
with its sibling fixed configuration and `metadata-view.mjs`. Keep these canonical
models in their existing location. Render and inspect each generated chart, and
report hidden steps and abstraction limits. No transitions are hidden by the
new view except label-preserving terminal stutters.

## Teardown and evidence

Go tests own temporary state and child cleanup. Inspect failures for leaked
children before continuing; stop only verified test-owned residue. Confirm logs
exist before cleanup. Preserve durable canonical models, views, source bindings,
and reviewed chart outputs; delete disposable checker states and task scratch
at task closure. Report model checks separately from Go journeys, packaged CLI
acceptance, remote service acceptance, and other-platform evidence.

For packaged metadata security acceptance follow the existing execution-admission
skill's `TestMetadataSecurityRealCLI` instructions with `RCC_ACCEPTANCE_BINARY`.
A skip is not acceptance; the optional N-1 test needs its independent binary.

## Synchronization

After a covered change, read the mapped functions, update the feature/model if
needed, run fixed/broken/restored checks and focused Go journeys, and update the
source mapping only after that inspection. Do not change product code to make
this skill pass. The [feature](features/metadata.md) lists boundaries and the
runtime-only operator renewal/revocation coverage.
