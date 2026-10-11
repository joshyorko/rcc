# Bounded runner/controller source review

Review scope: source only. No RCC command, test, Go build, or benchmark was launched. Both reviewed Python files passed in-memory `compile()` syntax checks without import or execution.

Reviewed immutable inputs:

- `run_bounded_v4.py` SHA-256 `a23d47e29e89189649fea75558f9ef4e065bde2abb321c32dfccb46642081c3a`
- `controller.py` SHA-256 `38304141264d61e875bdd5bd6225dc3ba637b09c780cea48554cfc17a497f48e`
- `native-admission.json` was read-only inspected during this review.

## Findings

**NO-GO for resuming measurement with the present source/admission binding.** The controller calls `verify_binding()` before each invocation and requires `native-admission.json.controller_sha256 == sha(controller.py)`. The inspected admission records `1bcc1c0f0b2e403fcd9e7bcb45ecab954a5149cf14c9e36ac849daf7cf785884`, while the reviewed controller hashes to `38304141264d61e875bdd5bd6225dc3ba637b09c780cea48554cfc17a497f48e`. The same admission allows only `controls` and `measure`; it omits the controller's `resume-measure` phase. It also has a phase-gate sentence saying measure remains prohibited, contrary to `allowed_phases` and the review's authorization booleans. The controller itself does correctly retain pair 1 and schedule only pair 2/3 on its resume branch, after checking pair 1's accepted receipts and pair-2-resident's failed invocation. Rebind the exact controller/admission and explicitly authorize only the intended resume phase before any continuation; do not use ordinary `measure`, which schedules pair 1 again.

**NO-GO on hard descendant cleanup in the generic bounded launcher.** It creates a new session, but termination targets the direct child plus identities already observed in the 100 ms polling census. After child exit, it treats only `known` PID/start identities as owned survivors. A long-lived same-PGID child created between polls, or otherwise not yet inserted in `known`, can therefore survive unnoticed. The receipt scope admits sampling gaps, but the runner does not prove full process-group cleanup. For this campaign, add final PGID census/cleanup or otherwise require an independent controller census before acceptance; preserve a failure if cleanup was required.

**Resource ceilings are sampled, so add final checks before claiming enforced caps.** The 8 MiB output and optional 128 MiB scratch logical-size checks happen only while the child is still running. A child that exits between samples can leave output/scratch above the stated threshold without setting `reason`. The receipt correctly labels these as sampled, so do not describe them as hard limits. A post-wait output size and scratch scan should set a failing stop reason when a ceiling was exceeded. Scratch traversal now correctly catches only `FileNotFoundError` and records vanished entries; other sampler exceptions set a failure reason and proceed into cleanup/wait.

## Positive controls

- The controller's `resume-measure` branch does not schedule the accepted pair-1 arms again. It verifies both retained receipts and their 90 passing tests before scheduling pair 2 and pair 3.
- `run()` appends the invocation result before asserting nonzero outer status, preserving the observed first-failure receipt. An accepted receipt is appended only after all run checks pass.
- The controller records pre/post directory guards around successful command return and keeps failure rows distinct from `accepted` rows.
- V4's narrow concurrent-delete handling in scratch traversal is scoped to `FileNotFoundError`; other filesystem errors propagate to the sampler error path.

These are source-review observations, not run evidence or admission to execute. The present resume phase remains blocked on exact re-binding/admission and the process-group cleanup gap.
