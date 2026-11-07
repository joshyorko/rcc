# Metadata admission evidence

TLC 1.7.4 checks `AuthenticatedMetadata.tla`. Fixed and restored configurations
explore 53 distinct states and pass `NoUntrustedExecution` and
`NoTrustDowngrade`. The statechart covers six readable states and 14 arrows;
four same-label transitions are not drawn. These are bounded safety properties,
not temporal/liveness guarantees or proofs of Go/Ed25519/filesystem correctness.

The authentication mutation skips the authority check: receive forged metadata,
skip authentication, retain it, permit execution (four transitions). The
rollback mutation drops the checkpoint on restart: retain sequence 2, restart,
replay sequence 1, permit execution (eight transitions). Generated broken charts
retain the bad states; their reports identify the hidden same-label steps.
`checker.log` retains both actual counterexamples and the restored success.

The model abstracts the process lock plus comparison/flush into one admission
step. Signatures are an unforgeable authentication predicate. The deployment
owns roots, scope, clock and retained state independently of the carrier. The
scope contains two sequence values, authenticated empty/revoked snapshots and
one forged snapshot; it does not prove cryptographic parsing, key rotation,
partial disk writes, concurrent Go scheduling, process supervision or outputs.
Storage failure cannot grant admission; successful commits survive process
restart. Recovery from host compromise, rollback of the trusted disk or loss
of the deployment floor is outside this assumption.

`falsify_metadata.py` also temporarily removed the actual Go signature guard
and rollback comparison, required the corresponding regressions to fail, and
restored the original bytes before rerunning successfully. The retained logs
and `mutations.json` record commands, exits and before/mutated source hashes.
This is independent falsification evidence, not extraction of Go into TLA+.

Runtime bindings:

- `artifacttrust/metadata.go:VerifyMetadata` authenticates the complete payload.
  `TestMetadataEnvelopeAuthenticatesAllPolicyInputs` detects the removed guard.
- `artifacttrust/metadata_state.go:MetadataState.Admit` compares and persists
  under the process lock. `TestStrictPolicyCommitsAuthenticatedRevocationBeforeDenying`
  detects the removed rollback guard; `TestMetadataStateIndependentProcesses`
  exercises concurrent processes against one retained journal.
- `cmd/environment_metadata_real_cli_test.go:TestMetadataSecurityRealCLI`
  repeats forgery and post-revocation replay through a built CLI, with one new
  process per decision and explicit machine-readable rejection codes.
- `environmentlifecycle/metadata_test.go:TestMetadataRevocationDeniesNewLeaseWithoutTerminatingRunningProcess`
  checks the lease boundary and unchanged running-process receipt, outside the
  model's coverage.

Rerun `python3 docs/formal/execution-lifecycle/check.py --model AuthenticatedMetadata`.
Read its result using the installed `$pstack:statechart` skill and the checked-in
view. For the Go mutation exercise, use an exclusively owned checkout with no
concurrent builds and run `python3 docs/formal/execution-lifecycle/falsify_metadata.py
--evidence /absolute/new/task-evidence-directory`. The destination must not exist.
