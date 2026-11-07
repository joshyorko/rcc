# ExecutionLifecycle

TLC reached 21 states and 24 steps. The chart draws 9 states and 10 arrows.
The chart does not draw 5 steps that stay in one chart state.

## NoTrustDowngrade breaks
The shortest path has 2 steps:
0. Artifact and carrier presented
1. operator selects strict verification: Strict policy selected
2. caller loses the required verification policy: Strict policy silently downgraded

## NoUntrustedExecution breaks
The shortest path has 5 steps:
0. Artifact and carrier presented
1. operator selects strict verification: Strict policy selected
2. caller loses the required verification policy: Strict policy silently downgraded
3. unchecked artifact is materialized after policy loss: Materialization admitted
4. admitted materialization receives a lease: Materialization leased
5. process becomes permitted: Process permitted under lease

## Rules that hold
NoDeadlock
