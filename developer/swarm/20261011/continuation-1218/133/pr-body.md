## Summary

Add the four verified missing source directories to the contributor repository map: `conda/`, `htfs/`, `remotree/`, and `settings/`. The existing published checkpoint has been integrated with current main while preserving its original commit; head is `5c2e8e95b0ecdaa62faf08b7e514c42b523593a3`, tree `e7ced47ed46cdaa3c1d9b3128f0e8a078c32a960`.

The change updates only `CONTRIBUTING.md`. It leaves setup links, build commands, version guidance, runtime behavior, and release configuration unchanged. The README build link resolves to `docs/BUILD.md`; the documented RCC venv floor of 17.17.0 is supported by the changelog entry introducing that feature. No tests are needed for this documentation-only map correction.

## Verification

- Current main: `727c1ff8b679cdbbe12836e53c87734a1edb0071`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`.
- Compared with main, this branch changes one file with exactly four insertions and no deletions.
- `git diff --check origin/main...origin/codex/rcc-133-contributor-map-20261011` exited 0 before publication; the merged tree was also checked against main and contains only these same four rows.
- All four directories exist on current main and each added row appears once.
- Every pre-existing directory named in the contributor map exists on current main.

No release tag, binary, or release setting is changed. Hosted CI for this exact PR head is pending.

Refs #133
