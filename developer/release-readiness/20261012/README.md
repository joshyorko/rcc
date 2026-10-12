# RCC v18.19.6 release readiness

Publication remains HOLD. This is retained source and release evidence, not a publication authorization. Josh's later approval authorizes continuing toward release after required checks, but the final new candidate and manifest still require the exact-tuple review.

The task started from clean `work` in `/workspace/rcc` at `7d17122436584dcbfe9b8afddeb56d7c81a10af7`, identical to remote main. There were no existing worktrees or user changes. The selected RCC environment became ready before inspection. Requested owner routing was GPT-6.1 Sol/high; runtime owner routing was not independently inspectable. Exactly two bounded GPT-6 Luna/high helpers implemented and cross-reviewed the changes in separate worktrees. No prior completed root was resumed.

The finite campaign covers worker Scale/Sync/cleanup, original lint findings, command/operation failure behavior, import-time home permissions, ledger shipping/security/release items, accepted integration, and one exact-main preparation. It excludes Holotree optimization, private prototype promotion, Actions source/pins, drafts224/225, formats, FUSE, packs, zstd, and default materializers. Historical Hive issues remain closed.

Merged and remotely read back:

| PR | Accepted head | Merge |
| --- | --- | --- |
| #243 pip listing | `56cab486cd70865c9ca73bc66da9d756086e12c4` | `3aa418395b60165613af98688580b627fb843623` |
| #242 ancestor modes | `3132e089f575260fb8104e365cdd37f3a5d6a799` | `f965f12acf19f333dca82a9a5dd6887e45c43a61` |
| #241 diagnostics/cache/workers | `5776739f151cb04282c333c5ba6ac29f54d805ef` | `9312f283068c0c5c259370b1bc8bd9065970225b` |

Each merged head has independent source review, focused and full Go verification, zero newly introduced lint findings, successful current hosted checks, and four actual downloaded native ZIP/binary/lifecycle/Robot receipts. Native Robot results were 1 pass, 0 failures, 0 skips per platform; Linux JAT receipts also passed the checked lifecycle validator. The integrated unit suite passed on main9312 with unchanged required Linux confinement tests executing outside the nested command sandbox.

Pending integration at this checkpoint:

- #244 network response/TLS cleanup, exact head `03eed7451c475c644d15ea49a781f1635f3db9d8`, independently accepted. The exact vet allowance narrows from seven to six after resolving the TLS timer leak; its regression rejects reintroduction. Native Linux, Windows and ARM passed; macOS x86 remains pending.
- #245 release notes/signing scope, exact head `df022c1eb8cd5106fe9d12ca3ddacc67343cf411`, independently accepted. Native Linux, Windows and ARM passed; macOS x86 remains pending. Merge after #244.
- Start exactly one `Rcc` manual preparation from the final exact main SHA with version v18.19.6 after both merges. Reverify all seven preparation jobs, full11, four native receipts, N-1 rollback, all nine assets, actual Go metadata and canonical ZIP/manifest digest. Refresh source security analysis on that exact source.

The original frozen preparation remains preserved. It is source `7945f956b428359928908a9f42e36417b113dafd`, run `38147141244`, canonical artifact `11705567118`, ZIP SHA-256 `75919093aaeb1a49bfa7898246b4bd8d2e56ea065db837a391fb0ec83d78c7f5`, manifest SHA-256 `26a47203ec1375e2c60d362c5aa55ba0aa65e334c1b24bc07b16f973a585f62c`. The actual canonical ZIP and review exporter sidecar were downloaded and independently rehashed/validated. The sidecar run is `38155612008`, artifact11708177941, ZIP SHA-256 `7c54e6bcc13f7a62c2c85ac62e02edbb33731215d858e9b8ea24168501c30125`. This old tuple is comparison evidence, not the corrected candidate's publication input.

`evidence/prerequisites.json` records the external boundary. Environment reads404; administration/protection/workflow/secret/security reads403. No credentials or security-sensitive settings were changed. The current push banner still reports 15 security alerts whose details cannot be read. Four-target source scans found affected x/text module/package inventory entries but no called affected symbol; they do not establish blanket security clearance. Actual signing inspection verifies ad hoc ARM code pages, without publisher identity, and absent vendor signatures on the other targets.

The next operator step is administrator release preflight: verify protected rcc-production with a different human reviewer, self-review prevented and admin bypass disabled; verify the audit token, publication contents-write and Homebrew dispatch permissions; obtain current security dispositions and decide signing scope. Then Josh reviews the final exact candidate/manifest tuple. No tag, release, publisher dispatch or Homebrew update has occurred.

Documentation receipt: release guidance and changelog updates are in pending #245; structural guidance validation and 52 focused Python tests passed. Stale publisher-proposal wording is removed, and ad hoc integrity is distinguished from publisher identity. Source, build, runtime, integration and publication facts remain separate.

The verifiers accept retained GitHub artifact metadata and actual ZIP bytes. Distribution binaries and ZIPs stay in `/workspace/rcc-mission/evidence` or the original immutable GitHub artifacts; this branch retains compact hashes, metadata, reports and raw source-scan/test evidence. Do not merge this evidence branch wholesale into main.
