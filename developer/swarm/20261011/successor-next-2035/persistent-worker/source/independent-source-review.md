# Persistent-worker pre-execution source review

Review scope: read-only review of the frozen harness and controller before native execution; reviewer did not build or launch either. Original RCC/Actions admission failures remain in force.

## Source binding

- `main.go`: SHA-256 `8b3f059965f93f9bb80e04e5ffb8caf1c059d395e3bee3b1efcb3d9eda410c9c`.
- `run_persistent_experiment.py`: SHA-256 `4cf4c69cbfe6570f7c4a7b20014e850b20028e461b4bb7debe18f8e9367de574`.
- Source-only admission: **GO, conditional on successful build/binding for this exact source and controller hash, and on the described guards succeeding before any measured arm.** This permits only the finite private RCC lifecycle diagnostic below.

## Scope and positive checks

The two arms call the real public `environmentlifecycle.ExecuteWithStreams` API. Each uses a newly created consumer home with absent CAS and cold provider acquisition; acquisition is reported separately and excluded from the symmetric sum of API-call durations. The fresh arm performs six Execute calls and the resident arm submits six newline-delimited requests to one Python child, then closes stdin. Both verify the same accepted gzip-1 fixture payload: Python re-reads and hashes all 512 files for every request; Go validates sequence, count, aggregate digest, PID cardinality, and response lease ID against the API handle. The controller checks six calls/six distinct PIDs versus one call/one PID, lease-ID cardinality, no remaining lease JSON, and identical source closure before/after.

A real `SystemExit(73)` control checks the nonzero-child path and lease cleanup before timed arms. Failure receipts retain parseable partial rows where available. Both Go and Python perform strict mode-0700, owner/group checks on the outer root and per-arm home before and after each worker/control. The controller also guards the result root, fixture root/provider roots, producer home, worker roots, and precreated mode-0750 global HT path. It binds the binary to frozen source and launcher/overlay hashes and requires the matching successful build receipt. A 25-second API context sits inside the existing 30-second bounded process-tree runner; the controller timeout is 40 seconds. The runner retains stop reason and process identities, terminates observed descendants, and explicitly disclaims sub-100ms/escaped-child and hard-quota coverage.

## Limits and non-blocking observations

This measures a six-request Python batch on a synthetic accepted fixture with uncontrolled warm OS cache. It is not a persistent daemon, Actions/solver/customer workload, release qualification, or evidence that cold acquisition improves. CPU profiles/counters cover the post-acquire interval; Go CPU counters are sampled after profile stop and sync, so include small profile-flush overhead. The controller's 40-second `subprocess.run` timeout exception path would exit before its post-guards and receipt; the inner bounded runner is intended to return by 30 seconds, but timeout handling could be made more fail-closed later. The worker's error receipt is best-effort if the process itself is killed before deferred cleanup.

No source blocker remains for this narrowly bounded experiment. Execution results must still be independently checked; a build, a clean runner receipt, or a passing synthetic run does not convert the original RCC 7945/5a or Actions 727/be55 admission failures into acceptance evidence.
