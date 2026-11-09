---
name: verify-build-coordination
description: Verify RCC build-claim expiry, takeover, and publication fencing with a bounded model and executable Go regressions.
---

# Verify build coordination

Use for owner/follower, claim expiry, takeover, or coordinator-publication changes.
[Feature map](features/README.md) binds the abstraction to source. This skill owns
its model; it does not replace artifact trust or real multi-process acceptance.

## Launch and doctor

From the repository root, select the checked-in development toolchain using
[rcc-development](../rcc-development/SKILL.md). Prepare generated assets if absent.
Run `go test ./buildcoord -run '^$'` before the first drive and after an unexpected
failure. A compile-only doctor establishes test-runtime readiness, not behavior.
Tests own isolated temporary coordinators and fake clocks; no shared daemon is used.

## Drive

1. Run `python3 docs/skills/verify-execution-lifecycle/scripts/check_models.py docs/skills/verify-build-coordination/models/BuildCoordination.tla --out /absolute/new-task-directory`.
   Fixed and restored must pass; the deliberately broken configuration must fail
   `NoStaleFirstPublication`. Use `$statechart` with `models/view.mjs` on both configurations.
2. Run `go test ./buildcoord -run '^(TestFilesystemCoordinatorFencesTakeoverAndRejectsStalePublish|TestCommittedArtifactWinsAndDivergenceIsVisible|TestCommittedArtifactRecoveryAcknowledgesClaimTransaction|TestFailedPrewarmBuilderRelinquishesClaimForRetry|TestWaitingPrewarmTakesOverAfterBuilderFailure)$' -count=1`.
3. For cross-process changes, also run `go test ./buildcoord -run '^(TestActiveFilesystemLockCannotBeStolenAcrossProcesses|TestPrewarmWaiterReusesCommittedArtifactAcrossProcesses)$' -count=1`.

Save complete output and exit status in task scratch before interpreting a failure.
After failure, repeat doctor and inspect the failing case before another drive.
The broken model is a synthetic removal of publication fencing, not a reported Go
vulnerability. Existing tests exercise the corresponding real rejection path;
they do not prove the model equivalent to all coordinator behavior.

## Rule and bounds

`NoStaleFirstPublication`: the first authoritative publication belongs to the current live owner.
Two owners, one key, one expiry, one takeover, and one publication are modeled.
The publication guard and authoritative write are atomic under the per-key lock.
No fairness or eventual-completion claim is made. Heartbeat timing, commit-journal
crash recovery, trust cryptography, resource enforcement, independent publication,
local fallback, and operating-system lock correctness are outside this model.
Same-digest retries can succeed without current ownership after commitment;
this model stops at the first publication and makes no claim about that retry.
Epochs are claim tokens, not globally monotonic counters after claim removal.

## Teardown

Go tests clean their own temporary state. Stop only task-owned failed test child
processes if a test times out. Confirm saved evidence exists before removing any
scratch state. Retain only reusable models/charts in this skill, then remove task
scratch. Report Linux test evidence separately from Windows/macOS and CLI proof.

## Source synchronization

Source inspected at `cf7a47e7b9430db4abebae68776f65f3e309624e`.
After a covered source change, update [source bindings](features/ownership.md),
rerun fixed/broken/restored and the relevant real tests, then advance this record.
