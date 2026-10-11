# RCC continuation: finite worker reuse and retained evidence

This is the same ONE user-authorized successor, continuing after the remotely verified checkpoint `003af703f74fdfcd35bd62d4685bb8c1a69e60f8`. That checkpoint preserves the exact parent `2f6570c99d5ba98f26ceb7b74d6da69fb84f99e8` and its original handoff. All prior files, failed receipts, issue ownership, branches and shipping pins remain unchanged. Repository `joshyorko/rcc`, ID `1051101627`; the fresh 20:49 UTC GitHub read still has main `7d17122436584dcbfe9b8afddeb56d7c81a10af7`.

## New actual RCC workload comparison

The coordinator built and executed a new private Linux diagnostic from frozen RCC `7945f956b428359928908a9f42e36417b113dafd` with the already reviewed owned-global-path and private-home-parent overlays. Source SHA-256 is `8b3f059965f93f9bb80e04e5ffb8caf1c059d395e3bee3b1efcb3d9eda410c9c`; diagnostic binary SHA-256 is `6c058c78cd7ddf20dd3da6230e805ca043b72d8a784573f48f02c8c8955606ce`. This is a modified diagnostic, separate from the frozen shipping Linux binary. Exact tool, overlays, launcher and build inputs are bound in [build-binding.json](persistent-worker/build-binding.json).

Both arms reuse the accepted gzip-1 fixture `sha256:0d77a90cd569b46d4e70ac8423584e802c5eb0141ef5a7ebceb23f67fc1c728d`, preserving its producer/provider bytes. Each arm cold-acquires it through normal public `NewAcquirer` compatibility, CAS and materialization behavior into a fresh private consumer home. The measured six-request execution batch then uses public `ExecuteWithStreams`: six new Python processes versus one finite resident Python process consuming six JSONL requests until EOF. Every request independently reads, size-checks and SHA-256 verifies all 512 payload files. No per-request Acquire was inserted into the fresh baseline. No persistent service or infrastructure was created.

Three counterbalanced pairs passed, following a new untimed exit-73 child/lease-cleanup control. The raw schedule, guards, source closure, all six worker receipts and supervisor outcomes are in [results](persistent-worker/results/controller.jsonl).

| Measured scope | Six fresh children | One resident child / six requests |
| --- | ---: | ---: |
| Median sum of ExecuteWithStreams durations | 643.637 ms | 134.099 ms |
| Batch samples | 821.470, 643.637, 627.534 ms | 134.099, 195.316, 126.506 ms |
| Median separate cold Acquire | 1,691.965 ms | 1,668.897 ms |
| Median provider setup + cold Acquire + execution batch | 2,338.636 ms | 1,796.746 ms |
| Median waited-child CPU | 601.454 ms | 126.287 ms |

Paired batch reductions were 687.371, 448.321 and 501.028 ms. The benefit is observed for this finite synthetic batch, including normal RCC execution and lease behavior. It does not qualify an Actions workflow, long-lived daemon, solver workload, self-contained Python distribution, arbitrary customer workload or shipping candidate. OS caches were warm/uncontrolled; there are only three pairs. Go CPU profiles exclude Python-child CPU; two resident profiles have zero samples and the third only 10 ms, so no Go hot-path conclusion is drawn from this short batch. Resource counters include output validation, lease checks and profile stop/flush; the narrower batch timer excludes that orchestration. RSS is sampled or a post-batch snapshot, never an absolute peak or hard quota.

The coordinator independently read back seven actual materializations (control plus six arms), checking all 3,591 logical files with zero mismatches, 36 request outputs representing 18,432 payload verifications, six distinct PIDs/lease IDs in each fresh arm, one PID/lease ID in each resident arm, and zero remaining leases. All private boundaries/homes remained owned 0700, the isolated shared-global directory remained 0750, source/provider bytes stayed unchanged, all seven supervisors returned zero with no stop reason and no observed live identities. [coordinator-verification.json](coordinator-verification.json) and the independent runtime review carry the raw verification scope. Sampling does not prove absence of sub-100-ms or otherwise unobserved escaped processes.

