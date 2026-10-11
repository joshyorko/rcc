# Attempt 13 retry: warm-only persistent-process control

Outcome: all six Actions passed on the already acquired attempt-12 materialization; the same worker process and lease persisted through the five warm calls and idle intervals. Cleanup verified the process exit, lease removal, and matching receipt. No acquire, provider call, copy, or environment rebuild occurred in this control.

- Actions source: `a70993fafc99a9f94041485542d5a720797ca394`, tree `562b49fc03bc4a16ef8a0b7b6f50b9bec88de959`; RCC v18.19.3 SHA256 `7e588c01751ca2ae15ba13ef67f2f4b7567697a5a8389737059a73936f509428`.
- Existing home: attempt 12, materialization `c2fd1aa_5a1fac3_02c4107a`, digest `sha256:ec3795182e7261439a8b9a9a9b7bd0834b7a7d65202067b17e5589e06f41b83e`, previously `cacheHit=provider` and verified.
- Settings: reuse enabled, min=max=1. First and five warm calls all returned 0 and `rcc-v18.19.3`. Handle object/key/generation and wrapper/worker PID plus creation times stayed constant.
- Timings: pool constructor/prewarm 800.146 ms. First `run_action` 682.014 ms (obtain-through-return 726.972 ms); warm `run_action` calls: 13.638, 12.836, 12.285, 12.507, 12.497 ms. The stage values are separate timings and their sum is not an exact end-to-end measurement.
- Lease: `ad77a4d62614e3f65026a4b82bb8c895`, SHA256 `e97533fa4144a7af2f3be20f0b2b72736152fc1c4dd877c44011ca3c02f17e2b`, owner wrapper PID `264639`; same lease ID/hash was present during every call and idle gap. After pool disposal, lease was absent and receipt matched it. Receipt status is `failed`, exit `-1`, reason `child exited non-zero`, verification valid; wrapper returned 0 and no tracked wrapper/worker remained.
- Python bytecode override: none; `PYTHONDONTWRITEBYTECODE` was unset in the launch environment.
- Resources: final full owned total 2411292739 bytes (stop 2523293286, hard 2576980377); free 2879393792 bytes (floor 2684354560). Peak sampled process-tree RSS sum 146882560 bytes; largest individual sample 52645888 bytes. Disk scan configured at 0.2 seconds; observed max scan gap 604.8 ms due to rescanning the writable warm home.
- The first attempt-13 supervisor setup failure is preserved separately in `warm-reuse-attempt-13/`; it stopped at a NameError before preflight or any RCC/Action work. The actual control is in this retry namespace.

Raw results, events, fixture, and SHA256 inventory are in this directory. Machine-readable summary: `attempt13-analysis.json`.

Public checkpoint note: raw `attempt13-analysis.json` remains preserved in cloud; this checkpoint includes [curated summary](attempt13-summary-public.json), excluding the complete private-home inventory and recording its original hash.
