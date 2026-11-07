# Strict admission model evidence

This finite TLA+ model checks only trust-policy downgrade and process admission. It has two scenarios: deployment-owned carrier + valid artifact signature + authenticated metadata, and attacker-controlled carrier + unsigned artifact + unsigned metadata. It does not model arbitrary cryptography, carrier compromise, revocation-list freshness/rollback, platform differences, process supervision, output durability, or executable search.

## Results

- Fixed config: TLC 1.7.4 / Temurin 25; 17 distinct states; `NoTrustDowngrade` and `NoUntrustedExecution` hold. See `fixed-report.md`, `fixed-chart.mmd`, and `fixed-tlc.txt`.
- Broken config: same checker; `NoTrustDowngrade` fails in the shortest 2-step path: select strict policy, then unsigned attacker metadata downgrades it to permissive. `NoUntrustedExecution` still holds because this fault alone does not grant explicit local opt-in. See `broken-report.md`, `downgrade-counterexample.mmd`, and `broken-tlc.txt`. Statechart explores beyond the first violation; its graph counts differ from the first failing TLC run.

## Reproduction

From the repository root, run `python3 docs/formal/execution-lifecycle/check.py` for the pinned fixed/broken/restored check. Read every result with `/statechart`, using the fixed and broken configs and `view.mjs`. The installed pstack statechart skill pins TLC 1.7.4 and Temurin 25 through mise. It writes each run to a new scratch output directory outside the repository. The fixed run must pass both invariants; the intentionally broken run must identify `NoTrustDowngrade` and the two-step path above. Terminal stuttering and steps within a chart label are not drawn. The model freezes evidence across acquisition and lease; changed-carrier refresh is a separate real-code test gate.

## Code boundary

The model contract is compared with source commit `f7960076843500d15835b476c07e60d7264027c1`:

- `environmentlifecycle/materialize.go`, `Acquirer.acquireLocked`: nil policy currently produces a valid receipt; otherwise the selected policy verifies before acquired content is returned (#252).
- `environmentlifecycle/lease.go`, `LocalMaterializer.Lease`: trust is refreshed and invalid receipts rejected before the durable lease is installed.
- `environmentlifecycle/execute.go`, `executeWithSignalsAndStreamsInDirectory`: lease and execution handle precede `process.Start`.
- `cmd/robotRunFromBundle.go`, `robotRunFromBundleCmd`: bundle acquisition currently supplies no trust policy (#252).

The counterexample demonstrates the model transition, not a reproduced Go runtime defect. Real-code regression tests and published-CLI runtime acceptance remain pending in the implementation lanes. Issue #253 additionally documents that policy-relevant detached metadata is not cryptographically authenticated and revocation snapshots lack rollback protection; the model explicitly assumes authenticated metadata and does not close that gap.