Independent source review initially rejected a cached-work bug: the resident child had hashed the workload once and reused its answer. That source was never benchmarked. The reviewed revision hashes all 512 files on every request. A subsequent bounded build failed on an unused import; exact failed source `82d9f176…`, stderr and exit 1 remain under `build-1`. The minimal import/diagnostic-text correction was reviewed, bound and compiled in `build-2` before native admission. The source preparation README/comment describes the earlier worker handoff; the coordinator's later build/runtime receipts establish the actual executed status. No failed receipt was relabeled.

## Six #125 directions and lifecycle barriers

| Direction | Current evidence and dependency |
| --- | --- |
| Compression | Retained earlier six RCC pipeline arms: gzip-6 stored 38,746 bytes versus gzip-1 46,058, a 15.88% reduction. Cold medians 1.733 versus 1.519 s and mixed paired latency; no reliable latency winner. Producer compression cost and Actions acceptance remain unmeasured. |
| Small-object packs | Retained earlier real RCC acquisition/materialization/execution six arms and twelve integrity controls. Cold medians 1.610 s filesystem versus 1.643 s pack, mixed pairs; no gain accepted. Production secure traversal/stat/allocation behavior was exercised by this provider. |
| Prewarming / worker reuse | Earlier real warm local Acquire + fresh execution passed at 0.221/0.210 s, with output parity and clean leases. The new finite resident-child comparison above adds actual worker reuse; Actions/long-lived-worker lifecycle remains separate. |
| Reflink / clonefile | Two owned 64-KiB FICLONE probes on the workspace overlay and tmpfs returned ENOTSUP (95); verified ordinary-copy fallback passed. Linux has no clonefile interface. This is not a universal no-go for another existing clone-capable host/filesystem. |
| Lazy / FUSE | Kernel FUSE registration exists, but `/dev/fuse` is absent for the inspected mount route. Genuine lazy mounted execution and late-fault lifecycle/integrity are not implemented or qualified. |
| Compressed image / COW | No inspected loop devices or SquashFS/EROFS tools. The existing `bwrap` namespace route was actually probed under a five-second bound and failed with `bwrap: setting up uid map: Read-only file system`, exit 1. No privileged retry or infrastructure provisioning. Existing RCC ZIP import/extraction is eager ordinary CAS/materialization, not a mounted image/COW. |

The public Acquirer currently hardwires LocalMaterializer. Its private constructor fields are a source integration seam, not by themselves a universal capability barrier. A private overlay could inject a backend, but no genuine backend on the inspected host passed admission. The source-only feasibility report is [compressed-image-cow-feasibility.md](compressed-image-cow-feasibility.md); the later actual namespace result is [namespace-probe/stderr](namespace-probe/stderr).

Read-only reports from the accepted CPU profiles show Linux Syscall6 at 65.31–73.81% flat and real Acquire at 70.21–88.37% cumulative. This supports investigating repeated CAS path traversal, not deleting verification. A proposed per-Acquire FD session remains unimplemented and unadmitted: an end-only root inode check is too late after a visible manifest/install commit, and cached FDs change ancestor/shard replacement semantics. See [coordinator-admission.md](local-hotpath-review/coordinator-admission.md). Compatibility preflight and materialized Python ABI validation remain distinct checks.

## Actions coordination and publication constraints

