# Issue #133 documentation acceptance audit

Audit date: 2026-10-11. Read-only. Source comparisons use immutable main `07db6221a3540d227f2106eccf69fe0e7c11fee0` and current candidate `324524fffa4bd0f76c618f9fbb6ede30e733b2ff`. The shared checkout was at `47a60ea78c5d97c892177f5649a1c950399faa6a`; relevant files were read directly from the two requested commit objects with `git show`, not from that older checked-out tree. There are no documentation changes between main `07db622` and candidate `324524`; the only diff among audited source/task files is an added Python acceptance precheck in `tasks.py` at candidate head.

## Finding

The historical claims in issue #133 do not all describe live guidance anymore. On both audited SHAs, the README build/setup link is valid, every directory actually named in `AGENTS.md` and the CONTRIBUTING repo map exists, and the `docs/venv.md` version floor has historical support. One real documentation gap remains: `CONTRIBUTING.md`'s repo map is incomplete for the maintained source tree, specifically omitting `conda/`, `htfs/`, `remotree/`, and `settings/`. Newer artifact packages are also absent from the map. `AGENTS.md` already names the first three and settings, so the minimal issue-tied correction is to update the CONTRIBUTING map only.

## Issue and live guidance

Issue #133 is open. Its original prose alleges a broken setup pointer, references to missing directories, and a stale minimum version. The issue body also contains an October 10 reconciliation: the README setup link and build guide exist; the contributor map remains incomplete (including `conda/`, `htfs/`, and `remotree/`); and `17.17.0` may be a valid feature minimum, so it should not be raised just because the current release is 18.x. That reconciliation matches the source audit below. The issue has no separate comments.

- `README.md:45` links to `/docs/BUILD.md` as the repository build setup guide. `docs/BUILD.md` exists in both trees; `git ls-tree` confirms exact case/path. `docs/BUILD.md:5-22` identifies Go, Invoke, Robot, `inv -l`, `inv build`, and `inv robot`; the instructions are sparse but the task names still exist.
- The directory names currently listed by `AGENTS.md:7-11` all exist at both SHAs: `cmd/`, `operations/`, `conda/`, `htfs/`, `remotree/`, `common/`, `settings/`, `pathlib/`, `shell/`, `assets/`, `templates/`, `docs/`, `blobs/`, `build/`, `developer/`, and `robot_tests/`.
- Every path currently named by the `CONTRIBUTING.md:121-131` map also exists: `cmd/`, `operations/`, `common/`, `pathlib/`, `shell/`, `assets/`, `blobs/`, `templates/`, `robot_tests/`, `developer/`, and `.dagger/`. The problem is omission, not a stale path. In addition to the issue's three known omissions, `settings/` is an active package also omitted from this map. The source tree has first-class artifact directories (`artifactpolicy/`, `artifactprovider/`, `artifacttrust/`, `environmentartifact/`, and `environmentlifecycle/`) that could be grouped in the same map if the maintainer wants the map to reflect current areas of change.
- `docs/venv.md:21` says RCC 17.17.0 or later. `docs/changelog.md:790-793` records `rcc holotree venv` dependency extraction as introduced in v17.17.0; lines 771-775 say the venv/depxtraction guide and man page appeared in v17.17.4. The 17.17.0 floor is therefore plausibly specific to the documented feature and is not contradicted by the project’s current v18 release. No version change is justified by this issue evidence.

## Commands checked against source

The current contributor commands are structurally supported at both SHAs:

- `CONTRIBUTING.md:39-56` uses `rcc run -r developer/toolkit.yaml -t robot`, plus `--dev` tasks `unitTests`, `local`, `build`, `assets`, `agentDocs`, and `tools`. `developer/toolkit.yaml:1-13, 26-29` maps these to `python call_invoke.py robot`, `test`, `local`, `build`, `assets`, and `agentdocs`; `tools` maps to `tooling` at lines 12-13.
- The direct Invoke alternatives at `CONTRIBUTING.md:104-112` are defined in `tasks.py`: `test` at 853-862, `build` at 1415-1451, `robotsetup` at 1472-1480, `local` at 1483-1490, and `robot` at 1493-1502. `robot` has `robotsetup`, `assets`, and `local` as prerequisites, so the “after `inv robotsetup`” note at line 111 is safe but redundant for `inv robot` itself. The robot task uses `build/rcc` and then runs Robot Framework.
- `tasks.py:1415-1451` confirms `inv build` is the cross-platform build task and enumerates Linux amd64, macOS amd64/arm64, and Windows amd64 targets. `inv local` builds the host binary after tooling/assets and tests.
- The Go toolchain statement is internally consistent at these SHAs: `go.mod:3` requires Go 1.26.9; `developer/setup.yaml:4-12` explains that Conda provides the 1.26.5 bootstrap while Go auto-selects 1.26.9; `AGENTS.md:25-34` and `CONTRIBUTING.md:21-26, 93-99` state the selected compiler version and commands. No unsupported Go command was found.

This audit validated command names and prerequisites against source; it did not execute Invoke/toolkit commands, create environments, download dependencies, or run a release gate. Current candidate CI `38103534317` built the exact candidate and its four hosted native jobs succeeded, but that is not claimed as a numerical verification of each documented contributor command.

## Minimal proposed correction

