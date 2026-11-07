# ExecutionLifecycle

TLC reached 18 states and 21 steps. The chart draws 9 states and 9 arrows.
The chart does not draw 5 steps that stay in one chart state.

## NoTrustDowngrade breaks
The shortest path has 2 steps:
0. Artifact and carrier presented
1. operator selects strict verification: Strict policy selected
2. unsigned metadata attempts to lower strict policy: Strict policy silently downgraded

## Rules that hold
NoUntrustedExecution, NoDeadlock
