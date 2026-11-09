---
name: deadcode-review
description: Turn RCC deadcode evidence or a selected Go symbol into a bounded simplification brief.
---

# Deadcode simplification review

Run `rcc run -r developer/toolkit.yaml --dev -t deadcode -- --review` to collect the pinned analyzer evidence. The report is a starting point, not a deletion list. Re-run it when HEAD or the working tree changes. A failed target is unknown; never infer that its code is unused.

Review selected functions and packages even when reachable. Read definitions and callers, inspect interfaces, exports, generated/platform files, and compare active alternatives. Separate application reachability from test-only references. Group an abandoned path when its functions are related, and identify shared helpers that remain needed.

For every brief include:

- What the code does, with exact file and symbol references.
- Observed evidence, separate from interpretation.
- The simpler alternative and why it helps, if one exists.
- Decision: retain, remove, reuse/consolidate, simplify in place, repair/finish, or investigate.
- Benefit, affected callers, behavior to preserve, and unresolved questions.
- Smallest proposed change and exact toolkit verification steps.

Do not use confidence percentages. Do not call work ready when compatibility or correctness questions remain. Keep the workflow read-only: no edits, issue creation, or implementation delegation. Follow Ponytail's ladder: verify need, reuse existing code, prefer the standard library, then make the smallest contract-preserving change.

Judge these real examples from source rather than hardcoding conclusions: Windows-only `shell.Task.StderrOnly`; `operations/zipper.go`'s `loopExecutor` and `unzipper.Explode` (including ignored `Execute` result); `wizard/common.go:memberValidation` versus `wizard/create.go:choose`; `pretty/internal.go:csif` versus `csi`; and test-only `buildcoord.ArtifactVerifierFunc` adapters. Missing platform evidence, failed analysis, or stale snapshots block removal recommendations. Static cross-target analysis does not establish native execution.
