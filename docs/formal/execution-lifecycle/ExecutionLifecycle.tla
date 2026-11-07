------------------------ MODULE ExecutionLifecycle ------------------------
EXTENDS FiniteSets
CONSTANT BreakTrustDowngrade

Scenarios == {
  <<"deployment carrier", "valid signature", "authenticated metadata">>,
  <<"attacker carrier", "unsigned artifact", "unsigned metadata">>
}
Policies == {"unset", "strict", "permissive"}
Decisions == {"unchecked", "accepted", "rejected"}
Stages == {"new", "verified", "materialized", "leased", "running", "refused"}

VARIABLES scenario, policy, strictChosen, explicitOptIn, decision, stage,
          materialized, leaseHeld, processPermitted
vars == <<scenario, policy, strictChosen, explicitOptIn, decision, stage,
          materialized, leaseHeld, processPermitted>>

Init ==
  /\ scenario \in Scenarios
  /\ policy = "unset"
  /\ strictChosen = FALSE
  /\ explicitOptIn = FALSE
  /\ decision = "unchecked"
  /\ stage = "new"
  /\ materialized = FALSE
  /\ leaseHeld = FALSE
  /\ processPermitted = FALSE

SelectStrict ==
  /\ stage = "new" /\ policy = "unset"
  /\ policy' = "strict" /\ strictChosen' = TRUE
  /\ UNCHANGED <<scenario, explicitOptIn, decision, stage, materialized,
                  leaseHeld, processPermitted>>

SelectPermissive ==
  /\ stage = "new" /\ policy = "unset"
  /\ policy' = "permissive" /\ explicitOptIn' = TRUE
  /\ UNCHANGED <<scenario, strictChosen, decision, stage, materialized,
                  leaseHeld, processPermitted>>

VerifyStrict ==
  /\ stage = "new" /\ policy = "strict"
  /\ decision' = IF (scenario[1] = "deployment carrier"
                      /\ scenario[2] = "valid signature"
                      /\ scenario[3] = "authenticated metadata")
                  THEN "accepted" ELSE "rejected"
  /\ stage' = IF (scenario[1] = "deployment carrier"
                  /\ scenario[2] = "valid signature"
                  /\ scenario[3] = "authenticated metadata")
               THEN "verified" ELSE "refused"
  /\ UNCHANGED <<scenario, policy, strictChosen, explicitOptIn, materialized,
                  leaseHeld, processPermitted>>

MaterializeStrict ==
  /\ stage = "verified" /\ policy = "strict" /\ decision = "accepted"
  /\ materialized' = TRUE /\ stage' = "materialized"
  /\ UNCHANGED <<scenario, policy, strictChosen, explicitOptIn, decision,
                  leaseHeld, processPermitted>>

MaterializePermissive ==
  /\ stage = "new" /\ policy = "permissive" /\ explicitOptIn
  /\ materialized' = TRUE /\ stage' = "materialized"
  /\ UNCHANGED <<scenario, policy, strictChosen, explicitOptIn, decision,
                  leaseHeld, processPermitted>>

DowngradeOnUnsignedMetadata ==
  /\ BreakTrustDowngrade
  /\ stage = "new" /\ policy = "strict" /\ strictChosen
  /\ scenario[3] = "unsigned metadata"
  /\ policy' = "permissive" /\ explicitOptIn' = FALSE
  /\ UNCHANGED <<scenario, strictChosen, decision, stage, materialized,
                  leaseHeld, processPermitted>>

CreateLease ==
  /\ stage = "materialized" /\ materialized
  /\ ((policy = "strict" /\ decision = "accepted")
      \/ (policy = "permissive" /\ explicitOptIn))
  /\ leaseHeld' = TRUE /\ stage' = "leased"
  /\ UNCHANGED <<scenario, policy, strictChosen, explicitOptIn, decision,
                  materialized, processPermitted>>

PermitProcess ==
  /\ stage = "leased" /\ leaseHeld
  /\ ((policy = "strict" /\ decision = "accepted")
      \/ (policy = "permissive" /\ explicitOptIn))
  /\ processPermitted' = TRUE /\ stage' = "running"
  /\ UNCHANGED <<scenario, policy, strictChosen, explicitOptIn, decision,
                  materialized, leaseHeld>>

Refuse ==
  /\ stage = "new" /\ policy = "strict" /\ decision = "rejected"
  /\ stage' = "refused"
  /\ UNCHANGED <<scenario, policy, strictChosen, explicitOptIn, decision,
                  materialized, leaseHeld, processPermitted>>

Next == SelectStrict \/ SelectPermissive \/ VerifyStrict \/ MaterializeStrict
        \/ MaterializePermissive \/ DowngradeOnUnsignedMetadata \/ CreateLease
        \/ PermitProcess \/ Refuse

NoTrustDowngrade == strictChosen => policy = "strict"

NoUntrustedExecution == processPermitted =>
  (leaseHeld /\ materialized
   /\ ((policy = "strict" /\ decision = "accepted"
        /\ scenario[1] = "deployment carrier"
        /\ scenario[2] = "valid signature"
        /\ scenario[3] = "authenticated metadata")
       \/ (policy = "permissive" /\ explicitOptIn)))

Spec == Init /\ [][Next]_vars
=============================================================================
