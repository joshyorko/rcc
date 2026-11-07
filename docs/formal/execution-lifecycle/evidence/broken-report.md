# ExecutionLifecycle

TLC reached 18 states and 16 steps. The chart draws 7 states and 8 arrows.
The chart does not draw 1 steps that stay in one chart state.

## NoTrustDowngrade breaks
The shortest path has 2 steps:
0. Artifact and carrier presented
1. operator selects strict verification: Strict policy selected
2. unsigned metadata attempts to lower strict policy: Explicit unsigned-local opt-in

## Rules that hold
NoUntrustedExecution
