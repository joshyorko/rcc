------------------------- MODULE LeaseReclamation -------------------------
EXTENDS Naturals
CONSTANT BreakLeaseProtection
VARIABLES phase, lease, dryRun, pathPresent, contentPresent
vars == <<phase, lease, dryRun, pathPresent, contentPresent>>

Init ==
  /\ phase = "ready"
  /\ lease = "none"
  /\ dryRun \in BOOLEAN
  /\ pathPresent = TRUE
  /\ contentPresent = TRUE

AcquireLease ==
  /\ phase = "ready" /\ lease = "none"
  /\ lease' = "active"
  /\ UNCHANGED <<phase, dryRun, pathPresent, contentPresent>>

IdentityUnavailable ==
  /\ phase = "ready" /\ lease = "active"
  /\ lease' = "ambiguous"
  /\ UNCHANGED <<phase, dryRun, pathPresent, contentPresent>>

IdentityRestored ==
  /\ phase = "ready" /\ lease = "ambiguous"
  /\ lease' = "active"
  /\ UNCHANGED <<phase, dryRun, pathPresent, contentPresent>>

OwnerExits ==
  /\ phase = "ready" /\ lease \in {"active", "ambiguous"}
  /\ lease' = "stale"
  /\ UNCHANGED <<phase, dryRun, pathPresent, contentPresent>>

ReleaseLease ==
  /\ phase = "ready" /\ lease # "none"
  /\ lease' = "none"
  /\ UNCHANGED <<phase, dryRun, pathPresent, contentPresent>>

ReconcileStale ==
  /\ phase = "ready" /\ lease = "stale" /\ ~dryRun
  /\ lease' = "none"
  /\ UNCHANGED <<phase, dryRun, pathPresent, contentPresent>>

EvictPath ==
  /\ phase = "ready" /\ ~dryRun
  /\ (lease = "none" \/
      (BreakLeaseProtection /\ lease \in {"active", "ambiguous"}))
  /\ phase' = "path-removed" /\ pathPresent' = FALSE
  /\ UNCHANGED <<lease, dryRun, contentPresent>>

RemoveReadyRecord ==
  /\ phase = "path-removed"
  /\ phase' = "ready-record-removed"
  /\ UNCHANGED <<lease, dryRun, pathPresent, contentPresent>>

RetireRoot ==
  /\ phase = "ready-record-removed"
  /\ phase' = "retired"
  /\ UNCHANGED <<lease, dryRun, pathPresent, contentPresent>>

ReclaimContent ==
  /\ phase = "retired"
  /\ phase' = "reclaimed" /\ contentPresent' = FALSE
  /\ UNCHANGED <<lease, dryRun, pathPresent>>

Done ==
  /\ (phase = "reclaimed" \/ (dryRun /\ phase = "ready"))
  /\ UNCHANGED vars

Next == AcquireLease \/ IdentityUnavailable \/ IdentityRestored \/ OwnerExits
        \/ ReleaseLease \/ ReconcileStale \/ EvictPath \/ RemoveReadyRecord
        \/ RetireRoot \/ ReclaimContent \/ Done

NoProtectedDeletion ==
  /\ (lease \in {"active", "ambiguous"} => pathPresent /\ contentPresent)
  /\ (dryRun => pathPresent /\ contentPresent)

Spec == Init /\ [][Next]_vars
=============================================================================
