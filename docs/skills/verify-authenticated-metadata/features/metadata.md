# Admission, retained state, and publication

Source mapping inspected at `cf7a47e7b9430db4abebae68776f65f3e309624e`.

| User flow | Source entry point | Model or runtime binding |
| --- | --- | --- |
| Authenticate an independently authorized snapshot | `artifacttrust/metadata.go:VerifyMetadata` | Existing `AuthenticatedMetadata.Authenticate`; real parsing, signatures, validity windows, and roots require Go tests |
| Retain sequence and reject replay/equivocation across restarts | `artifacttrust/metadata_state.go:MetadataState.Admit` | Existing `AuthenticatedMetadata.Commit`, `RejectRollback`, and `Restart` |
| Retain authenticated revocation before denying execution | `artifacttrust/trust.go:Policy.Verify` | Existing `AuthenticatedMetadata.Decide`; `TestStrictPolicyCommitsAuthenticatedRevocationBeforeDenying` |
| Renew or revoke without discarding the deny set | `artifacttrust/operator.go:MetadataOperator.Change` | `TestMetadataOperatorRenewalPreservesCompleteSnapshotAndDenials`; runtime-only coverage |
| Distribute only after retaining the checkpoint, then confirm matching authenticated envelope | `artifacttrust/operator.go:MetadataOperator.Distribute` | `MetadataDistribution.Authenticate`, `Retain`, `Write`, `ReadBack` |
| Keep retained state after transport failure | `artifacttrust/operator.go:MetadataOperator.Distribute` | `MetadataDistribution.TransportFailure`; `TestMetadataOperatorTransportFailureRetainsCheckpoint` |
| Refresh trust before each new lease | `environmentlifecycle/trust.go:refreshMaterializationTrust` | `TestMetadataRevocationDeniesNewLeaseWithoutTerminatingRunningProcess`; runtime-only coverage |

## Live recipe

Run the skill's Doctor and Drive commands. The artifacttrust tests exercise real
signature verification and task-local journals/carriers, including restart and
transport failure. The lifecycle test starts and reaps a local child and proves
revocation denies the next lease without terminating an existing running lease.
These are offline implementation journeys, not proof of remote carrier service,
packaged CLI, other operating systems, or filesystem power-loss durability.

## Abstraction limits

`MetadataDistribution` considers one attempt, two sequence numbers, one checkpoint,
and an unforgeable authentication predicate. Its operator lock is assumed held
from retention through readback. It combines carrier write/readback errors into
terminal failure branches and does not model a process crash, arbitrary bytes,
keys, expiry, same-sequence equivocation, interprocess scheduling, or journal flush
internals. Authentication is a model input, not a cryptographic proof. The
canonical admission model covers restart/rollback separately. A failed transport
may already have changed carrier bytes; the model promises neither rollback nor
transactional remote publication. No liveness claim is made.
