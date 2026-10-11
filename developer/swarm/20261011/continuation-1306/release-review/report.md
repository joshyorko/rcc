# Independent two-phase release workflow review

Reviewed frozen commit `de40558f994cfbacfad0a1aa5745551a84a5115f` (tree `b9e66759bf4d7cfcc6ad83e9108f2b235b676e20`), based on `727c1ff8b679cdbbe12836e53c87734a1edb0071`. The worktree was clean. This review did not modify source, publish, create tags/releases, dispatch workflows, or change GitHub state.

## Findings

1. **P2 — The publisher does not independently validate the semantic index entry.** [`validate_release_topology.py`](/workspace/work/rcc-swarm/two-phase-release/scripts/validate_release_topology.py:199) checks that the first `tested` entry has URL basenames matching four expected filenames, but it does not check the entry version, the URL host/release-version path, or that URLs target the current release. [`release_publication.py`](/workspace/work/rcc-swarm/two-phase-release/scripts/release_publication.py:281) verifies the index bytes against the manifest hash, but does not parse its semantics. I confirmed this with an adversarial fixture: changing `tested[0].version` to `v0.0.0` and all four URLs to `https://attacker.invalid/wrong-version/<expected-filename>` still returned `errors: []`. The current generator in [`rcc.yaml`](/workspace/work/rcc-swarm/two-phase-release/.github/workflows/rcc.yaml:330) emits the expected version and URLs, so I did not find a bad generated index; the independent publication gate simply would not catch a regression or malformed prepared artifact here. Add expected-version and release URL checks before treating this validation as complete.

2. **P2 — A separate one-shot manual recovery workflow can publish v18.19.5 without the new protected environment.** [`recover-rcc-185.yml`](/workspace/work/rcc-swarm/two-phase-release/.github/workflows/recover-rcc-185.yml:27) enables its write-capable `recover` job on `workflow_dispatch` from `main`; it directly invokes the recovery script's `publish` phase ([lines 60–64](/workspace/work/rcc-swarm/two-phase-release/.github/workflows/recover-rcc-185.yml:60)). That script creates and publishes `v18.19.5` and flips the release out of draft ([`recover_rcc_185.py`](/workspace/work/rcc-swarm/two-phase-release/.github/workflows/recovery/recover_rcc_185.py:237)). It is narrowly hard-coded to a historical tag/source/run and refuses an already-existing release, so it cannot publish a new version and is not an automatic tag-triggered bypass. It remains an alternate manual publication route outside `rcc-production`; document this explicit historical exception or disable it once recovery is no longer needed.

## Controls that passed review

- Preparation dispatch is restricted by the job conditions and checkout assertions to the exact `main` SHA. The publisher itself only runs for manual dispatch on `refs/heads/main`; the prep run must be `workflow_dispatch`, have `head_branch == main`, exact head SHA, expected workflow ID/path, successful conclusion, and the expected preparation artifact.
- The publisher checks that the prepared source is an ancestor of the publisher run's main SHA, validates the version/changelog and absence of tag-triggered publication in the RCC source, and refuses an existing tag or release. Tag creation is a create-only API call; there is no delete/recreate route.
- `rcc-production` protection fails closed when its audit token/API data is absent or unreadable. The validator requires one nonempty required-reviewer rule, `prevent_self_review == true`, and `can_admins_bypass == false`; the job also uses the GitHub environment approval gate.
- The artifact downloader compares the raw ZIP SHA-256 with GitHub's artifact digest, drops the bearer header on cross-origin HTTPS redirects, rejects non-HTTPS redirects, traversal/absolute/backslash/duplicate/symlink/nonregular ZIP entries, and caps download and expanded sizes. Bundle validation checks the exact nine-asset set and hashes, manifest/provenance identity, actual Go 1.26.9 build metadata for all eight binaries, and exactly four native receipts plus Linux JAT. The preparation API verifier requires successful Build, Release Candidate Verification, all four native jobs, and Assemble jobs; the release-candidate gate receipt must be source-bound with both gate and tee exit codes zero.
- Publisher action references are SHA-pinned. Inputs are passed through environment variables and quoted shell arguments; the release version and source SHA are format-validated before use. The Homebrew PAT is step-scoped and separate from the GitHub token. The code correctly states that downstream dispatch permission is not proven by its read-only workflow preflight.
- The old `Create Release Tag` workflow is now a read-only manual hold and exits nonzero. `patchraptor-release.yml` still automatically pushes a version bump and dispatches that deprecated entry after qualifying successful main pushes, but it does not directly create a tag or publish a release; the dispatched target now fails closed. This stale auto-bump path is not a current release bypass, though it remains a separate automatic `main` mutation path worth retiring or explicitly retaining.

## Verification

- `python3 -m unittest scripts.test_release_publication scripts.test_validate_release_topology` — exit 0, 35 tests passed.
- Parsed all 10 `.github/workflows/*.yml` files with `yaml.safe_load` — exit 0.
- Adversarial topology check described in finding 1 — exit 0 for the check command, demonstrating the validator returned no errors for the deliberately wrong version and URL host/path.
- `pytest` was unavailable in the supplied Python runtime (`No module named pytest`); the test files use `unittest`, and the 35-test suite ran directly.
- No Actionlint binary was present, so no `actionlint` result is claimed.

## Security-state limit

The separate security lane observed GitHub's push-hook summary of 15 default-branch Dependabot vulnerabilities (1 critical, 6 high, 4 moderate, 4 low). The alert inventory API returned 403, so affected packages and reachability remain unknown; this review does not classify those alerts as cleared or nonblocking.

## Documentation receipt

- Canonical guidance: no source docs changed by this review; release-process documentation should account for the fixed-version recovery exception if retained.
- Durable learning: artifact integrity alone does not validate release-index semantics; workflow-specific exceptions must be explicitly included in the release-path inventory.
- Evidence: exact frozen source SHA/tree, the 35 passing unit tests, the 10-file YAML parse, and the adversarial index check above.
- Stale guidance removed: none.
- Remaining uncertainty: Dependabot alert identities/reachability are unavailable through the current GitHub integration; the v18.19.5 recovery workflow's future operational need remains a maintainer decision.
