# BundleFlow

TLC reached 17 states and 23 steps. The chart draws 13 states and 14 arrows.
The view hides 6 steps, so the chart does not draw them.

## NoFalseSuccess breaks
The shortest path has 4 steps:
0. Bundle presented
1. import environment and extract task: Environment and task prepared
2. run selected task: Task running
3. child exits successfully: Task succeeded; outputs open
4. fault: return success before finalization: Success reported before outputs saved

## Rules that hold
NoDeadlock
