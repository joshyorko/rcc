# Independent re-review of release workflow corrections

Reviewed frozen commit `83ddea8a8e8304692b53ffe38d9c6bd03ee1d370` (tree `43366f51cf0871759edee7a81c80965d9e804531`), parent `de40558f994cfbacfad0a1aa5745551a84a5115f`, based on `727c1ff8b679cdbbe12836e53c87734a1edb0071`. The worktree was clean. This was read-only; no tags, releases, dispatches, pushes, or workflow mutations occurred.

## Correction review

The prior index-validation gap is closed. The release topology helper now requires the latest tested entry's version to equal the approved source version and every platform URL to exactly match the canonical `https://github.com/joshyorko/rcc/releases/download/<version>/<platform-asset>` mapping. `validate_prepared_bundle` independently runs those checks with the approved version, and both assembly and publisher workflow commands pass that version explicitly. The historical recovery workflow is now a read-only `workflow_dispatch` hold with a failure message. The source validator and workflow topology validator require this hold and reject the former recovery permissions/commands.

I exercised seven malformed index cases: wrong latest version, attacker host, prior version path, different product repository, swapped architecture asset, explicit port, and query redirect. All seven were rejected with the expected semantic error. This confirms the previous adversarial case no longer passes. The recovery hold has `contents: read`, a manual-only trigger, and exits before any release operation.

The old PatchRaptor bot still has an automatic qualifying-main version bump and dispatches the now-failing `Create Release Tag` hold. It cannot create tags or publish through that route; the release-process and workflow README now identify it as version-bump automation only. This is not a publication bypass.

## Verification

- `python3 -m unittest scripts.test_release_publication scripts.test_validate_release_topology` — exit 0, 37 tests passed.
- Seven independent malformed index mappings — all rejected.
- Parsed all 12 `.yml` and `.yaml` workflow files — exit 0.
- `git diff --check de40558..83ddea8` — exit 0.
- Worktree status remained clean.

## Remaining release constraints

This source review does not prove hosted full release candidate and four-native acceptance for this exact candidate, nor that the `rcc-production` environment and audit token are configured correctly in GitHub. The separate Homebrew preflight proves only that the downstream workflow is readable and active, not that its token can dispatch it; operators must verify that separately. Signing coverage remains limited as described in `docs/release-process.md`. The Dependabot inventory remains unknown because the integration's alerts endpoint returned 403; the push-hook summary was 15 alerts (1 critical, 6 high, 4 moderate, 4 low), so no security-clear claim is made.

No publication is approved by this review; root's publication hold remains in force.
