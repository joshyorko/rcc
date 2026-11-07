----------------------- MODULE AuthenticatedMetadata -----------------------
EXTENDS Naturals
CONSTANTS BreakAuthentication, BreakRetainedState

\* <<sequence, authenticated by independent authority, revoked>>.
\* Carrier can replay, replace, or omit evidence; signature forgery is excluded.
Snapshots == {<<1, TRUE, FALSE>>, <<2, TRUE, TRUE>>,
              <<2, FALSE, FALSE>>, <<2, TRUE, FALSE>>}
Empty == <<0, FALSE, FALSE>>
VARIABLES phase, offered, checkpoint, highestCommitted
vars == <<phase, offered, checkpoint, highestCommitted>>

Init == /\ phase = "idle" /\ offered = Empty /\ checkpoint = Empty
        /\ highestCommitted = 0

Fetch == /\ phase \in {"idle", "denied", "permitted"}
         /\ offered' \in Snapshots /\ phase' = "fetched"
         /\ UNCHANGED <<checkpoint, highestCommitted>>

Authenticate == /\ phase = "fetched"
                /\ phase' = IF offered[2] \/ BreakAuthentication
                            THEN "authenticated" ELSE "denied"
                /\ UNCHANGED <<offered, checkpoint, highestCommitted>>

\* Comparison and durable append are serialized under the cross-process lock.
\* highestCommitted is a history variable, not a second implementation store.
Commit == /\ phase = "authenticated"
          /\ offered[1] >= checkpoint[1]
          /\ (offered[1] # checkpoint[1] \/ offered = checkpoint)
          /\ checkpoint' = offered /\ phase' = "persisted"
          /\ highestCommitted' = IF offered[1] > highestCommitted
                                 THEN offered[1] ELSE highestCommitted
          /\ UNCHANGED offered

RejectRollback == /\ phase = "authenticated"
                  /\ (offered[1] < checkpoint[1] \/
                      (offered[1] = checkpoint[1] /\ offered # checkpoint))
                  /\ phase' = "denied"
                  /\ UNCHANGED <<offered, checkpoint, highestCommitted>>

StorageFailure == /\ phase = "authenticated" /\ phase' = "denied"
                  /\ UNCHANGED <<offered, checkpoint, highestCommitted>>

Decide == /\ phase = "persisted"
          /\ phase' = IF offered[3] THEN "denied" ELSE "permitted"
          /\ UNCHANGED <<offered, checkpoint, highestCommitted>>

Restart == /\ phase' = "idle" /\ offered' = Empty
           /\ checkpoint' = IF BreakRetainedState THEN Empty ELSE checkpoint
           /\ UNCHANGED highestCommitted

Next == Fetch \/ Authenticate \/ Commit \/ RejectRollback \/ StorageFailure
        \/ Decide \/ Restart

NoUntrustedExecution == phase = "permitted" => offered[2]
NoTrustDowngrade == phase = "permitted" =>
                   (offered[1] >= highestCommitted /\ offered = checkpoint)
Spec == Init /\ [][Next]_vars
=============================================================================
