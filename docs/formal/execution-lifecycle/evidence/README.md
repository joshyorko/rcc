# Strict admission model evidence

This finite TLA+ model checks only trust-policy downgrade and process admission. It has two scenarios: deployment-owned carrier + valid artifact signature + authenticated metadata, and attacker-controlled carrier + unsigned artifact + unsigned metadata. It does not model arbitrary cryptography, carrier compromise, revocation-list freshness/rollback, platform differences, process supervision, output durability, or executable search.

## Results

- Fixed config: TLC 1.7.4 / Temurin 25; 17 distinct states; `NoTrustDowngrade` and `NoUntrustedExecution` hold. See `fixed-report.md`, `fixed-chart.mmd`, and `fixed-tlc.txt`.
- Broken config: same checker; `NoTrustDowngrade` fails in the shortest 2-step path: select strict policy, then lose that context at the caller boundary. `NoUntrustedExecution` fails after 5 steps: policy loss, unchecked materialization, lease creation and process admission. See `broken-report.md`, `downgrade-counterexample.mmd`, and `broken-tlc.txt`. Statechart explores beyond the first violation; its graph counts differ from the first failing TLC run.

## Reproduction

From the repository root, run `python3 docs/formal/execution-lifecycle/check.py` for the pinned fixed/broken/restored check. Read every result with `/statechart`, using the fixed and broken configs and `view.mjs`. The installed pstack statechart skill pins TLC 1.7.4 and Temurin 25 through mise. It writes each run to a new scratch output directory outside the repository. The fixed run must pass both invariants; the intentionally broken run must identify `NoTrustDowngrade` and the two-step path above. Terminal stuttering and steps within a chart label are not drawn. The model freezes evidence across acquisition and lease; changed-carrier refresh is a separate real-code test gate.

## Code boundary

The model contract is compared with combined source commit `96151d5406c3c543fd66ea8849d0a5bf2e045574`:

- `environmentlifecycle/materialize.go`, `Acquirer.acquireLocked`: nil policy currently produces a valid receipt; otherwise the selected policy verifies before acquired content is returned (#252).
- `environmentlifecycle/lease.go`, `LocalMaterializer.Lease`: trust is refreshed and invalid receipts rejected before the durable lease is installed.
- `environmentlifecycle/execute.go`, `executeWithSignalsAndStreamsInDirectory`: lease and execution handle precede `process.Start`.
- `cmd/robotRunFromBundle.go`, `robotRunFromBundleCmd`: the bundle caller now supplies strict policy, retains lease refresh inputs, and rejects an explicitly strict source-only fallback.
- `cmd/robotBundleTrust.go`, `bundleTrustOptions.acquireWith` and `materializationForBundledArtifact`: deployment keys, carrier and selected policy reach acquisition and lease creation. Removing this caller context is the injected fault; the legacy nil-policy API remains available to other controlled callers.

The counterexample demonstrates a caller-policy omission fault, not a claim that
metadata mutates Go policy fields. The generated skill's source-policy regression
caught a real Go mutation forcing permissive mode; the source was restored and
the regression passed (`source-policy-mutation.txt`). Real baseline CLI replay
also demonstrated unsigned execution, but model success still does not prove Go
implementation behavior. Post-fix real CLI acceptance remains a separate gate.
Issue #253's detached metadata authentication and rollback exposure remains open;
the model explicitly assumes authenticated, current metadata and does not close
that gap.
