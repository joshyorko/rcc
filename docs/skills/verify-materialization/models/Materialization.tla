-------------------------- MODULE Materialization --------------------------
EXTENDS Naturals
CONSTANT Fault
VARIABLES phase, restored, checked, ready, crashed
vars == <<phase, restored, checked, ready, crashed>>
Init == /\ phase = "absent" /\ restored = FALSE /\ checked = FALSE
        /\ ready = FALSE /\ crashed = FALSE
VerifyContent == /\ phase = "absent" /\ phase' = "verified"
                 /\ UNCHANGED <<restored, checked, ready, crashed>>
BeginRestore == /\ phase = "verified" /\ phase' = "restoring"
                /\ UNCHANGED <<restored, checked, ready, crashed>>
Restore == /\ phase = "restoring" /\ phase' = "restored" /\ restored' = TRUE
           /\ UNCHANGED <<checked, ready, crashed>>
RejectRestore == /\ phase = "restoring" /\ phase' = "failed"
                 /\ UNCHANGED <<restored, checked, ready, crashed>>
Validate == /\ phase = "restored" /\ phase' = "validated" /\ checked' = TRUE
            /\ UNCHANGED <<restored, ready, crashed>>
RejectValidation == /\ phase = "restored" /\ phase' = "failed"
                    /\ UNCHANGED <<restored, checked, ready, crashed>>
PublishReady == /\ (phase = "validated" \/ (Fault /\ phase \in {"restoring", "restored"}))
                /\ phase' = "ready" /\ ready' = TRUE
                /\ UNCHANGED <<restored, checked, crashed>>
(* At most one crash. Provisional records are intent, never readiness. *)
Crash == /\ phase \in {"verified", "restoring", "restored", "validated"} /\ ~crashed
         /\ phase' = "interrupted" /\ crashed' = TRUE
         /\ restored' = FALSE /\ checked' = FALSE /\ UNCHANGED ready
Reconcile == /\ phase = "interrupted" /\ phase' = "verified"
             /\ UNCHANGED <<restored, checked, ready, crashed>>
Done == /\ phase \in {"ready", "failed"} /\ UNCHANGED vars
Next == VerifyContent \/ BeginRestore \/ Restore \/ RejectRestore \/ Validate
        \/ RejectValidation \/ PublishReady \/ Crash \/ Reconcile \/ Done
ReadyOnlyAfterValidation == ready => (restored /\ checked)
=============================================================================
