# RCC issue #133 contributor-map branch review

Review timestamp: 2026-10-11 12:15:43 UTC. Read-only review; no branch, source, issue, PR, or Actions state was changed.

## Live issue and reviewed refs

Issue [#133](https://github.com/joshyorko/rcc/issues/133), “[guide] Update stale setup guidance and repository structure references,” remains open. The current body was last updated 2026-10-11 02:32:55 UTC and has no separate comments. It records the existing correction scope: setup/build links and contributor commands were rechecked; the valid RCC 17.17.0 `venv` feature floor is not changed; the four current source-directory map omissions are on the existing published branch pending integration.

Current accepted main is `727c1ff8b679cdbbe12836e53c87734a1edb0071`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`. The existing published branch `codex/rcc-133-contributor-map-20261011` was at `012703c2474dd8585972edba0695bad4633d9230`, tree `884db3fd157fb540e4d48261d164e36896c20a01` when refreshed. Per root authorization, it is now advanced by a merge commit `5c2e8e95b0ecdaa62faf08b7e514c42b523593a3`, tree `e7ced47ed46cdaa3c1d9b3128f0e8a078c32a960`, with parents `[012703c, 727c1ff]` in that order and a DCO sign-off trailer. This preserves the accepted checkpoint and adds main as its second parent. The pre-publication local `git merge-tree --write-tree` produced exactly the same tree; API refs were rechecked before and after the publication.

## Independent diff and content checks

Before publication, comparing the branch against current main yielded one changed path, `CONTRIBUTING.md`, with exactly `4` insertions and `0` deletions; `git diff --check origin/main...origin/codex/rcc-133-contributor-map-20261011` exited **0**. After publication, GitHub compare reported the branch ahead of main by the preserved checkpoint and new merge commit, with merge-base exactly main727 and the PR diff still one file, four additions, zero deletions. Current main contains none of these four rows in the repo map; the candidate map has each row exactly once, with the reviewed purpose text:

| Directory | Purpose in candidate | Main directory exists | Candidate row count |
|---|---|---:|---:|
| `conda/` | Python environment creation, activation, and package installation | yes | 1 |
| `htfs/` | Holotree filesystem storage and environment materialization | yes | 1 |
| `remotree/` | Remote cache protocol, service, and synchronization | yes | 1 |
| `settings/` | RCC settings, endpoint, and provider-profile configuration | yes | 1 |

The purpose descriptions align with current source: `conda/` contains environment and package installation code; `htfs/` contains filesystem, library, relocator and materialization code; `remotree/` contains remote cache/service/synchronization modules; `settings/` contains settings, profile and provider configuration. `AGENTS.md` already groups these same directories. All 11 existing directory paths in the contributor map are unique and exist on current main; no stale path or duplicate row was found. The patch adds plain table rows and changes no links. The README’s repository setup link points to `/docs/BUILD.md`, which exists with exact case on current main. The venv guide’s `17.17.0 or later` floor is supported by `docs/changelog.md`'s v17.17.0 entry introducing dependency extraction in `rcc holotree venv`; the patch appropriately leaves it unchanged.

The repository now also has source packages such as `artifactpolicy/`, `artifactprovider/`, `artifacttrust/`, `environmentartifact/`, and `environmentlifecycle/` that are not listed in this map. The map has historically been selective rather than exhaustive; these are a possible separate scope expansion, not duplicate or stale rows in this bounded correction. The current issue update identifies the four-row omission as the intended correction. If a maintainer intends the map to enumerate every maintained package, resolve that scope before treating #133 as fully complete.

## Acceptance and integration recommendation

The existing branch satisfies the current bounded documentation correction: it fills the four confirmed omissions while preserving the valid setup link, toolchain/build guidance, and venv minimum. It needs no automated tests. Keep issue #133 open until a reviewed PR is integrated; no close action or issue-body mutation is included here.

Main is frozen for the accepted release. After that freeze lifts, open the prepared PR against then-current `main`, review its fresh diff, and merge the four-row documentation change if the tree/map checks still pass. Do not rebase, reset, force-push, or alter the published branch for this review. If main has changed `CONTRIBUTING.md` in the meantime, retain the four-row intent and resolve conflicts through the authorized owner rather than overwriting new guidance.

## Prepared PR

Title: [`pr-title.txt`](pr-title.txt)

Body: [`pr-body.md`](pr-body.md)

PR URL: https://github.com/joshyorko/rcc/pull/236. Exact-head CI is pending as recorded in `verification.json`.

## Documentation receipt

- Canonical guidance: `CONTRIBUTING.md` repo map; the published branch adds rows for `conda/`, `htfs/`, `remotree/`, and `settings/`.
- Durable learning: verify directory references against the current source tree; preserve supported historical feature floors rather than raising them to the latest release number.
- Evidence: `verification.json` records current main/branch refs and trees, changed path/numstat, row counts, path existence, link target, version-floor support, and numeric diff-check exit.
- Stale guidance removed: none. The verified gap was omission, not an invalid current path/link/version.
- Remaining uncertainty: whether maintainers want the contributor map expanded to cover newer artifact packages; no change is included for that broader editorial choice.
