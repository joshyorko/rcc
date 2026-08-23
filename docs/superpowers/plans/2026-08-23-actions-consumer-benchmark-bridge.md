# Actions Consumer Benchmark Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Break the RCC #120/#125 and Actions #134 dependency cycle with a bounded real Actions consumer proof pinned to an exact RCC candidate, then use that workload to decide RCC storage and materialization defaults.

**Architecture:** RCC remains the lifecycle authority and Actions remains a consumer. The temporary Actions lane preserves its current v2 package and preload-worker process architecture while replacing persisted interpreter/path authority with RCC Artifact identity and typed lifecycle receipts. Measurements return to RCC #125; any missing general-purpose lifecycle primitive is implemented in RCC, never hidden in the Actions adapter.

**Tech Stack:** Go 1.26, RCC Environment Artifacts, Python/Poetry, Actions Runtime v2, GitHub Actions, Linux filesystem benchmarks.

**Spec:** User-directed cross-repository handoff recorded in the active RCC program task.

## Global Constraints

- Pin every Actions proof to an exact pushed RCC candidate SHA and record the resulting Actions SHA.
- Keep no more than three writer lanes; root is the only integration writer.
- Do not merge PR #119, promote, tag, release, freeze Actions #143, or begin RCC #185 work.
- Do not persist `PYTHON_EXE`, `CONDA_PREFIX`, Holotree paths, or materialization paths as consumer authority.
- Preserve leases through child lifetime, mutation isolation, provider-dead warm reuse, and provider-backed clean acquisition.
- Benchmark carrier/index behavior separately from physical storage and materialization.
- Push coherent checkpoints normally; never rebase, squash, force-push, or retag.

---

### Task 1: Restore the RCC Candidate Gate

**Files:**
- Modify: `.github/workflows/coverage-gate.yml`
- Modify: `.github/workflows/quality-report.yml`
- Modify only Go files reported by the exact PR-base lint command.

**Interfaces:**
- Preserve mandatory real-Bubblewrap confinement tests in Coverage and Quality.
- Produce a lint-clean exact candidate relative to the PR base SHA.

- [ ] Reproduce remote failures from runs `32670483542`, `32670483548`, and `32670483609`.
- [ ] Install Bubblewrap in the two Linux jobs that run the mandatory confinement tests.
- [ ] Repair only new-revision lint findings, preserving material cleanup and release errors.
- [ ] Run exact-base lint, focused tests, contained `unitTests`, YAML validation, and `git diff --check`.
- [ ] Commit, merge with preserved history, push, and read back the remote SHA and checks.

### Task 2: Run the Bounded Actions #134 Consumer Vertical

**Files:**
- Modify only the existing Actions Runtime environment/preload integration and its focused tests.
- Update the Actions repository's canonical consumer guidance in the same commit.

**Interfaces:**
- Consume RCC publish/acquire/verify/materialize/lease/exec receipts by Artifact identity.
- Preserve one long-lived preload generation for repeated warm Action calls.
- Emit phase timings and provider-operation counts suitable for RCC #125.

- [ ] Write failing tests that reject persisted interpreter/path authority and require RCC Artifact identity.
- [ ] Verify one real Action execution and repeated warm process reuse through the pinned RCC binary.
- [ ] Prove source-only reload performs zero publish/acquire/provider work.
- [ ] Prove environment-changing reload creates a new generation where practical.
- [ ] Prove provider-backed clean acquisition and verified provider-dead warm execution.
- [ ] Commit and push the bounded Actions checkpoint with exact RCC and Actions SHAs.

### Task 3: Route General Consumer Gaps Back to RCC

**Files:**
- Modify the narrow RCC lifecycle/CLI package and focused tests only when the Actions proof demonstrates a general missing primitive.

**Interfaces:**
- Keep the primitive consumer-neutral and receipt-driven.
- Preserve stream-safe execution, child-lifetime leases, cancellation, atomic receipts, and platform fallbacks.

- [ ] Capture the failing real-consumer case before changing RCC.
- [ ] Add the smallest failing RCC regression test for the missing contract.
- [ ] Implement and verify the general primitive in RCC.
- [ ] Push the RCC checkpoint, repin Actions, and rerun the exact failed consumer proof.

### Task 4: Complete the Evidence-Backed #125 Decision

**Files:**
- Extend: `developer/benchmarks/`
- Add machine-readable raw evidence under the existing benchmark result contract.
- Update the canonical #125 decision document.

**Interfaces:**
- Attribute time to compression, object-open/syscall churn, destination materialization, relocation, Python/preload startup/import, and request-path materialization.
- State storage encoding, object layout, Linux/macOS/Windows materializers, universal fallback, and carrier recommendation.

- [ ] Capture cold, warm, source-reload, environment-reload, clean-consumer, and provider-dead Actions traces.
- [ ] Benchmark gzip object-per-file, zstd dual-read, hybrid zstd packfiles, reflink/clonefile, lazy/FUSE, compressed image plus CoW, and prewarm/process reuse against that workload.
- [ ] Allow platform/filesystem-specific winners only when exact evidence supports them.
- [ ] If no named candidate clears the material gate, profile the remaining bottleneck and benchmark at least one evidence-backed alternative where practical.
- [ ] Commit raw evidence and the ranked decision, then push the exact RCC SHA.

### Task 5: Resume the Release Graph

**Files:**
- Update issue/PR receipts and release metadata only after all acceptance evidence exists.

**Interfaces:**
- Produce an exact-SHA all-PASS #121-#127 ledger before #120 promotion.
- Return to Actions #134 as the primary next program only after the RCC handoff is complete.

- [ ] Complete native compatibility and N-1/self-host receipts at the final RCC candidate.
- [ ] Run the full promotion gate and independent exact-SHA review.
- [ ] Only then complete PR #119 merge, RC/stable publication, live asset/Homebrew verification, and issue receipts.
