Accepted deterministic worker regressions are integrated by #235 at main ba2c904a7d3f75e6e5fdb3ae8b3f0d29ffeb764e. Exact PR source fa464a23 passed the six hosted contexts and all four native platforms; root and worker independently inspected the actual native ZIPs, binary/compiler/source metadata, Robot and Linux JAT receipts.

The fresh four-test race process returned 0 (2.019 s). Its supervisor separately failed at 120 s: independent root reproduction showed that its process observer counted an already-exited ps child after Go had completed. The failed wrapper, prior scratch-cap failure and numeric command output are preserved; root independently verified the owned process group was empty. This accepts the actual test result without labeling the failed wrapper successful.

#181 remains open for its broader acceptance scope. This test-only integration does not change frozen v18.19.6 shipping source 7945f956 or its prepared assets. No release tag or publication occurred.