Update only the repo map in `CONTRIBUTING.md:121-131`. Add rows for `conda/`, `htfs/`, and `remotree/` as the issue’s confirmed omissions, plus `settings/` to match the existing AGENTS structure map. If maintainers want the map to include newer code surfaces, add one grouped row for `artifactpolicy/`, `artifactprovider/`, `artifacttrust/`, `environmentartifact/`, and `environmentlifecycle/`. Do not change the README setup link, the `docs/BUILD.md` path, or the `17.17.0` venv floor on current evidence. The bounded repo-map correction was subsequently authorized and committed as described below; no issue or GitHub PR mutation was made.

## Source identity and path hashes

Path hashes below are Git blob IDs from each immutable source commit. They show the primary live docs are byte-identical between main `07db622` and candidate `324524`; `tasks.py` differs because of candidate Robot acceptance wiring.

| Path | Blob at `07db622` | Blob at `324524` |
|---|---|---|
| `README.md` | `80ad86e1ad01fc12cfac986c813bea1299c5ebbd` | `80ad86e1ad01fc12cfac986c813bea1299c5ebbd` |
| `CONTRIBUTING.md` | `2da80862743817bdb709287b4f06758ef48c8822` | `2da80862743817bdb709287b4f06758ef48c8822` |
| `AGENTS.md` | `469d7d53342860ed7c9df7742934ca9b2004449b` | `469d7d53342860ed7c9df7742934ca9b2004449b` |
| `docs/BUILD.md` | `08e80147fb78914c1379739bfdf5ffee94453691` | `08e80147fb78914c1379739bfdf5ffee94453691` |
| `docs/venv.md` | `5983484ff2a707d00929215c609ec843e15d31ea` | `5983484ff2a707d00929215c609ec843e15d31ea` |
| `docs/agent-boundaries.md` | `be75c396f5ec6efd24daa9b963d5de4142928076` | `be75c396f5ec6efd24daa9b963d5de4142928076` |
| `docs/skills/rcc-development/SKILL.md` | `adf85746355efdf968329483b13aa7180e4b1d3b` | `adf85746355efdf968329483b13aa7180e4b1d3b` |
| `developer/README.md` | `1ea3e88b3183eae5dfdead843f7a285cd740d42e` | `1ea3e88b3183eae5dfdead843f7a285cd740d42e` |
| `developer/toolkit.yaml` | `adf3c3363a5fca5c9209d38610adaaf870377fa5` | `adf3c3363a5fca5c9209d38610adaaf870377fa5` |
| `developer/setup.yaml` | `1fa65bfc87ac7fd6540a914ecf241d5f70dbaf9f` | `1fa65bfc87ac7fd6540a914ecf241d5f70dbaf9f` |
| `go.mod` | `45d55a4487431d47ab10d200e465c44f4c11f3c3` | `45d55a4487431d47ab10d200e465c44f4c11f3c3` |
| `tasks.py` | `6d0ea440bfac28fce5bf1304ed096b909c9fa632` | `417bc571423bcdf0c07c80647e3b6b7bbe57105e` |

The issue and docs acceptance policy were read-only references: repository `AGENTS.md`; `docs/agent-boundaries.md` (`Read source/history/issues and propose documentation deltas` allowed, edit only when requested); and `docs/skills/rcc-development/SKILL.md` (source orientation, verify gates, and required documentation receipt). The task explicitly authorized an audit/report, not a documentation edit, so the audit stops at a concrete proposed delta for root review.

## Remaining uncertainty

No uncertainty remains about the live README target, current directory existence, toolkit task names, or `17.17.0` changelog association at these SHAs. Whether to expand the contributor map beyond the issue’s three confirmed omissions, and whether the brief `docs/BUILD.md` should be rewritten to point into `CONTRIBUTING.md`, are maintainer scope choices; neither is required to correct the verified issue gap.


## Implemented follow-up

Root authorized the four-row CONTRIBUTING-only correction on a new worktree from main `07db6221a3540d227f2106eccf69fe0e7c11fee0`, branch `codex/rcc-133-contributor-map-20261011`. The change is committed as `94c61125528c8adb0a13561d9b47179293e627a0`, tree `884db3fd157fb540e4d48261d164e36896c20a01`; commit includes `Signed-off-by: Josh Yorko <joshua.yorko@gmail.com>`. Changed file: `CONTRIBUTING.md` only (4 insertions). Final verification: `git diff --check HEAD^ HEAD` passes, and a focused filesystem/table check confirms `conda/`, `htfs/`, `remotree/`, and `settings/` each exist and have one repo-map row. No tests were needed for this documentation-only change. The worktree is clean. Root owns any publish/PR step.

Documentation receipt
- Canonical guidance: `CONTRIBUTING.md:125-128`, added concise entries for `conda/`, `htfs/`, `remotree/`, and `settings/`.
- Durable learning: the contributor repo map must include the current environment, Holotree, remote-cache, and settings packages; exact directories verified from source.
- Evidence: immutable source `07db6221a3540d227f2106eccf69fe0e7c11fee0` and candidate `324524fffa4bd0f76c618f9fbb6ede30e733b2ff`; source/path hashes above; the added directory existence and map-row check; clean `git diff --check`.
- Stale guidance removed: none; this corrects an omission without changing current commands, links, or version claims.
- Remaining uncertainty: whether maintainers want additional newly introduced artifact packages added to this same map; intentionally excluded per bounded scope.
