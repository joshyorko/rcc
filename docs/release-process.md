# RCC release preparation and publication

RCC releases use two separate operations. A tag push never publishes a release. The `Rcc` workflow's manual preparation path builds and tests an exact `main` commit, then uploads one immutable `rcc-release-preparation` artifact. A maintainer reviews that artifact and its evidence. A separate, manually dispatched `Publish RCC Release` workflow accepts the preparation run ID, full source SHA, version, and asset-manifest SHA-256. It rejects a source that is not in current `main` history, an unsuccessful or incomplete preparation run, a changed archive or asset, an existing tag/release, or missing release-environment protection.

The preparation workflow waits for the cross-platform build, full release-candidate gate, four native runtime receipts, Linux JAT consumer receipt, and assembly job. It packages exactly eight Go binaries and `index.json`, plus the full11 and native evidence. Its manifest and checksums bind each asset's size and SHA-256 to the source SHA, version, and preparation run. Compiler provenance records `go version -m` for each binary and requires Go 1.26.9, expected GOOS/GOARCH, the exact source revision, and a clean VCS build. The publisher downloads the GitHub artifact ZIP through the API, checks the downloaded ZIP digest against GitHub's artifact metadata, rejects unsafe ZIP paths and symlinks, then validates the extracted bundle before making a tag.

Canonical assembly omits the bundle-output Robot fixture's mutable `tmp/output/bundle-output/producer-home` and `tmp/output/bundle-output/homes` trees. It retains their receipts, command logs, immutable replay inputs and all other release evidence; the companion release-candidate artifact retains the complete diagnostic payload. Preparation enforces the publisher's existing expanded limits of 1 GiB per file and 2 GiB total before uploading the canonical bundle. An over-limit preparation must fail before publication approval; do not increase those limits to admit mutable test environments.

For independent review, manual preparation also emits nine auxiliary per-asset CI artifacts: one for each binary and `index.json`. Each contains that asset, the shared manifest, checksum list, provenance, and an identity record tying source SHA, version, run ID, manifest SHA, asset SHA, and byte size together. Each raw payload is capped at 24 MiB. These sidecars are for reviewers who cannot fetch the larger canonical ZIP through their artifact interface; they are not accepted by the publisher and do not change the release asset set. Each native runtime receipt artifact also carries the exact executable used by its runner plus source/platform/version/hash and Go metadata linked to its native receipt, with the same 24 MiB raw-payload cap. The canonical `rcc-release-preparation` bundle remains the publisher's sole artifact input.

Native Environment Artifact acceptance is recorded in `output.xml` beside each
`native-runtime-receipt.json`. Nested reports such as `micromamba/output.xml`
belong to separate suites: preserve them in the canonical preparation, but never
use them as a substitute for the required native acceptance report.

The manual `Export RCC Preparation Review Evidence` workflow is a read-only access path for the larger canonical preparation artifact. It accepts an existing preparation run ID, source SHA, version, and manifest SHA; verifies that the selected source is in the current main history; uses only `actions: read` and `contents: read`; rechecks the exact successful preparation run, all required job results, GitHub's ZIP digest, the prepared bundle, and actual Go 1.26.9 binary metadata; then uploads a source-bound evidence-only sidecar capped at 24 MiB raw. The sidecar includes the original full ZIP digest, asset manifest/checksums/provenance, numeric full11 exit and task receipt, self-host and N-1 receipts, four native runtime receipts and Robot XML, and Linux JAT receipt. It excludes distribution binaries, the N-1 archive, and private home materializations. It does not create a release, tag, or Homebrew dispatch and cannot replace or alter the canonical preparation artifact consumed by the publisher.

Publication requires the `rcc-production` environment to exist and have exactly one required-reviewers rule, at least one reviewer, and admin bypass disabled. Self-review prevention remains the default. The approved sole-maintainer exception applies only to `joshyorko/rcc`, environment `rcc-production`, and exactly one User reviewer `joshyorko` (GitHub user ID 54248591), with `prevent_self_review=false`. Josh may dispatch and manually approve that release; this retains approval but removes a second-person check. The publisher queries that policy and fails closed if it cannot read it.

