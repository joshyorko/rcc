-------------------------- MODULE ProviderRecovery --------------------------
CONSTANT BreakReplay
VARIABLES phase, validArchive, pending, committed, visible
vars == <<phase, validArchive, pending, committed, visible>>
Init == /\ phase = "supplied" /\ validArchive \in BOOLEAN
        /\ pending = FALSE /\ committed = FALSE /\ visible = FALSE

Validate == /\ phase = "supplied"
            /\ phase' = IF validArchive THEN "validated" ELSE "rejected"
            /\ UNCHANGED <<validArchive, pending, committed, visible>>
Begin == /\ phase = "validated" /\ phase' = "begun"
         /\ UNCHANGED <<validArchive, pending, committed, visible>>
StageRecords == /\ phase = "begun" /\ phase' = "pending"
                /\ pending' = TRUE
                /\ UNCHANGED <<validArchive, committed, visible>>
Commit == /\ phase = "pending" /\ phase' = "committed"
          /\ committed' = TRUE
          /\ UNCHANGED <<validArchive, pending, visible>>
Publish == /\ phase = "committed" /\ phase' = "published"
           /\ visible' = TRUE
           /\ UNCHANGED <<validArchive, pending, committed>>
Crash == /\ phase \in {"validated", "begun", "pending", "committed", "published"}
         /\ phase' = "crashed" /\ visible' = FALSE
         /\ UNCHANGED <<validArchive, pending, committed>>
Replay == /\ phase = "crashed" /\ phase' = "recovered"
          /\ visible' = (committed \/ (BreakReplay /\ pending))
          /\ UNCHANGED <<validArchive, pending, committed>>
Done == /\ phase \in {"rejected", "published", "recovered"} /\ UNCHANGED vars
Next == Validate \/ Begin \/ StageRecords \/ Commit \/ Publish \/ Crash \/ Replay \/ Done

OnlyCommittedRestoreVisible == visible => (committed /\ validArchive)
Spec == Init /\ [][Next]_vars
=============================================================================
