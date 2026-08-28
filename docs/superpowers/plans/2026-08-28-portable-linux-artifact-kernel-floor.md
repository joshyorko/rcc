# Portable Linux Environment Artifact Kernel Floor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent a Linux Environment Artifact built on a newer producer kernel from being rejected solely because the worker kernel is older, while retaining genuine compatibility gates.

**Architecture:** Linux artifact requirements will record an honest RCC-supported kernel floor rather than the builder's `uname -r`. The existing artifact compatibility evaluator remains the enforcement point for architecture, libc, required libraries, CPU features, filesystem, and kernel-floor checks. Tests will use explicit producer/worker capability fixtures and the existing lifecycle path to prove rejection occurs before object fetch for real incompatibilities.

**Tech Stack:** Go, RCC Environment Artifact v1 compatibility model, focused `go test`, contained RCC toolkit.

**Spec:** User-provided Linux Environment Artifact portability objective in the task request.

## Global Constraints

- Preserve Linux amd64 architecture, glibc/native-library, CPU-feature, filesystem, and relocation checks.
- Do not disable compatibility checks or use `permissive-local`.
- Do not change Josh Room or JAT.
- Do not rebuild or publish a JAT artifact, merge, or release.
- Start from `origin/main`, preserve unrelated work, and report source/build/runtime/publish status separately.

### Task 1: Prove the Linux builder-kernel false rejection

**Files:**
- Modify: `environmentartifact/compatibility_test.go`
- Test: `environmentartifact/compatibility_test.go`

- [x] **Step 1: Write the failing producer-newer/worker-older test**

Add a test with a valid Linux amd64 requirement fixture using the corrected Linux family identity and `3.15` kernel floor, and a valid worker fixture representing the producer's `7.1.8` build and the supported older `5.14` worker. Add a lifecycle-generation assertion that fails while the producer-side function still copies `uname -r`.

- [x] **Step 2: Run the focused test and verify RED**

Run the focused lifecycle/artifact tests inside the contained toolkit and confirm the lifecycle test fails because the current requirement still requires the builder kernel, while the explicit evaluator floor tests establish the intended fail-closed behavior.

### Task 2: Implement the smallest honest Linux requirement fix

**Files:**
- Modify: `environmentlifecycle/compatibility_linux.go`
- Modify: `environmentlifecycle/compatibility_linux_test.go`
- Modify: `environmentartifact/compatibility_test.go`

- [x] **Step 1: Add a Linux requirement contract assertion**

Assert that materialization requirements use the stable Linux OS identity and an RCC-supported kernel floor, not the exact `uname -r` observed during build. Keep libc probing, ELF imported-library probing, amd64 SSE2, and native architecture unchanged.

- [x] **Step 2: Implement the fixed Linux floor**

Introduce the named Linux kernel floor constant `3.15`, derived from RCC's unconditional `renameat2(RENAME_NOREPLACE)` publication path, and use it for `KernelMinimum`. Set Linux `MinimumVersion` to the stable Linux OS family identity `1` rather than copying `uname -r`. Do not weaken the evaluator or alter non-Linux platform files.

- [x] **Step 3: Run focused GREEN tests**

Run `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact ./environmentlifecycle -count=1` and verify the regression passes while libc, CPU, and filesystem incompatibility tests still reject.

### Task 3: Document compatibility semantics and run broader verification

**Files:**
- Modify: `docs/environment-artifact-compatibility.md`

- [x] **Step 1: Document Linux OS/kernel semantics**

Explain that Linux `minimumVersion` is not the builder kernel and `kernelMinimum` is an honest RCC runtime floor; producer `uname -r` must not become a blanket worker minimum. State that architecture, libc, required native libraries, CPU features, and filesystem requirements remain fail-closed.

- [ ] **Step 2: Run verification**

Run `GOARCH=amd64 CGO_ENABLED=0 go test ./environmentartifact ./environmentlifecycle -count=1`, `rcc run -r developer/toolkit.yaml --dev -t unitTests`, `git diff --check`, and (if available without changing host state) the repository's relevant Robot acceptance task. Record skipped/unavailable gates explicitly.

### Task 4: Publish the draft PR without merge/release

**Files:**
- No additional source files.

- [ ] **Step 1: Inspect the exact diff and commit**

Review `git diff --stat`, `git diff --check`, and the focused test output, then commit with `fix(environmentartifact): use portable Linux kernel compatibility floor`.

- [ ] **Step 2: Push and open a draft PR**

Push branch `fix/portable-linux-artifact-kernel-floor` and open a draft PR targeting `main`, including the root cause, RED/GREEN evidence, compatibility rationale, and downstream requirement to rebuild/re-publish the JAT artifact after this RCC fix. Do not merge, tag, or release.
