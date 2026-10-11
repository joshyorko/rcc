# RCC acceptance checkpoint

Snapshot: **2026-10-11 01:58:27 UTC** (read-only public GitHub refresh; root receipts through 01:57 UTC). This is a durable evidence handoff, not release approval.

- Main: `07db6221a3540d227f2106eccf69fe0e7c11fee0`, tree `04338ee249398525abcf9ef69b79ee1a072e2c10`.
- PR #233: open draft, published head `324524fffa4bd0f76c618f9fbb6ede30e733b2ff`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`, frozen freeze `codex/rcc-integrated-candidate-20261011-hosted-gate`.
- Live inventory: **10 open issues, 3 open PRs, 67 branches, 33 tags, 27 releases, 0 milestones**. #208 is closed after #231 acceptance; #98 is open again. Latest stable/release remains v18.19.5.

The current PR workflow is active: Rcc run **38103534317** is in progress; Coverage gate **38103534344** has passed; CodeQL **38103534328**, PR metrics **38103534368**, Quality report **38103534370**, and Lint **38103534384** passed. The contained full `ReleaseCandidateVerification` gate **114364186629** is running, Build **114364187148** passed. No current 11-gate result is available yet.

Root verified the published source object and that its prospective merge tree against current main is exactly `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`. The new opt-in workflow and validator changes passed 23 focused topology tests plus actual embedded-pipeline controls with exit codes 0, 7, and 10 preserved; receipt-bound metadata outside the checkout passed positive and negative controls. Those checks validate the workflow implementation, not completion of the active hosted release gate.

Native Linux/macOS-arm/Windows receipts were completed on parent 2de; macOS Intel was pending there. They are not current 324 native receipts. Current-head cross-platform acceptance remains pending. The previous 097 local full-gate attempt exited 1 after eight gates passed: Robot 171 PASS / 0 FAIL / 1 expected Windows skip; selfHost failed because `GOTOOLCHAIN=local` leaked into the Conda Go 1.26.5 bootstrap; goVet and coordination did not run. The 2de local wrapper never launched because preflight disk space was 6.82 GB, below its 10 GB minimum. Neither result is a 324 pass.

Strict-consumer and four-target default-tag security evidence remains bound to ancestor 097, not a fresh 324 run. The strict proof passed four numeric commands and rehashed 5,049 CAS files against 5,045 indexed objects with no mismatches; the refreshed empty revocation fixture was unsigned. The security scan had no call-traced affected-symbol findings, while GO-2026-5970 package-level and GO-2026-6629 module-level findings remain. A 17-command provider probe likewise remains bound to 097 and documents the named-profile trust-carrier gap in #118/#125.

The immutable Actions adapter at `a70993fafc99a9f94041485542d5a720797ca394` accepts exactly RCC v18.19.3 in its opt-in check; stable v18.19.5 and candidate v18.19.6 are rejected. This is an existing consumer constraint, not a candidate regression. No Actions repository changes, release tag, publisher signing, notarization, or release approval are claimed.

See [inventory and dependency graph](inventory.md) for current issue/PR status and source-bound receipt hashes, and [restart prompt](restart-prompt.md) for active gates and safe handoff boundaries. This checkpoint preserves reviewable evidence and the remaining integration gates.
