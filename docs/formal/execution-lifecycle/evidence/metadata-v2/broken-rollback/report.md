# AuthenticatedMetadata

TLC reached 87 states and 297 steps. The chart draws 7 states and 17 arrows.
The chart does not draw 3 steps that stay in one chart state.

## NoTrustDowngrade breaks
The shortest path has 8 steps:
0. Ready for next admission
1. carrier supplies a snapshot: Carrier snapshot received
2. verify independent authority: Authority checked
3. compare sequence and flush checkpoint: Checkpoint retained
4. restart loses checkpoint [injected fault]: Ready for next admission
5. carrier supplies a snapshot: Carrier snapshot received
6. verify independent authority: Authority checked
7. compare sequence and flush checkpoint: Checkpoint retained
8. apply authenticated revocations: Execution permitted after rollback

## Rules that hold
NoUntrustedExecution, NoDeadlock
