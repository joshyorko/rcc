# Restoration and readiness

RCC restores verified portable content, validates its executable and runtime
compatibility, then publishes a ready record. An interruption leaves provisional
intent that reconciliation removes; intent alone cannot make the environment ready.

## Source bindings

- `environmentlifecycle/materialize.go:Acquirer.Acquire`: content transaction before
  artifact transaction, held through acquisition and materialization.
- `environmentlifecycle/materialize.go:Acquirer.acquireLocked`: reconcile, local or
  remote content, trust verification, then warm reuse or restoration. Corrupt local
  cache is not silently treated as a remote miss; active leases block rematerialization.
- `environmentlifecycle/materialize.go:LocalMaterializer.materializeLocked`:
  verified-content → materializing → restore → regular Python executable → runtime
  compatibility → ready. Errors return before readiness publication.
- `environmentlifecycle/reconcile.go:reconcileLockedWithRepair`: removes provisional
  verified/materializing records; an existing ready record remains authoritative.
- `environmentlifecycle/materialize.go:warmMaterialization`: revalidates ready-record
  identity, target directory, executable, compatibility, and metadata before reuse.
- `environmentlifecycle/publish.go:Publish`: object upload and detached trust staging
  precede manifest commit. This adjacent producer ordering is an executable gate.

## Model mapping

`VerifyContent` starts with verified content as an abstract contract.
`BeginRestore`, `Restore`, `Validate`, and `PublishReady` preserve implementation
ordering. `RejectRestore` and `RejectValidation` terminate unsuccessfully. `Crash`
may occur at a provisional boundary once; `Reconcile` discards partial-progress
knowledge and retries from verified content. Atomic restoration is an abstraction;
no partial filesystem crash consistency is proved. The artifact transaction excludes
concurrent restoration in this model. `Fault = TRUE` permits early readiness,
representing the ordering fault the invariant must detect. `Done` is terminal stutter.
No step is hidden by the view; terminal stutter is not an arrow.

## Executable recipe

Follow the skill's Drive commands. `TestMaterializationFailureNeverPublishesReadyRecord`
corrupts actual local legacy content after verification and confirms failed restore
cannot publish readiness. Warm tests validate actual cached environments without a
remote provider. The crash matrix exercises implementation crash hooks in subprocesses;
its broader lease/GC coverage must not be attributed to this narrow formal model.
