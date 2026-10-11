# Independent persistent-worker runtime review

I read the retained controller, worker receipts, bounded-runner results, fixture/build binding, and each of the seven materialization trees directly. I did not build, rerun, or modify the workload.

The exact source (`8b3f059965f93f9bb80e04e5ffb8caf1c059d395e3bee3b1efcb3d9eda410c9c`) and binary (`6c058c78cd7ddf20dd3da6230e805ca043b72d8a784573f48f02c8c8955606ce`) match the retained build binding, including the recorded input hashes; build-2 returned zero. All seven retained bounded-runner results returned zero with `stop: null` and no observed live identities. The exit-73 lease-cleanup control ran first and passed.

I independently matched all 513 fixture manifest entries in each of seven retained materializations: 3,591 file size/SHA-256 checks with no mismatch. The six measured worker arms each emitted six ordered results with the expected aggregate `cacb33eadcd7c2abb40d6930a6943327b54223af3b51833c5f67a13b6521dce8` and 512 verified payload files per request (18,432 request-level payload checks). Each fresh arm shows six distinct child PIDs and lease IDs; each resident arm shows one PID and one lease ID across six requests. All leases were removed. The producer/provider fixture closure matches its pre-run 1,038-regular-file snapshot.

For every control/arm invocation, the eight pre-guard and post-guard records match: all private roots and homes remain owned mode 0700, and the isolated global HT directory remains owned mode 0750. These are the observed configured bounds and guards, not hard quota or proof against brief/escaped processes that the runner did not sample.

The retained three-pair medians are 643.637 ms (six fresh `ExecuteWithStreams` calls) versus 134.099 ms (one resident child, six streamed requests), for the measured execution batch. Provider setup + separate cold acquire + batch medians are 2,338.636 ms versus 1,796.746 ms. This is a finite synthetic warm-OS-cache workload result; it does not establish Actions acceptance, a persistent daemon, customer/solver performance, or release readiness.

The earlier cached-work implementation remains a **NO-GO** and was not benchmarked; the reviewed frozen source hashes the 512 payload files inside each request loop. The earlier 60.253 ms versus 6.283 ms component wave remains unaccepted because its outer invocation failed the 0700→0750 guard and its reader omitted production secure-path/stat/allocation behavior. Its failure and original 0750 home evidence remain preserved; it was not rerun or repaired. See [the successor checkpoint report](../report.md) for the prior acceptance and shipping holds, which this diagnostic does not alter.

Detailed machine-readable counts are in [independent-runtime-review.json](independent-runtime-review.json); raw evidence remains in [results](results/controller.jsonl), with the original fixture closure and per-run receipts retained there.
