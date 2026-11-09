# ProviderRecovery

TLC reached 14 states and 19 steps. The chart draws 12 states and 14 arrows.
The chart does not draw 5 steps that stay in one chart state.

## OnlyCommittedRestoreVisible breaks
The shortest path has 5 steps:
0. Backup supplied
1. validate backup before publication: Archive and closure checked
2. append restore begin: Restore transaction opened
3. append transaction records: Restore records pending
4. process stops: Stopped before commit
5. replay pending records [injected model fault]: Uncommitted restore exposed

## Rules that hold
NoDeadlock
