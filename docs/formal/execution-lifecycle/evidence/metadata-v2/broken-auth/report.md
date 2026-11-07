# AuthenticatedMetadata

TLC reached 72 states and 248 steps. The chart draws 7 states and 16 arrows.
The chart does not draw 5 steps that stay in one chart state.

## NoUntrustedExecution breaks
The shortest path has 4 steps:
0. Ready for next admission
1. carrier supplies a snapshot: Carrier snapshot received
2. skip signature verification [injected fault]: Authority checked
3. compare sequence and flush checkpoint: Checkpoint retained
4. apply authenticated revocations: Execution permitted from forged metadata

## Rules that hold
NoTrustDowngrade, NoDeadlock
