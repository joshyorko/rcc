----------------------- MODULE MetadataDistribution -----------------------
EXTENDS Naturals
CONSTANT BreakRetention
VARIABLES phase, offered, authenticated, checkpoint, retained, readBack
vars == <<phase, offered, authenticated, checkpoint, retained, readBack>>

\* A single distribution attempt; the operator lock excludes competing writers.
\* Equal sequences mean identical envelopes here; equivocation is checked by
\* the existing AuthenticatedMetadata model and the implementation regressions.
Init == /\ phase = "received"
        /\ offered \in {1, 2}
        /\ authenticated \in BOOLEAN
        /\ checkpoint \in {0, 2}
        /\ retained = FALSE /\ readBack = FALSE

Authenticate == /\ phase = "received"
                /\ phase' = IF authenticated THEN "authenticated" ELSE "failed"
                /\ UNCHANGED <<offered, authenticated, checkpoint, retained, readBack>>

Retain == /\ phase = "authenticated" /\ offered >= checkpoint
          /\ phase' = "retained"
          /\ checkpoint' = IF BreakRetention THEN checkpoint ELSE offered
          /\ retained' = ~BreakRetention
          /\ UNCHANGED <<offered, authenticated, readBack>>

RejectRollback == /\ phase = "authenticated" /\ offered < checkpoint
                  /\ phase' = "failed"
                  /\ UNCHANGED <<offered, authenticated, checkpoint, retained, readBack>>

StorageFailure == /\ phase = "authenticated" /\ phase' = "failed"
                  /\ UNCHANGED <<offered, authenticated, checkpoint, retained, readBack>>

Write == /\ phase = "retained" /\ phase' = "written"
         /\ UNCHANGED <<offered, authenticated, checkpoint, retained, readBack>>

TransportFailure == /\ phase \in {"retained", "written"}
                    /\ phase' = "failed"
                    /\ UNCHANGED <<offered, authenticated, checkpoint, retained, readBack>>

ReadBack == /\ phase = "written" /\ phase' = "distributed"
            /\ readBack' = TRUE
            /\ UNCHANGED <<offered, authenticated, checkpoint, retained>>

Done == /\ phase \in {"distributed", "failed"} /\ UNCHANGED vars
Next == Authenticate \/ Retain \/ RejectRollback \/ StorageFailure \/ Write
        \/ TransportFailure \/ ReadBack \/ Done

DistributedOnlyAfterRetention == phase = "distributed" =>
  (authenticated /\ retained /\ checkpoint = offered /\ readBack)
Spec == Init /\ [][Next]_vars
=============================================================================