The workflow passes its automatically issued per-job `GITHUB_TOKEN` through `RCC_RELEASE_POLICY_AUDIT_TOKEN` to the authenticated auditor. [Get an environment](https://docs.github.com/en/rest/deployments/environments#get-an-environment) requires repository **Actions: read**, already granted by this workflow; no separately stored audit credential is required. A read-only job runs the full policy audit before the publication job becomes eligible for manual approval, and the publication job repeats the same audit after approval. The publication job must remain attached to the protected environment; the API checks do not replace its manual approval gate. A missing token, insufficient permission, absent environment, unreadable protection, admin bypass or mismatched sole-maintainer identity blocks publication. Do not substitute another stored credential or treat an unreadable API response as approval.

The publisher also requires a separate `HOMEBREW_TOOLS_PAT` secret that can dispatch `tap-auto-update.yml` in `homebrew-tools`. Its current preflight proves only that the downstream workflow is readable and active; it does not prove the token has dispatch permission. Confirm that access separately before any publication. The repository's configured `GITHUB_TOKEN` must also be allowed to create a new tag and release. The publisher never deletes or replaces a tag or release and does not edit an existing release.

Before dispatching preparation, confirm that the selected `main` commit has the intended `common/version.go` value and a matching `docs/changelog.md` heading. Start `Rcc` with `workflow_dispatch`, selecting the exact current `main` SHA and version. Review the preparation job's full receipts, native receipt ZIPs, `assets/index.json`, `evidence/asset-manifest.json`, `evidence/SHA256SUMS`, `evidence/provenance.json`, and the GitHub artifact's published digest. After independent review, configure the protected environment and required secrets, then dispatch `Publish RCC Release` from `main` with the same run ID, source SHA, version, and manifest SHA. Review the artifact identity in the workflow summary before approving the environment. The workflow's tag creation and release publication occur only after that approval and all preflight checks.

The legacy `Create Release Tag` entry is intentionally disabled and exits with a migration message. Use the protected publisher only after independent artifact review and explicit publication approval. Do not use a release tag as a test trigger.

The historical `recover-rcc-185.yml` workflow is also retired: manual dispatch
only reaches a read-only hold that exits nonzero, with no tag, release, or
Homebrew write. The old `recovery/recover_rcc_185.py` implementation and its
tests are retained as historical evidence, but the workflow no longer invokes
them. `patchraptor-release.yml` is now a manual read-only hold as well. The
PR-only metadata controller and retirement of its direct-main bump/retired-tag
dispatch ship atomically. Neither controller is a publication authorization.

## Release metadata PR controller

`tools/release-metadata` pins Release Please 17.11.2 and its npm integrity lock.
Run `npm ci --prefix tools/release-metadata --ignore-scripts` and
`npm test --prefix tools/release-metadata` to exercise actual released-library
versioning and notes, plus the complete REST lifecycle with a simulated GitHub
transport. The adapter uses Release Please's Go strategy, patch versioning and
generic updater; it never calls its release/tag API. Its own REST write boundary
permits only git objects, the singleton branch, and draft PR creation/text.

The authoritative version baseline is the latest stable **published release's
tag commit**, verified in main history, never a Release Please manifest or
`target_commitish`. Initially only the same version or next patch is accepted.
An existing source-only next-patch heading is preserved, including authored
safety/migration notes and anchors; one marked generated block is replaced.
Historical sections remain byte-for-byte unchanged. The source commit date
supplies a missing heading date, not an unsupported publication claim. Go's
grouped backtick constant retains its v prefix with an inline updater marker.
Minor/major policy overrides are deliberately unsupported and fail closed until
a separately reviewed policy change. Merged branch commits provide linked
source facts; aggregate merge messages are excluded to avoid duplicate notes.

Only `release-please--branches--main` is maintained. Refresh commits fast-forward
that branch with both the prior PR head and current main as parents; they never
force-update it. Main/published identities and branch head are checked again
before ref updates and success receipts. `.release-metadata.json` records the
published baseline, candidate, included commits, base SHA and generated-file
digest. Once merged, an unpublished candidate holds in `PENDING_PUBLICATION`;
new work cannot silently advance it again. Once protected publication creates
that candidate's release, the actual published tag becomes the next baseline.
This hold does not freeze main; later merges are queued for the following
release and do not retroactively qualify an older preparation SHA.

Review the generated draft, amend safety notes if necessary, and merge only with
the normal owner authority and checks. A refresh detects reviewer edits and
fails closed rather than overwriting them. Review/reconcile those edits before
refreshing; a closed unmerged PR similarly requires owner resolution. Crashes
after branch creation can safely retry using its recognized metadata state.
A moved tag, unrelated/truncated history, stale main/head, unrelated branch
files, tag collision, or an active old release run prevents writes.

The `Release metadata` workflow defaults to read-only previews. Automatic PR
writes require repository variable `RCC_RELEASE_METADATA_ENABLED=true`, which
must remain unset until qualification. For live App/CI qualification, an owner
can manually dispatch it on main with `propose=true` while automatic maintenance
remains disabled. This still requires the atomically retired legacy writer and
no queued/running legacy run. Never restart the old controller as fallback.

The owner must install a dedicated GitHub App on **only this repository**, with
Contents write, Pull requests write, and Actions read (to verify legacy runs
are drained), and configure `RCC_RELEASE_APP_ID` and
`RCC_RELEASE_APP_PRIVATE_KEY`. The workflow requests those scoped installation
permissions; there is no PAT or GITHUB_TOKEN write fallback. The App's contents
permission is inherently capable of tag/release writes at GitHub's permission
layer; the tested adapter's API boundary excludes those operations. Verify the
actual App-generated PR receives CI on its exact head before enabling the
variable. Configured secrets or a successful token action alone do not prove
that acceptance. No automatic mode may bypass rcc-production or artifact review.

`release-metadata-receipt` is public metadata/preview evidence, not a release
readiness or publication receipt. Full release preparation remains the existing
manual exact-main operation until its separate automation is qualified.

## Dependency preparation policy

PatchRaptor may open patch PRs for Cobra, Viper, and yaml.v3 without a dashboard
checkbox. Every RCC dependency still requires review and CI before merge;
automerge and platform automerge are disabled, including inherited pin/digest
rules. Other updates, including Go compiler, native modules, Actions, and Python
requirements, require dashboard approval before PR creation. Concurrency remains
one PR. Actions and Python discovery do not authorize a release. Micromamba
automation remains deferred; its archive/executable verification and native
acceptance must continue through the existing reviewed asset workflow.

The Go toolchain group covers `go.mod`, Dagger's compiler default, all three
explicit Rcc CI selectors, publisher/export selectors, and the release verifier's
`EXPECTED_GO_VERSION`. `python scripts/validate_compiler_pins.py` fails if a
selector is missing or differs from `go.mod`. Rcc build CI runs this check and
its negative fixtures. `developer/setup.yaml` intentionally retains its older
Conda bootstrap; Go auto-selection uses `go.mod` for the actual compiler.

The Dependency policy workflow installs Renovate 43.288.0 without lifecycle
scripts and tests resolved inherited presets, rule precedence, compiler
extraction/replacement, and Actions/Python extraction against an immutable
central-policy baseline. To repeat with an installed runner and downloaded
central policy, run `node scripts/test_renovate_policy.mjs <renovate-package-dir>
<inherited-json>`. This offline fit-test is not evidence of a live sweep,
dashboard approval, actual bot-created PR checks, native qualification, or
publication. Keep those gates separate when changing dependency policy.


## Optional read-only readiness receipt

The manual `Export RCC Preparation Review Evidence` workflow accepts an optional
`readiness=true` flag. It first performs its existing canonical ZIP digest,
asset/binary metadata and full evidence audits, then emits a small
`rcc-release-readiness-<run>-<source>` JSON artifact. With the default false,
older main-ancestor preparation review remains unchanged. Readiness is stricter:
the prepared source must still equal current main, its version must be the next
patch after the stable published release, and the candidate tag/release must be
absent. It rechecks main, published tag, preparation attempt, artifact identity
and all successful required jobs before writing the receipt. A rerun, moved
baseline, stale source, changed evidence or collision fails closed without a
readiness output. The readiness script consumes already-audited sidecar bytes
from the preceding exporter step in the same trusted job; invoking it on an
arbitrary locally constructed sidecar does not independently prove canonical
ZIP provenance. Repeated audits use distinct workflow runs/artifact names;
local output files are never overwritten.

The receipt records `READY_FOR_OWNER_REVIEW`, exact source/version, previous
published tag/commit, preparation run/attempt/artifact/ZIP and manifest hashes,
nine asset identities, full11 gates and numeric exits, Robot pass/fail/**skip**
counts, four native receipt hashes, consumer/self-host/N-1 evidence hashes,
limitations, and all four publisher inputs. Publication authorization is always
false with `OWNER_APPROVAL_REQUIRED`; this is not an approval, signature claim,
or substitute for the publisher's original canonical artifact. The workflow
summary links preparation and lists inputs for independent review. It has only
Actions read and Contents read permissions and cannot dispatch preparation,
create tags/releases, or update Homebrew. No second binary build is introduced.

Automating preparation dispatch remains gated on live qualification of the
metadata controller's App-created PR and exact-head CI. Until that acceptance,
use the existing owner-dispatched exact-main preparation, then this optional
readiness audit. The publisher and its protected environment remain unchanged.

## Signing limits

This workflow verifies source, version, target architecture, checksums, and runtime receipts. It does not introduce a new signing or attestation policy and does not claim that every platform binary has a verified publisher signature. Go's Darwin ARM64 linker produces ad hoc code-integrity signatures; those signatures do not establish publisher identity. The build has no vendor-signing step for Darwin AMD64, Windows, or Linux binaries. Review signing requirements and platform-specific signature evidence separately before publication.

## Documentation receipt

- **Guidance added:** manual exact-source preparation, independent artifact review, protected publication, token permissions, and the current signing limits.
- **Review access:** preparation emits nine manifest-bound per-asset sidecars for reviewers with a 32 MiB artifact-download interface cap; native receipt artifacts also include the exact runner binary. These capped sidecars are review-only and never replace the publisher's canonical preparation bundle.
- **Build cleanliness:** generated documentation is committed with its source, and the build task checks tracked files immediately before each Go compilation so a prerequisite cannot silently produce a binary with dirty-source metadata.
- **Durable lesson:** a successful tag-triggered CI run is not a publication authorization; the publisher must consume an independently reviewed preparation artifact and enforce protection before creating a new tag.
- **Evidence:** workflow and helper validation bind the preparation run, ZIP digest, nine asset hashes, compiler metadata, source/version, full11 exit, and four native receipts.
- **Stale guidance replaced:** the old automatic tag-to-release and tag-recreation path is disabled; it must not be used as an alternate route.
- **Uncertainty retained:** downstream dispatch permission and platform-wide signing coverage require separate operator verification.

