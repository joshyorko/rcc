# Materialization

TLC reached 20 states and 32 steps. The chart draws 9 states and 14 arrows.
The chart does not draw 10 steps that stay in one chart state.

## ReadyOnlyAfterValidation breaks
The shortest path has 3 steps:
0. Artifact requested
1. verify artifact content: Content verified
2. begin restoration: Restoring environment
3. publish readiness: Ready published before validation

## Rules that hold
NoDeadlock
