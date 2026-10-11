# Independent native artifact review: main 7945

Reviewed shipping source `7945f956b428359928908a9f42e36417b113dafd` (`v18.19.6`), for the root-reported manual run `38147141244`. This lane used only the preserved ZIPs and extracted files under `/dev/shm/rcc-main7945-native-review-root`; it did not download, build, execute foreign platform binaries, mutate a source worktree, or rerun hosted CI.

## Artifact verification

I independently recomputed each ZIP SHA-256 and raw size, compared them to the preserved artifact API metadata record (artifact ID, source, version, platform, SHA and size), enumerated exact expected members, rejected unsafe paths/symlink entries, and hashed each ZIP member against the separately preserved extracted file. For every platform, the binary SHA/size matches the review metadata and execution receipt; receipt source/version/platform/GOOS/GOARCH and embedded Go build source/target/clean-VCS fields match 7945. Robot XML reports 1 pass / 0 fail / 0 skip on each platform and its JSON evidence names the same binary SHA.

- `linux_amd64` artifact `11705179118`: ZIP 13386023 bytes / `4c94f47bc09e80eb44b970199aff0142b36834f43d36bc3b760d84ec99ab7f8b`; executable 22392994 bytes / `5a6ae55f33e041e376ca4538c592e7c214b9da30cc6c1cf32cd62a0f65077e21`; all validation booleans pass; Robot 1 pass, 0 fail, 0 skip.
- `darwin_amd64` artifact `11705238385`: ZIP 13072836 bytes / `da0ebc9d866071efbeb203ecea78f665996b415cae2261eefe3319223b200315`; executable 22228928 bytes / `771b06ca9352b84df08507bccb759a115e354a898a78c678127dbdb2d2a1e60c`; all validation booleans pass; Robot 1 pass, 0 fail, 0 skip.
- `darwin_arm64` artifact `11704669558`: ZIP 12498015 bytes / `6b042fd446ead249047c67e16747d4329c340c76ea291b2749dd1e0c6e4f76c5`; executable 20969810 bytes / `4a953e052041082345af2ea120b60dac2f38b2018b6da54c3b23ccd09a5f6001`; all validation booleans pass; Robot 1 pass, 0 fail, 0 skip.
- `windows_amd64` artifact `11704699968`: ZIP 10850198 bytes / `1b9b53cc3bcd9ca0df36a3399410f2fc37c9aa64ac294f697bf07578a655cae5`; executable 20117504 bytes / `cef95bf88055c4a587b06c38fcef5a7edb48cad63202f7bb3238ad7fe03cb895`; all validation booleans pass; Robot 1 pass, 0 fail, 0 skip.

Linux JAT receipt is also present and binds source 7945, the same Linux binary SHA and v18.19.6; its exact CLI receipt includes native `sqlite3` import evidence. The independent verifier result is `pass: true` for all four native artifact bundles.

The available local Go inspection command is Go 1.26.5; its read-only `go version -m` output identifies each embedded compiler as Go 1.26.9 and matches source/target/clean-VCS. Preserved per-platform Go 1.26.9 metadata logs independently match those fields; their SHA-256 values are recorded in `verification.json`. This lane did not run a separate Go 1.26.9 executable.

## Source-input comparison

Comparing Git tree inputs from `727c1ff8b679cdbbe12836e53c87734a1edb0071` to `7945f956b428359928908a9f42e36417b113dafd` found no changed paths among 429 Go files, four `go.mod`/`go.sum` manifests, or the three files under `blobs/assets` and `blobs/docs` used as embedded build inputs. The exact sorted path/blob manifests match; their hashes and changed-path lists are in `verification.json`. Thus this scope shows unchanged Go sources, dependency manifests and embedded RCC assets. Other documentation/workflow/release-helper files did change and are outside this equality claim.

## Signing, Rosetta and security limits

The Intel macOS receipt reports `codeSigning: unsigned-or-unverified`; ARM reports `verified`. Both ran natively (`rosetta: native`) and `translationAllowed: pass`; both explicitly skip `nativeOnlyOnRosetta` as `skipped:native-runner`. These artifacts do not establish Intel signing or Rosetta-translated execution, and they do not prove universal signature coverage. The repository’s release-process documentation states the same signing limitation.

This review does not clear Dependabot findings or rebind previous vulnerability scans to 7945. The known dashboard API 403 leaves the alert inventory unresolved.

## Evidence files

- `verification.json`: per-platform ZIP/member/binary/receipt/Robot/Go checks and source-input manifests.
- `verify_native_artifacts.py`: read-only independent verifier.

SHA-256 hashes for both files are in `SHA256SUMS.txt`.
