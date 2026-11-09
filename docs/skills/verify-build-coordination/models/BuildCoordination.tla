----------------------- MODULE BuildCoordination -----------------------
EXTENDS Naturals
CONSTANT Fault
VARIABLES phase, owner, live, published
vars == <<phase, owner, live, published>>
Init == /\ phase = "unclaimed" /\ owner = 0 /\ live = FALSE /\ published = 0
Claim == /\ phase = "unclaimed"
         /\ phase' = "building" /\ owner' = 1 /\ live' = TRUE
         /\ UNCHANGED published
Expire == /\ phase = "building" /\ phase' = "expired" /\ live' = FALSE
          /\ UNCHANGED <<owner, published>>
Takeover == /\ phase = "expired" /\ phase' = "successor" /\ owner' = 2
            /\ live' = TRUE /\ UNCHANGED published
PublishCurrent == /\ phase \in {"building", "successor", "rejected"} /\ live
                  /\ phase' = "committed" /\ published' = owner
                  /\ UNCHANGED <<owner, live>>
(* Represents the old token: stale by expiry or by successor ownership. *)
PublishOld == /\ phase \in {"expired", "successor"}
              /\ IF Fault
                     THEN /\ phase' = "committed" /\ published' = 1
                     ELSE /\ phase' = "rejected" /\ UNCHANGED published
              /\ UNCHANGED <<owner, live>>
Done == /\ (phase = "committed" \/ (phase = "rejected" /\ ~live))
        /\ UNCHANGED vars
Next == Claim \/ Expire \/ Takeover \/ PublishCurrent \/ PublishOld \/ Done
NoStaleFirstPublication == published = 0 \/ (published = owner /\ live)
=============================================================================
