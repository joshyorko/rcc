# ProcessExecution

TLC reached 53 states and 67 steps. The chart draws 10 states and 13 arrows.
The chart does not draw 2 steps that stay in one chart state.
The view hides 14 steps, so the chart does not draw them.

## LeaseUntilReaped breaks
The shortest path has 4 steps:
0. Execution requested
1. acquire execution lease: Lease held
2. resolve command and prepare supervisor: Command and supervisor prepared
3. start child: Child running under lease
4. fault: release lease before child reap: Child alive without lease

## Rules that hold
NoDeadlock
