------------------------ MODULE ProcessExecution ------------------------
CONSTANT ReleaseBeforeReap
VARIABLES phase, outcome, lease, started, reaped, reported
vars == <<phase, outcome, lease, started, reaped, reported>>

Init ==
  /\ phase = "request"
  /\ outcome \in {"success", "nonzero", "cancel", "spawn failure", "setup failure", "attach failure"}
  /\ lease = FALSE /\ started = FALSE /\ reaped = FALSE
  /\ reported = "pending"

AcquireLease ==
  /\ phase = "request" /\ phase' = "leased" /\ lease' = TRUE
  /\ UNCHANGED <<outcome, started, reaped, reported>>
ResolveAndPrepare ==
  /\ phase = "leased" /\ outcome # "setup failure"
  /\ phase' = "prepared"
  /\ UNCHANGED <<outcome, lease, started, reaped, reported>>
SetupFailure ==
  /\ phase = "leased" /\ outcome = "setup failure"
  /\ phase' = "cleanup" /\ reported' = "error"
  /\ UNCHANGED <<outcome, lease, started, reaped>>
StartChild ==
  /\ phase = "prepared" /\ outcome # "spawn failure"
  /\ phase' = "running" /\ started' = TRUE
  /\ UNCHANGED <<outcome, lease, reaped, reported>>
SpawnFailure ==
  /\ phase = "prepared" /\ outcome = "spawn failure"
  /\ phase' = "cleanup" /\ reported' = "error"
  /\ UNCHANGED <<outcome, lease, started, reaped>>
TerminateChild ==
  /\ phase = "running" /\ outcome \in {"cancel", "attach failure"}
  /\ phase' = "terminating"
  /\ UNCHANGED <<outcome, lease, started, reaped, reported>>
ReapChild ==
  /\ ((phase = "running" /\ outcome \in {"success", "nonzero"}) \/ phase = "terminating")
  /\ phase' = "cleanup" /\ reaped' = TRUE
  /\ reported' = CASE outcome = "success" -> "success"
                     [] outcome = "nonzero" -> "child failure"
                     [] OTHER -> "error"
  /\ UNCHANGED <<outcome, lease, started>>
ReleaseLease ==
  /\ phase = "cleanup" /\ phase' = "returned" /\ lease' = FALSE
  /\ UNCHANGED <<outcome, started, reaped, reported>>
ReleaseFailure ==
  /\ phase = "cleanup" /\ phase' = "returned" /\ reported' = "error"
  /\ UNCHANGED <<outcome, lease, started, reaped>>
PrematureRelease ==
  /\ ReleaseBeforeReap /\ phase = "running" /\ lease
  /\ lease' = FALSE
  /\ UNCHANGED <<phase, outcome, started, reaped, reported>>
Done == phase = "returned" /\ UNCHANGED vars

Next == AcquireLease \/ ResolveAndPrepare \/ SetupFailure \/ StartChild
        \/ SpawnFailure \/ TerminateChild \/ ReapChild \/ ReleaseLease
        \/ ReleaseFailure \/ PrematureRelease \/ Done
LeaseUntilReaped == (started /\ ~reaped) => lease
ReturnedChildReaped == (phase = "returned" /\ started) => reaped
SuccessMatchesChild == reported = "success" => (outcome = "success" /\ reaped)
Spec == Init /\ [][Next]_vars
=============================================================================
