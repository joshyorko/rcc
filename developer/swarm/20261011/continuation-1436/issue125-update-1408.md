RCC swarm checkpoint at 2026-10-11 14:08 UTC: the performance lane continues alongside release CI. Publication remains **HOLD: no tag or publication**. Full six-way #125 scope and all historical source-bound observations remain intact; no optimization is selected.

New evidence: one external CPython codec invocation covered **all20 indexed objects with logical size>1MiB**,102,457,064 logical/45,517,701 original stored bytes (65.524% of this fixture's stored data). Root independently verified20 original objects, manifest/index,40 canonical encoded frames and all500 timed samples.

| Component,20-object per-repetition total | Median wall seconds |
| --- | ---: |
| Legacy gzip decode |0.385237|
| External gzip level1 encode |1.196861|
| External zstd level3 encode |0.414601|
| External gzip level1 decode |0.393095|
| External zstd level3 decode |0.119449|

New frames total43,769,827 gzip bytes/39,419,371 zstd bytes. These are external codec measurements on the historical Actionsa709/stableRCC3 fixture, **not Go/RCC/Actions end-to-end gains**. Worker numeric exit0 and500 samples were preserved, but the **whole invocation failed in its outer wrapper** (`SystemExit0` caught by `BaseException`). Outer1 is inferred from retained traceback/source flow; numeric outer completion was not captured. No timed retry occurred. Root accepted the component data only; the failure and a separate five-stub-test wrapper proposal remain visible.

All six directions remain separate: compression now has small and large object component coverage; small-object packs retain10 warm filesystem controls with equal cryptographic verification; persistent worker reuse retains historical real Actions3 observations; reflinks retain one scoped overlay errno95 with observer limitations; lazy/FUSE and compressed images/COW still need actual working-set, supported filesystem and lifecycle qualification. A Go BestSpeed analogue is being prepared under an independent bounded lane; no build/runtime admitted yet, no dependencies or production behavior changed.

Actions evidence consumed: root acquired original #308 artifact11699213923 (8,664B), verified all7 members and9 immutable c103 source pins, and checked JUnit3/0/0/0 for exact c103/d5a/6181. This proves the scoped stableRCC3/Core1.0.2 public cases, last-good recovery and one publication. Fixture elapsed78.476/59.801/33.760s includes setup/cleanup; it is not a cold-first Action/API benchmark. #308's wheel repair does **not** relax the adapter/helper/workflow/fixture RCC3 gates; its candidate-wheel offline-warm test expects failure. The owner is explicitly authorized to repair newer-version compatibility. We await an implemented, owner-qualified checkpoint/controller/fixture contract before isolated candidate6 controls; publication is not required for those tests. No Actions source, production pins or worktrees changed.

Release work: PR237 c6c7a012 correction is pushed; root139 script tests passed, Linux+ARM native jobs and actual binary/source/compiler proofs passed, while Windows/Intel/full11 remain running. Source727 remains preserved. Fresh cold consumer disk budget still does not fit; no unowned cache was deleted.

Durable ledger, raw measurements, failures, owner reconciliation and restart: [17841dd checkpoint](https://github.com/joshyorko/rcc/tree/17841dd6859b0382f339075d5e7e345dbb2c9891/developer/swarm/20261011/continuation-1406). Root verified147 new remote files6,484,737B and preserved all1,034 prior entries including original119. #125 remains research, not a mandatory release gate.
