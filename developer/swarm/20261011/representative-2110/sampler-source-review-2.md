# Revised bounded sampler source review

**Source verdict: GO for the bounded generic-sampler controlled probe, conditional on the separate exact-source admission binding. This is not run approval or measurement evidence.** No RCC command, test, build, or benchmark was launched. Both Python sources passed in-memory `compile()` syntax checks only.

Reviewed inputs:

- Revised `run_bounded_v4.py` SHA-256: `3c953181a2ac08d0bd15d24d4504d87ce1954ea86136c39131e100eddee9194a`
- Unchanged `controller.py` SHA-256: `38304141264d61e875bdd5bd6225dc3ba637b09c780cea48554cfc17a497f48e`
- Prior pre-review runner preserved separately; this review does not overwrite `sampler-source-review.md`.

The v4 changes address the source-level blockers from review 1: it counts/records concurrent `FileNotFoundError` during optional scratch scans while propagating other sampler failures into the failure-and-cleanup path; after the direct child exits it scans `/proc` for all remaining processes in the launcher's private session (including RCC children that changed process group); it signals each observed PID through a pidfd after verifying the recorded start tick; and it performs final output and scratch-size scans before writing the result receipt. This closes the earlier gap where a between-poll child or post-sample output/scratch growth could silently pass. The exact-PID/start signal prevents a recycled PID from being signaled.

The controller's `resume-measure` branch checks the failed pair-2-resident invocation and both previously accepted pair-1 receipts, imports pair 1 without rerunning it, and schedules pair 2/3 only. A failed invocation is appended before its nonzero result is asserted; an accepted row is appended only after all acceptance checks. The root's stated separate resume admission must bind the reviewed v4 SHA and unchanged controller SHA before a probe is launched; the controller's generic `verify_binding()` does not itself pin `run_bounded_v4.py` to an expected digest.

Scope remains deliberately limited: sampled RSS is not a hard memory quota or absolute peak; disk-free observation and scratch scans are not global quotas; a process that deliberately creates a new session can escape the private-session census. This is suitable only for the declared trusted cooperative RCC worker lifecycle, not hostile-code containment. If the final census discovers a remaining session member, the runner records failure even if cleanup later removes it; that conservative behavior is appropriate for admission.

The older `sampler-source-review.md` NO-GO referred to runner SHA `a23d47e...` and stale prior admission state; it remains valid historical evidence and is superseded for the revised runner source verdict only. No claims about a successful probe, resume admission, or workload measurement are made here.
