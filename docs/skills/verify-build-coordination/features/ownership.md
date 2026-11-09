# Build ownership and first publication

A caller claims a build or waits for its live owner. Expiry permits a successor.
The expired or replaced builder cannot publish the first authoritative result.
Once an artifact is committed, callers reuse it; a divergent candidate is reported.

## Source bindings

- `buildcoord/buildcoord.go:Filesystem.ClaimContext`: lock, authoritative-result
  reuse, live-owner contention, and expiry takeover.
- `buildcoord/buildcoord.go:Filesystem.Heartbeat`: matching live owner/epoch renewal.
- `buildcoord/buildcoord.go:Filesystem.Publish`: verify candidate, lock, existing
  result handling, then matching unexpired claim before the authoritative write.
- `buildcoord/buildcoord.go:Filesystem.recoverCommittedLocked`: repair interrupted
  bookkeeping without deleting a foreign claim. This is a runtime gate only.
- `buildcoord/buildcoord.go:Filesystem.prewarmOne`: waiting, retry after failure,
  staging, heartbeat, cleanup, and relinquishment on failure.

## Model mapping

`Claim`, `Expire`, and `Takeover` abstract a single key with owner tokens 1 and 2.
`PublishCurrent` abstracts the guarded first write. `PublishOld` attempts the first
builder's captured token after expiry or takeover. Its fixed branch rejects the
attempt; `Fault = TRUE` skips the real freshness guard. `Done` is terminal stutter.
`live` abstracts the expiry comparison; `published` records the publisher, not an
artifact digest. No step is hidden by the view; terminal stutter is not an arrow.

## Executable recipe

Follow the skill's Drive commands. `TestFilesystemCoordinatorFencesTakeoverAndRejectsStalePublish`
replays claim → clock expiry → takeover → old publish rejection → successor commit.
`TestCommittedArtifactWinsAndDivergenceIsVisible` covers postcommit behavior omitted
from the model. These tests use real filesystem coordinator methods with fake time;
the cross-process recipe separately checks actual OS lock contention and reuse.
