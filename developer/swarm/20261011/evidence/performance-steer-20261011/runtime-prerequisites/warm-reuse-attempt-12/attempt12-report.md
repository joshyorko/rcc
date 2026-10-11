# Attempt 12: explicit acquire and persistent-reuse readiness

Outcome: explicit acquire succeeded, then the full-scope disk stop interrupted pool prewarm. No Action call or warm call ran.

- Source: `a70993fafc99a9f94041485542d5a720797ca394`; RCC `v18.19.3` SHA256 `7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428`.
- The consumer home was empty before RCC commands/imports. After `rcc --version`, only ordinary settings/templates existed: no CAS objects, materialization, or lease.
- Acquire returned status 0 in 16.114s with `cacheHit=provider`, materialization `c2fd1aa_5a1fac3_02c4107a`, and valid verification.
- Pool prewarm was interrupted by the supervisor while waiting for worker connection; worker status `KeyboardInterrupt`. Actions run: 0; warm calls: 0. Provider exited with status 0.
- Preflight projected 2,330,859,917 bytes, 31,372,095 bytes below stop. Observed final full owned total: 2,402,412,238 bytes, 40,180,226 over stop and 174,568,139 below hard cap. Free space was 2,889,736,192 bytes, 205,381,632 above the minimum. One-second disk scans caused a bounded stop-threshold overshoot; no hard-cap or free-space-floor violation occurred.
- Peak sampled process-tree RSS sum was 118,247,424 bytes (summed simultaneous RSS can double-count shared pages); largest individual sampled RSS was 68,321,280 bytes.
- `run_consumer.py` did not set `PYTHONDONTWRITEBYTECODE`; it was unset in the inherited launch environment. The Action child did not start, so this is not evidence about Action bytecode-cache behavior.
- Attempt 11 remains unchanged. No retry was made after acquire began.

Raw logs, full results, exact SHA256 inventory, and detailed machine-readable analysis are in this directory. See `attempt12-analysis.json`.
