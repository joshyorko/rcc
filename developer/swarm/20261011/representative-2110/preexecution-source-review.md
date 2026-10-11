# Representative workload source admission review

Read-only review; no source edits, build, tests, acquisition, or native workload execution were performed by this reviewer.

Frozen files reviewed:

- Go worker `source/main.go`, SHA-256 `2304eb87bae940e4469d1a3c7aabcd46fa35a8fce5eec339442bb1cc95c365a8`.
- Controller `controller.py`, SHA-256 `c95d4e107748b8aa96e03ecd454478698394a76383ad2eef7e7643592d9bf12c`.
- Task driver staged at `successor-next-2035/representative-2110/source/task_driver.py`, SHA-256 `ba0e12c464fda8faffa906c4e78c14da8a560292bc1fbe1c0ddc7d2f3fdef893`.
- Workload design says tracked source commit `7945f956b428359928908a9f42e36417b113dafd`, archive SHA-256 `70654af0d400a8e04cbacaa72a7f3af65315a7a8f92f9f40d110c59c217cc204`, and real maintained suites of 5 agent-doc tests plus 25 release-topology tests.

## Decision: NO-GO pending one deterministic protocol correction

The fresh arm cannot currently pass beyond its first task. `main.go` invokes six separate `ExecuteWithStreams` calls in fresh mode, passing each one request with sequence values 1 through 6. `task_driver.py` starts every child with `next_sequence = 1` and rejects a request whose sequence differs. Fresh calls for sequences 2–6 therefore emit protocol errors before executing their test suites. The root should fix this narrow mismatch, freeze and hash the changed driver, then rebind the source/build/admission receipts before any native execution. Keep strict ordering and job-parity validation for the resident six-request child.

## Controls and integrity checks reviewed

The Go worker binds response sequence/job/lease, counts, statuses and clean-state flags; it checks one lease is removed after each API call, verifies PID/lease cardinality (6 fresh versus 1 resident), rechecks strict 0700 ownership/mode for outer/input/task/home roots, and compares the source/provider hash before and after the arm. The controller runs error and cancellation controls before timing, requires a successful bounded-runner result with no sampled live identities, verifies exact main/driver binding, and pre/post checks owned 0700 roots plus global shared 0750. Cancellation emits flushed parent/child PID+start identities and lets the bounded runner clean the shared process group; it does not claim RCC itself kills descendant groups.

The driver runs the real unittest modules from the frozen source snapshot, requires exact 5/25 test counts, logs bounded output, uses one private task temp directory per request, restores the documented Python process state, removes modules loaded from the snapshot, and checks bytes and active lease identity before/after each task. The selected control driver writes `cancel-ready` then blocks, so the immediate-EOF concern from the earlier pre-freeze read is resolved. The workload description appropriately limits isolation to trusted cooperative code and excludes OS sandbox, full Actions/release qualification, and persistent-daemon claims.

Two non-blocking coverage details should remain explicit: the response checker does not independently reject an `UNEXPECTED_SUCCESS` outcome in `testIds` (the pinned selected suites currently have no expected-failure decorators); and the task driver's per-task source hash covers bytes while the Go arm-level closure hash and controller inventory also cover modes. The latter catches mode changes by arm end, but not before the next task. Do not elevate either into a broader adversarial-isolation claim.

No native timing or result is admitted by this source review. Previous strict-0700 failure receipts remain unchanged.

## Addendum after files moved into the review directory

The driver has since been copied into this directory without a content change; its SHA remains `ba0e12c464fda8faffa906c4e78c14da8a560292bc1fbe1c0ddc7d2f3fdef893`. The current Go and controller hashes are still `2304eb87bae940e4469d1a3c7aabcd46fa35a8fce5eec339442bb1cc95c365a8` and `c95d4e107748b8aa96e03ecd454478698394a76383ad2eef7e7643592d9bf12c`.

Additional NO-GO findings confirmed in the current exact bytes:

1. The worker launches Python with `driver input tasks lease artifact`, but the driver's required positional arguments are `mode input_root task_root lease_dir artifact`. The invocation must supply the proper `worker`/`control` mode and exact paths.
2. As noted above, the driver starts `next_sequence` at 1 for every child, while the fresh Go arm sends one request with sequence values 1 through 6. A validated start-sequence is needed for fresh children; resident mode must still require the ordered six-request stream.
3. `controller.py` reads the live gzip-1 `fixture.json`, but `verify_binding()` does not hash/pin that file, `prepared.json`, or the provider closure. The observed fixture bytes are SHA-256 `cd43bfd93be0863808cffa7aa76d64367239af1d866ded8218a2fe1dc5d62183`, `prepared.json` is `8f7777814ff36804d62c6a8e815355c254cc7205f40421650a55ca60b57b4957`, and the artifact digest is `sha256:0d77a90cd569b46d4e70ac8423584e802c5eb0141ef5a7ebceb23f67fc1c728d`. A new source-bound admission must establish the exact allowed fixture/provider identity.
4. Cancellation remains a required preflight control, not something this source review can waive: `ExecuteWithStreams` only terminates the direct child, while the control driver starts a sleeping grandchild. The outer bounded runner must demonstrate whole-process-group cleanup, both identities non-live, and no lease before timing. The current source/controller bytes do not by themselves prove that result.

Therefore do not run timed arms or label the workload admitted until all four points are corrected/proven and their exact source/build/fixture bindings are frozen. No native work was performed here.

## Follow-up read-only review of updated sources

The coordinator has since corrected the driver/CLI defects. Current source hashes are Go worker `f3bd699f0f5fccdda76252c1848a0a7d69e6c0f2c92563a76145583623577a04` and task driver `7271b67e95d5c3471b57471571689ce4bddc4c27569724166ca42d617ec6e042`. The Go call now passes `worker`/`control`, the four required paths, artifact digest, and request sequence. The driver initializes its expected sequence from validated `start_sequence`, bounds it to 1–6, requires control start 1, and continues to check job parity and request order. The two earlier deterministic protocol defects are resolved in these new bytes.

Admission is still **HOLD**. Current controller hash remains `c95d4e107748b8aa96e03ecd454478698394a76383ad2eef7e7643592d9bf12c`, and its binding checks still do not pin `fixture.json`, `prepared.json`, or the provider closure before reading the live fixture artifact field. The available build-1 binding is explicitly `COMPILE_ONLY_UNADMITTED_CLI_DEFECT` and binds the old Go source hash, so it cannot attest the updated files. Finally, the RCC Linux supervisor terminates only the direct child; the cancel control intentionally starts a sleeping grandchild. Wait for the coordinator's owned process-group overlay, a new exact source/build binding, and a retained cancellation negative-control receipt with both PID identities absent and lease removed before native admission.
