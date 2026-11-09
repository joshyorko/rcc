# Transactional restore and deferred resolution

Source mapping inspected at `cf7a47e7b9430db4abebae68776f65f3e309624e`.

| User flow | Source entry point | Model/runtime binding |
| --- | --- | --- |
| Reject unsafe or incomplete archives before publishing | `artifactprovider/journal.go:Journal.Restore` | `ProviderRecovery.Validate`; bounded paths, digests, closure and size checks require Go tests |
| Stage a complete transaction before publishing manifests | `artifactprovider/journal.go:Journal.Restore` | `Begin`, `StageRecords`, `Commit`, `Publish` |
| Restart after interrupted restore | `artifactprovider/journal.go:NewJournal` | `Crash`, `Replay`: records with a transaction ID remain pending until `restore-commit` |
| Ignore incomplete restore objects after restart | `artifactprovider/journal.go:NewJournal` | `OnlyCommittedRestoreVisible`; `TestJournalRestoreFailureLeavesRecoverableUncommittedTransaction` |
| Preserve committed restore and avoid duplicate history | `artifactprovider/journal.go:Journal.Restore` | `TestJournalRestoreIsRestartDurableWithoutDuplicateHistory`; idempotent retry is runtime-only |
| Resolve a lazy provider once | `artifactprovider/deferred.go:deferredProvider.get` | `sync.Once`: unresolved → ready or failed; runtime-only |
| Keep resolution failures stable and redact implementation errors | `artifactprovider/deferred.go:deferredProvider.get` | `TestDeferredProviderStableError`, `TestDeferredProviderDoesNotExposeResolverError`; runtime-only |
| Reject incompatible provider requirements | `artifactprovider/deferred.go:ValidateCapabilityIntersection` | `TestValidateCapabilityIntersectionFailsClosed`; runtime-only |

## Live recipe

Run Doctor, then the skill's artifactprovider Drive command. These tests operate
on fresh temporary journals, restore real tar archives, restart provider objects,
and launch bounded test child processes at restore boundaries. No external server
or credentials are required. `TestJournalRestoreChildProcessBoundary` asserts
uncommitted objects are invisible; the crash matrix additionally checks recovered
provider health. The matrix's health check alone is not visibility proof.

## Abstraction limits

`ProviderRecovery` models one transaction with boolean valid-archive and complete
pending-record predicates. Commit records and validated sidecar bytes survive
process termination; arbitrary filesystem corruption, power loss, concurrent
transactions, policy-file application errors, partial record bytes, GC, HTTP,
and archive parsing are omitted. The journal's administration/data locks are
assumed to serialize restore visibility. The model does not claim that no sidecar
file exists before commit: files can be installed before commit but remain
unpublished to external operations under the lock and are discarded on recovery
when no committed transaction references them. Restoration preserves existing
state; `visible` means only the newly restored transaction. No liveness proof.

The deferred provider's permanent ready/failed state is documented and exercised
by Go tests rather than a second model. A new provider wrapper instance is needed
to retry resolver setup after failure; repeated methods on the same wrapper do
not rerun the resolver.
