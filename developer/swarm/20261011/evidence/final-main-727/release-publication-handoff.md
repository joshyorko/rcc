# Prepared RCC v18.19.6 publication handoff

Prepared source: main `727c1ff8b679cdbbe12836e53c87734a1edb0071`; tree `cfae5c5ee9df0e5031e8e9ae37040b5c98ac6152`, equal to accepted candidate324. No tag/release was created. All four current-main native jobs and the four required push workflows passed, with actual receipts/binaries independently verified by root at 02:50 UTC. This is a reviewable publication proposal, not tagged-source/publication acceptance.

## Authority and callable path

Repository `docs/agent-boundaries.md` requires **“Merge, tag, release, publish binaries, or update downstream consumers | Never implied | Explicit release authority.”** The user explicitly authorized safe accepted merges; no separate publication instruction is recorded. Current GitHub connector capabilities do not expose tag creation, workflow dispatch or release publication, and gh authentication is expired. Do not recover/copy connector credentials or use branch-ref APIs as tag workarounds.

The existing `Create Release Tag` workflow on main reads common/version.go, validates HOMEBREW_TOOLS_PAT, asserts checked-out HEAD equals origin/main and refuses an existing tag by default. It pushes v18.19.6, which automatically runs the Rcc tag workflow; successful tag build/full11/native acceptance triggers draft assets, topology/tag validation, then public release and homebrew-tools/tap-auto-update.yml dispatch. This is not a tag-only test path. No Actions repository branch/state mutation is part of this path.

Once release authority and required gates are satisfied, the authorized operator may run the repository workflow with main still pinned to727:

```sh
gh workflow run create-release-tag.yml --repo joshyorko/rcc --ref main -f recreate_existing_tag=false
```

Root did not execute this command. Refresh main/tag/release state immediately before any action; if main moved, re-evaluate exact-source acceptance. Never recreate an existing tag. Secret presence/access is not independently verified.

## Exact remaining gates

- Completed: current-main Rcc38105340592, four native jobs and CodeQL/Coverage/Lint passed. Root independently bound all four actual receipt ZIPs and binary hashes to727, including native/Robot and Linux JAT acceptance. Local CGO fullGo and116scripts pass. Main full11 job skips by design; successful full11 receipt remains source324/sameTree.
- Document security disposition within actual scope: default-tag four-target ancestor097 scans have no call-traced symbols on unchanged implementation/module graph; known GO-2026-5970/6629 package/module findings are not fixed, private Dependabot access403/unmapped alerts are not cleared, seven unchanged vet findings stay allowed.
- Obtain the separate explicit publication authority and callable release workflow/authentication path.
- Validate actual v18.19.6 tag dereferences to approved source727, then actual tag-source full11/numeric0 and four native/binary provenance receipts. Never relabel candidate receipts as tagged-source evidence.
- Validate actual published eight binary assets plus index.json, SHA256/digests, versions/platform naming, index retention and recorded signing scope. Current simulations are local only. macOSARM signing reports verified; IntelmacOS unsigned-or-unverified; authenticated publisher identity/notarization is not attested. No new mandatory signing policy is invented.
- Confirm publication/distribution/Homebrew outcome using actual workflow results and assets. The immutable Actions opt-in adapter still pinsv18.19.3; retain version3 in distribution index and do not claim universal Actionsv6 acceptance or update that swarm.

## Accepted candidate facts and limits

Candidate324 sixchecks/4native/full11 numericgate+tee0, Robot171/0/1, sixcoordination scenarios,2GiB64KiB streaming/118752-byte Goheapdelta(notRSS), N1releasedv5 imported/consumed archive9ed01cbfe2463f0f064ab10efaf61e8e48a9c96767e7b6aa0629e4e8295bcb85. Root independently checked5045stored/decompressed archive object hashes, closure andManifestidentity. The uploaded rollback receipt omits its private post-acquire snapshot, so an exact provisional-removal count cannot independently be reconstructed; the gate's internalvalidator passed. Source324 fullgate actualbinary8e362301a33092fc04d7b8b37697faf34033c7350a4cac529003d045b6f27daf is distinct from final-main staged binaries below.

Actual stable5 indexasset549249378 SHA256fbf3f53357f40463790a9f6b6bd6c9f5e8fcd69ad158831a6620d5d149f4f4de was independently verified against GitHubdigest. Source727 generator preserves19 prior entries unchanged plus6 under existing20cap; stable5/Actions3 retained, oldestv18.12.0 drops from the listing. No assets deleted.

## Current staged CI binaries (not published assets)

| File | Actual SHA256 |
|---|---|
| `rcc-linux64` | `be55dadc391ae4ac7dd38356755a1698c595fdf41bd4a0141f8df7cc9d29f00e` |
| `rcc-macos64` | `866527e2c4f21771d57b4707a1b3c2ad19bb0e7254e2f0ac27feb0c5c7530a5a` |
| `rcc-macosarm64` | `b216a96f99bb302ca370c08290ce61480b96af005867a925448182deeeef038b` |
| `rcc-windows64.exe` | `ade9c2cb34379b8a279056d093f02e110fc311e7f4f9e33cf3565bc8c7b66c39` |
| `rccremote-linux64` | `51e73325e6f6a032fdd733de6de4f8b20faa7a4ebfc38aa81b7cc0894303b094` |
| `rccremote-macos64` | `dec400e169164f05ce45002892e0f2892a8fa5d6abe0a034e99e8fb8461322c9` |
| `rccremote-macosarm64` | `41663642bf9cf737ee8da4678b8e244e213e8281f1aa1095bd8f83025b810e6d` |
| `rccremote-windows64.exe` | `cbafc85815627c021df993c50119eae048ba4031f802a767d80a77696243c33b` |

All eight unique files and four runtime bundle copies report Go1.26.9, vcs.revision727, vcs.modified=false. A future tag run may yield different ZIP or binary bytes; verify its actual assets independently.

Documentation receipt
- Canonical guidance: no repository product guidance change in this evidence handoff.
- Durable learning: release workflow publishes automatically after tag gates; version index has existing20entrycap; script suite requires build/rcc in one existing fixture.
- Evidence: actual source727 workflow, numerical source324/full-main results, actual GitHub-digested bundle/index bytes.
- Stale guidance removed: historical sources are kept explicitly separate.
- Remaining uncertainty: private alert inventory, publication authority/tool path, tagged/published artifacts and publisher identity. Performance research and actual Actions workloads remain separate from release publication prerequisites.
