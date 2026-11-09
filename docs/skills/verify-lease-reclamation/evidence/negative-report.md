# LeaseReclamation

TLC reached 20 states and 36 steps. The chart draws 9 states and 14 arrows.
The chart does not draw 8 steps that stay in one chart state.
The view hides 7 steps, so the chart does not draw them.

## NoProtectedDeletion breaks
The shortest path has 2 steps:
0. Ready without lease
1. execution obtains a lease: Active owner protects environment
2. collection removes an eligible environment: Environment removed; ready record remains

## Rules that hold
NoDeadlock
