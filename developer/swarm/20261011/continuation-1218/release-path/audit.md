# RCC v18.19.6 release-path audit (read-only)

**Observed main:** `727c1ff8b679cdbbe12836e53c87734a1edb0071`, tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`. The tree matches accepted candidate #324, but source-bound evidence remains distinct by commit. `common/version.go` says `v18.19.6`; changelog has an empty `Unreleased` section and a dated v18.19.6 section. No v18.19.6 tag or release exists in the GitHub API view. Existing latest stable is v18.19.5.

## Read-only account and API results

Commands were run from `/workspace/work/rcc-swarm/final-main-727`. No credential value was requested or printed.

| Command | Exit | Result |
|---|---:|---|
| `gh api user --jq '{login, id}'` | 0 | Authenticated as `joshyorko` (ID 54248591). |
| `gh api repos/joshyorko/rcc --jq '{full_name, default_branch, permissions}'` | 0 | Default branch `main`; API reports admin/maintain/push/pull/triage true for this identity. This is repository role metadata, not a dispatch test. |
| `gh api repos/joshyorko/rcc/actions/permissions` | 1 | HTTP 403 `Resource not accessible by integration`; Actions policy settings unavailable to this caller. |
| `gh api repos/joshyorko/rcc/actions/secrets` (names/metadata only) | 1 | HTTP 403 `Resource not accessible by integration`; secret names/readiness are not observable. Secret values were not requested. |
| `gh api repos/joshyorko/rcc/actions/workflows --paginate` | 0 | `Rcc` and `Create Release Tag` workflows are active; workflow metadata readable. |
| `gh api repos/joshyorko/rcc/git/ref/tags/v18.19.6` | 1 | HTTP 404: tag ref absent. |
| `gh api repos/joshyorko/rcc/releases/tags/v18.19.6` | 1 | HTTP 404: release absent. |
| `gh api repos/joshyorko/rcc/releases/tags/v18.19.5` | 0 | Stable v18.19.5 is published; its 9 assets include index.json and eight binaries. |

The authenticated API proves account identity, repository admin metadata and read access to workflow/runs/artifacts. It does **not** prove that `HOMEBREW_TOOLS_PAT` exists, can dispatch the downstream Homebrew workflow, or is available to the workflow. Secret enumeration was explicitly denied (403). I did not test write capability because that would require a mutating dispatch/tag operation, outside this read-only lane.

## Current-main hosted run and cached artifact digests

`gh api repos/joshyorko/rcc/actions/runs/38105340592` returned run 586, `push`, completed/success, exact head `727c1ff8...`. Its jobs were Build RCC success; four Native Runtime jobs success; Release Candidate Verification skipped; Create GitHub Release skipped (expected for a main push). Separate exact-head push runs CodeQL `38105340724`, Coverage gate `38105340564`, and Lint `38105340590` all completed successfully. PatchRaptor RCC Release run `38106400198` completed successfully.

The current run's artifact API metadata matched existing local ZIPs byte-for-byte by SHA-256; no ZIPs were redownloaded:

| Artifact | ID | Size | API SHA-256 | Cached ZIP |
|---|---:|---:|---|---|
| RCC-Binaries | 11690060283 | 64,478,883 | `0233fd07e55753a7c1cf3fa0214f15f28c9c63c32413b172c206824d9c4338ce` | match |
| native-runtime-binaries | 11689755940 | 49,089,276 | `88f406c654de1155e717ded1534fa058ca1c5375007109f2a06364d3a5036ac2` | match |
| linux-amd64-native-runtime-receipt | 11689851536 | 185,396 | `ec8d7cc46646fcfca7a228392e1bc2cb53341d838563802abd7d306c1f07eaba` | match |
| macos-amd64-native-runtime-receipt | 11689987200 | 172,527 | `577c6a6a5f05251654f0c4ee6029a3a08407001ba19fd8e4f2659c1091922eb9` | match |
| macos-arm64-native-runtime-receipt | 11689732116 | 172,216 | `0336c09e5c16717eb0536f5e532b027aa99f115348a7403bec633738f92f7384` | match |
| windows-amd64-native-runtime-receipt | 11689687809 | 174,186 | `45282dbe3c2e6f374757b785e6223e9a4511cd0545f7a88e5bcbe884510ab246` | match |

The cached artifact SHA-256 check was repeated with `sha256sum`; all six values matched the API metadata and the preserved verification receipt at `evidence/hosted-native/final-main-727/root-six-artifact-digest-verification.json`. The existing independent receipt verification establishes exact source/binary binding and native consumer tests; this audit did not re-download or re-extract those bundles.

## Release path and required gates

At this source, `.github/workflows/create-release-tag.yml` is `772fd9225958910707d08d022e2a3d84229437e4407425aaa722304b7b04a0d7`; `.github/workflows/rcc.yaml` is `3d22e6e2ef10d126064c6771d413783109a50d751dd7138044f57aa6e4dc77a1`; `scripts/validate_release_topology.py` is `f952813843d8a787cc662e34f9bc7577740b90391ac953d7529e3770ab886fc0`.

The supported operator entrypoint documented in the preserved handoff is:

```sh
gh workflow run create-release-tag.yml --repo joshyorko/rcc --ref main -f recreate_existing_tag=false
```

**Do not run this as a probe:** it is a write operation. The workflow has `contents: write`; it reads and validates the version, requires `HOMEBREW_TOOLS_PAT`, checks the workflow checkout exactly equals current `origin/main`, then pushes the version tag. A tag push is the trigger for the full release workflow, so this is not a tag-only or dry-run path. `recreate_existing_tag=true` deletes/recreates a tag and must not be used for this proposal.

For a v18.19.6 publication, the workflow graph requires:

1. Refresh and approve the exact main commit/version; require v18.19.6 still absent and source tree unchanged. Existing main CI only has candidate full11 evidence on a different commit with the same tree; main’s own `Release Candidate Verification` is skipped. Do not rebind that receipt.
2. The operator dispatch must succeed, the named PAT must be available to the workflow, and the created `v18.19.6` tag must dereference to approved main.
3. The tag-triggered build must stage the eight named RCC/rccremote assets; the **tag-source** `release-candidate` job must pass the full 11 gates with numeric command result 0 and uploaded full evidence; all four native runtime jobs must pass on that exact tag/source with source-bound receipts/binaries.
4. The `release` job validates the tag/source match, asset names and topology, stable index retention/cap, then creates and publishes a GitHub release containing eight binaries plus `index.json`; its final step dispatches `homebrew-tools/tap-auto-update.yml` through the same PAT.
5. Inspect actual tag run, final assets/digests/index and downstream dispatch result before claiming publication complete.

There is no apparent workflow cycle: tag push triggers the RCC tag workflow; its release waits for build, tag-source full11 and native matrix. PatchRaptor is a separate `workflow_run` path and its current recorded run succeeded.

Signing evidence in current-main CI is platform-specific and incomplete as a universal release claim: macOS ARM reports verified signing; macOS Intel is `unsigned-or-unverified`; Linux and Windows report no signing attestation. The release workflow inventory/topology does not itself establish all-platform signing/notarization. Preserve the recorded scope and obtain actual tag-asset metadata before making a signing claim.

## Decision-relevant blockers

- The operator identity is readable and GitHub reports repository admin/push role, but the Actions settings and secrets endpoints return 403. `HOMEBREW_TOOLS_PAT` presence/readiness and downstream permissions are therefore unverified.
- Dispatching the tag workflow would immediately create/push a tag and trigger full release publication if gates pass. This audit did not exercise that write path; do not infer it will work just from repo role metadata.
- Full11 and four-native receipts bound to `727c1ff8...` do not yet exist. Existing candidate324 full11 receipt is same-tree but different-commit evidence. Obtain exact tag-source full11/native runs and binary provenance before claiming publication acceptance.
- Current release signing evidence does not support a universal signature/notarization statement.

No source, ref, Actions setting, secret, tag, release, or downstream repository was mutated.
