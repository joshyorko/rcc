# MetadataDistribution

TLC reached 29 states and 39 steps. The chart draws 6 states and 8 arrows.
The chart does not draw 11 steps that stay in one chart state.

## DistributedOnlyAfterRetention breaks
The shortest path has 4 steps:
0. Snapshot supplied
1. verify independent authority: Authority checked
2. skip checkpoint retention [injected model fault]: Checkpoint skipped
3. write detached carrier: Carrier written, awaiting readback
4. confirm matching authenticated envelope: Published without retained checkpoint

## Rules that hold
NoDeadlock
