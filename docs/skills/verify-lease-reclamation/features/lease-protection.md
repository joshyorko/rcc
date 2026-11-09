# Lease protection and collection

An executing environment receives a durable lease with a PID/start identity and
an embedded protected closure. Collection skips active owners and uncertain
owners. A missing owner or reused PID makes the lease stale; reconciliation may
remove it. Release is idempotent but rejects a changed stored lease snapshot.

When retention and policy permit collection, it removes the materialization,
removes the ready record, then retires its reference root. Content collection
honors live roots, retained retired roots, policy and embedded lease closures.
Dry-run preserves storage and does not repair stale leases.

## Source bindings

Source inspected at `cf7a47e7b9430db4abebae68776f65f3e309624e`:

- `environmentlifecycle/lease.go:LocalMaterializer.Lease` checks ready identity,
  refreshed trust, root and strong owner identity; `LocalMaterializer.Release`
  checks the stored snapshot and tolerates an already absent lease.
- `environmentlifecycle/reconcile.go:classifyLease` distinguishes active, stale
  and ambiguous; `reconcileLockedWithRepair` removes only stale leases.
- `environmentlifecycle/gc.go:collectDigestLocked` skips active/ambiguous leases,
  applies policy/retention, then removes path and ready record before retiring
  the root. `durableProtectedDigests` preserves embedded non-stale closures;
  unreadable lease records conservatively protect all content.
- `environmentlifecycle/gc.go:collectUnreferencedContent` requires pressure or a
  byte budget, honors retention and records interrupted content reclamation.
- `environmentlifecycle/admin.go:Repair` and `RepairFromProvider` refuse active
  leases only. The model's ambiguity protection applies to GC, not every repair.

## Model boundary

`models/LeaseReclamation.tla:NoProtectedDeletion` requires active/ambiguous
leases and dry-run to preserve materialization and content. One artifact, one
lease and one content object are modeled; all candidates already satisfy
retention, pressure and policy eligibility. Identity uncertainty can recover,
or owner exit can establish staleness. New lease issuance abstracts ready/root,
trust and strong owner checks already covered by admission and Go tests.

Path removal, ready-record removal and root retirement remain distinct states.
The GC transaction excludes concurrent lease acquisition across those states.
Crashes/cancellation may stop between them; the model permits those intermediate
states but does not model restart/partial directory traversal. A ready record
is consequently not modeled as proof that a complete path still exists.

The model excludes CAS sharing across artifacts, damaged roots, malformed
records, filesystem races/power loss, real PID lookup and platform-specific
locks. Existing Go tests exercise damaged roots and cross-process ownership.
No liveness or eventual-reclamation claim is made. The broken configuration
removes the active/ambiguous lease guard; its counterexample is a synthetic
fault, not a demonstrated RCC bug.

## Live recipe

Use the parent skill's Drive command. It exercises an actual lease-owning child,
post-crash reconciliation, protected CAS content with damaged roots, and ordered
eviction/retention in isolated fixture homes. Confirm the selected tests run and
record skips individually; do not claim real CLI or other-platform acceptance.