Existing repository-channel posts were actually created and independently fetched with exact matching bodies: RCC [#125](https://github.com/joshyorko/rcc/issues/125#issuecomment-6113444261), RCC [#132](https://github.com/joshyorko/rcc/issues/132#issuecomment-6113444452), Actions [#133](https://github.com/joshyorko/actions/issues/133#issuecomment-6113444599). POST/GET receipts are in this directory. These are receipt-backed repository comments, not a delivered message to the external root; the historical direct-steering NOT_FOUND remains true.

The existing Actions owner `01a128d1-75e2-71fe-9528-54b0163600dc` keeps #101/#133/#134/#139/#143 and PRs #221/#308. Fresh heads remain #221 `e2c81adce7858d5a952991ba9a8b7f72e902038a` and #308 `8d49d0865ad5ad27b2d4e325ec135212080625e8`; both drafts. The preceding refreshed status was 34 success/one skip for #221 and 25 success/one Windows Python 3.12 dev-mode failure for #308. New comment readback contains no later owner runtime admission. Source-bound Actions fixtures/admission remain retained in the parent checkpoint; they do not constitute raw end-to-end qualification. The ordered chain remains baseline 18.19.3 → released 18.19.5 → exact candidate `727c1ff8b679cdbbe12836e53c87734a1edb0071` / `be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e`, stopping at the first failure. No owner pins, worktrees, homes, controller or source repairs were altered, and no replacement Actions task was created.

RCC v18.19.6 remains **PUBLICATION HOLD**. Frozen source `7945f956b428359928908a9f42e36417b113dafd` and shipping Linux SHA-256 `5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21` remain distinct from Actions and diagnostics. #185 approval policy/signing scope and GO-2026-5970/GO-2026-6629 dispositions remain unresolved; protected reads returned 403 and the inspected required production environment was absent/read404. No credentials, tags, releases or publisher dispatches were created. Fresh #224/#225 remain open drafts and NO-GO. Research is not a release gate.

The old 60.253-ms versus 6.283-ms warm component wave is still an **unaccepted** comparison: its outer invocation failed the 0700→0750 guard and its reader omits production secure-path/stat/allocation behavior. Neither that timed wave nor any accepted completed wave was rerun. The actual failed baseline home remains 0750 with its receipts; it was not repaired or waived.

## Routing, bounds and next work

The coordinator owns native execution, difficult diagnosis, integrity admission and normal branch checkpoints. Two reused, bounded source/review workers were configured as `gpt-6-luna`, high reasoning; the coordinator was requested as GPT-6.1 Sol High. Actual task receipts and source/native outcomes are retained. Provider routing/execution telemetry is not exposed, so no provider telemetry claim is made. Each diagnostic invocation had a 25-second internal context under the 30-second sampled supervisor; builds were bounded at 90 seconds. Sampled caps were 2 GiB tree RSS, 128 MiB owned logical scratch, 8 MiB output and 2.5 GiB free-floor, with GOMAXPROCS=2/GOMEMLIMIT=1GiB. These are sampled bounds, not hard quotas.

Next dependency-safe work is an actual workload using the same finite streamed execution seam, with explicit per-task isolation/cancellation semantics and no weaker integrity checks; source-bound Actions consumer execution through its existing owner once the ordered baseline admits it; or an existing clone/FUSE/image-capable host route if one is available without provisioning. A future FD session requires independent rename/symlink/pre-commit identity controls before measurement. No pending CI or absent capability was turned into campaign-wide acceptance or universal no-go.

Documentation receipt
- Canonical guidance: no change; private source/measurement notes remain within the existing evidence branch.
- Durable learning: finite RCC streamed execution can amortize repeated Python startup while preserving six full workload checks and lease lifetime in this scoped diagnostic; cached-result comparisons must be rejected before timing; namespace UID mapping on this host fails read-only.
- Evidence: exact source/build binding, independent source/runtime reviews, seven supervisor results, 36 verified child responses, all 3,591 materialized-file readbacks, retained profiles and namespace stderr.
- Stale guidance removed: none; source-preparation notes are explicitly historical in this report.
- Remaining uncertainty: Actions version-chain admission; realistic workloads, worker cancellation/long-lived isolation; non-Linux mode behavior and genuine mounted lazy/COW lifecycle on a capable existing host.
