# Performance workstream checkpoint — 2026-10-11 03:45 UTC

Release engineering remains accepted at main `727c1ff8b679cdbbe12836e53c87734a1edb0071`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`. All four required main push workflows and four native platforms passed root verification. Accepted source324 has full11 numeric gate/tee exits0 and the same tree; that receipt is not rebound to main or a future tag. No v18.19.6 tag/release exists. Performance does not gate publication.

## Actual Actions controls admitted and completed

All measurements use isolated public community Actions `a70993fafc99a9f94041485542d5a720797ca394`, exact supported RCC v18.19.3 binary SHA256 `7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428`, and unchanged Python3.11.11/actions-core1.0.0 fixture. The RCC3 binary reports modified VCS metadata; tagged source is a structural reference, not exact clean source-build proof. No Actions tracked source, production pin, branch or active swarm worktree changed. Current draft221 is `ba85aac7324ea4463bfba8c5f8586e27706dc2fe`, with a different core1.0.2 fixture and existing owner HOLD; these community controls do not qualify it.

| Control | Measured scope/result | Remaining boundary |
|---|---|---|
| Unchanged real artifact-to-Action test, attempt10 | Numeric pytest0, publish/acquire/Action0, expected result. Publication28.970s; acquire14.672s; import45.855s including those nested stages; worker startup0.771s; Action API0.856s; outer setup/test/cleanup49.520s. Root rehashed5,541 actual provider objects. | One Linux run using partial solver/package caches, not a fully cold host or optimization comparison. |
| Fresh consumer, attempt12 | Empty home before RCC; settings-only/no CAS/materialization/lease before acquire. Explicit supported adapter acquire0/provider/valid permissive-local verification; command16.109s, instrumented stage16.114s. Root rehashed5,541 actual consumer CAS files. | Disk guard stopped pool prewarm; zero Actions. Combined cold-acquire-to-first-invocation timing remains outstanding. |
| Warm-only reuse, attempt13-retry-01 | Existing acquired attempt12 home, no acquire/copy/republish. Numeric worker/supervisor0; six expected Action results. Constructor/prewarm0.800146s, first Action API0.682014s, five warm API calls13.638/12.836/12.285/12.507/12.497ms, median12.507ms. Same wrapper264639, Python264655, PID creation times, handle/key, generation0 and lease observations; processes gone and lease removed. | Supported external min=max1/reuseTrue control, not full controller/autoreload acceptance. Instrumented context times include identity/lease diagnostics; exact API timer excludes those and pool obtain/return. No gain claimed. |

Both completed Action controls deliberately dispose their child process after success. Their actual RCC receipts therefore report failed/-1 with valid verification; warm-control RCC wrapper exits0. Do not relabel those disposal receipts as successful child exit. Root independently read all six result files and the warm disposal receipt, validated source hashes and sampled process identities, and confirmed no remaining sampled process. The live lease JSON was not separately saved before disposal: its parsed fields and SHA256 are observer evidence, not independently rehashed raw live JSON. The receipt matches its observed ID; owner start ticks match the sampled process creation time.

Warm-control outer setup/cleanup wall8.962s, sampled tree RSS sum146,882,560B, sampled individual RSS52,645,888B. Tree sums may double-count shared pages and sampling may miss peaks. CPU stage deltas include observer and surviving/new processes, omit already exited children, and are not total API CPU. Attempt10 wait4 covers reaped-child user28.932/system13.405s and527,680KiB maxrss, distinct from sampled/stage RSS. Syscall/object operation counts, wire bytes and controlled OS cache state are not instrumented; do not invent them.

## Resource and failure evidence

Attempts04/05/08 stopped at disk guards;07 exposed19 distinct package checksum inconsistencies repeated twice. Root showed all38 actual package SHA256s matched JSON metadata while expected values disagreed, consistent with the retained SOLV being inconsistent; interruption causality is unproven. Only that SOLV was atomically moved; JSON/channel/pin/checksum verification remained unchanged. Attempts10/12 then succeeded in publication/acquisition with unchanged JSON/noarch SOLV and the regenerated other SOLV. Ordinary harness/setup failures01/02/03/09/11/13 are separate from Actions acceptance and remain retained. Root's first warm-verifier literal source-pattern check used the wrong local variable name; actual timer boundaries were inspected and verified with the corrected helper, without modifying runtime files.

The attempt10 nested counter omitted earlier evidence/archives; root preserved it and corrected full scope. Count checkout + entire runtime-prerequisites tree + fixed22,282,402-byte RCC3 tool. Final warm run uses2,411,292,739 owned bytes, free2,879,393,792. Hard2.4GiB=2,576,980,377B; operational stop2.35GiB=2,523,293,286B; free floor2.5GiB=2,684,354,560B. One completed raw home and bad SOLV were removed only after root checked reversible gzip archives, all member hashes/metadata and no active handles. Archives and diagnostics remain counted and retained. No further runtime control is admitted; do not silently raise the budget or remove historical/unowned state.

## Existing graph and remaining runtime matrix

RCC118 compatibility parent retains closed124 identity and closed208 serving acceptance, plus open125 research. Merged229 supplies harness/baseline, not completed optimization research; historical64 is closed/unmerged and its percentage claims require current reproduction. RCC185 remains separate L5 operator governance. Draft224/225 stay NO-GO and are not release prerequisites.

Coordinate the full matrix through existing Actions101 campaign/controller,133/134 adapter/execution,139/143 same-subject conformance/default-off policy and221 Runtime integration. Current candidate6 support is blocked on an owner-supported version/feature policy and admitted fixture/controller checkpoint: both inspected adapters require exact RCC3. Do not bypass the check or change production pin.

| Runtime scenario | Status / next prerequisite |
|---|---|
| Cold first invocation | Fresh consumer acquire accepted separately; complete contiguous cold acquisition plus first Action still needed in owner-approved fixture. |
| Warm execution/persistent reuse | Historical stable3 external control accepted; current221 campaign subject still requires owner admission. |
| Source-only autoreload | Waiting for admitted controller/metadata fixture with unchanged environment/generation evidence. |
| Environment-changing reload/generation pinning/lease drain | Waiting for exact subject controller plus distinguishable artifact generations and drain/last-good oracle. |
| Multi-package/concurrent workers | Waiting for owner-admitted multi-package fixtures and quiet resource reservation; current min=max1 control does not qualify concurrency. |
| Provider outage/restart/recovery/last-good | Waiting for fixture/carrier/trust-policy admission and restart/generation oracle. Omitting provider changes policy; RCC-only offline proofs are not full Actions recovery acceptance. |

## Research retained, no recommendation

Complete source-bound [reconciliation](reconciliation.md) and [warm admission](warm-reuse-admission.md) retain zstd vs gzip, small-object packfiles, prewarm/persistent reuse, safe reflink/clonefile, lazy/FUSE, and compressed immutable images+COW. Existing gzip/object-per-file compatibility, crypto identity, verification/trust/offline, lease/generation, relocation/native, recovery/cleanup/isolation/N-1 guarantees stay mandatory. Encoding changes need exact old carrier reconstruction or explicit versioned digests/dual-read/rollback; no new format/default enters v18.19.6.

Prior five-run Linux baseline cold24.209/warm0.700/provider-dead0.717s and one publication34.551s remain old source-bound observations. Separate publication profile5077 indexed objects/4302<4KiB=84.7%,32.077s wall/14.44s sampled Go CPU used different source/toolchain/scope and overlapping shared load. New fixture has5537 indexed versus5541 actually stored metadata-inclusive objects. These samples are not a controlled comparison or a proven materializer bottleneck. Persistent reuse is now observable on supported stable3; no optimization improvement or final materializer recommendation is established.

## Worker lifecycle and next actions

Root Sol/High remains coordinator/checkpoint writer. Luna/High runtime, native acceptance, identity preservation, graph inventory and worker-limit/test lanes are complete; Sol/Extra High reconciliation/admission and prior profile lanes are complete. No worker was interrupted or diverted from release gates. One root slot of seven active at checkpoint; no new infrastructure or six implementation fan-out.

Next release action is the existing authorized/authenticated publication path, followed by actual tagged-source full11/native gates and nine distribution assets/checksums/version/index/signing/Homebrew validation. Private Dependabot inventory remains403/unverified; dependency findings retain scoped disposition. Next performance action is existing Actions-owner admission of a same-subject controller/version contract, or a separately admitted quiet bounded stable3 fixture/control with operation counters and raw live lease preservation. Preserve prior receipts; do not relabel old evidence or select storage defaults without repeatable comparative platform/consumer evidence and rollback.
