---------------------------- MODULE BundleFlow ----------------------------
CONSTANT ReportBeforeFinalization
VARIABLES phase, task, copied, closed, reported
vars == <<phase, task, copied, closed, reported>>

Init ==
  /\ phase = "opening" /\ task = "not started"
  /\ copied = "pending" /\ closed = FALSE /\ reported = "pending"
PrepareBundle ==
  /\ phase = "opening" /\ phase' = "prepared"
  /\ UNCHANGED <<task, copied, closed, reported>>
RejectSetup ==
  /\ phase \in {"opening", "prepared"}
  /\ phase' = "returned" /\ reported' = "setup error"
  /\ UNCHANGED <<task, copied, closed>>
RunTask ==
  /\ phase = "prepared" /\ phase' = "running" /\ task' = "running"
  /\ UNCHANGED <<copied, closed, reported>>
TaskCompletes ==
  /\ phase = "running" /\ phase' = "reaped"
  /\ task' \in {"success", "failure"}
  /\ UNCHANGED <<copied, closed, reported>>
CloseOutput ==
  /\ phase = "reaped" /\ phase' = "finalizing" /\ closed' = TRUE
  /\ UNCHANGED <<task, copied, reported>>
CopyOutput ==
  /\ phase = "finalizing" /\ phase' = "finalized"
  /\ copied' \in {"success", "failure"}
  /\ UNCHANGED <<task, closed, reported>>
ReturnResult ==
  /\ phase = "finalized" /\ phase' = "returned"
  /\ reported' = CASE copied = "failure" -> "copy error"
                     [] task = "failure" -> "task error"
                     [] OTHER -> "success"
  /\ UNCHANGED <<task, copied, closed>>
PrematureSuccess ==
  /\ ReportBeforeFinalization /\ phase = "reaped" /\ task = "success"
  /\ phase' = "returned" /\ reported' = "success"
  /\ UNCHANGED <<task, copied, closed>>
Done == phase = "returned" /\ UNCHANGED vars
Next == PrepareBundle \/ RejectSetup \/ RunTask \/ TaskCompletes
        \/ CloseOutput \/ CopyOutput \/ ReturnResult \/ PrematureSuccess \/ Done

NoFalseSuccess == reported = "success" =>
  (task = "success" /\ copied = "success" /\ closed)
FinalizeAfterTask == copied # "pending" =>
  (task \in {"success", "failure"} /\ closed)
TaskFailureStillFinalizes ==
  (phase = "returned" /\ task = "failure") => copied # "pending"
Spec == Init /\ [][Next]_vars
=============================================================================
