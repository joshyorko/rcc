# Independent review: release review sidecars

Reviewed frozen source commit `29c3cfdcc89591768a7204ec594414be0ce846f4` (tree `9bd919e7e68caa237052f354f49c03c5e9e01691`), parent `bb2759e3fe9b2121e1ce8134a028d7d27d4d1936`. Worktree status at capture: `## codex/rcc-two-phase-release-20261011`. This review applies only to this exact SHA; parent hosted checks are not rebound to it.

## Result

No blocker found in the five-file delta. Manual preparation stages nine per-asset review artifacts after the canonical preparation artifact; upload is confined to the main-branch `workflow_dispatch` assembly job. The publisher still consumes only `rcc-release-preparation`.

The sidecar validator binds source SHA, version, preparation run ID, manifest digest, exact nine-name set, exact directory/file layout, checksum list, byte hashes, and sizes. Symlinks/nonregular payload entries are rejected and each payload has a 24 MiB raw-byte cap. Native staging correctly maps displayed macOS jobs to Go `darwin` targets and validates the exact executed binary against the receipt hash/version/runtime platform plus Go source/target/clean-VCS metadata. The added `native-binary-review` directory is a sibling of the native receipt and Robot XML under the downloaded per-platform artifact root; existing recursive receipt/XML lookup remains valid.

## Verification

- `python -m unittest scripts.test_release_publication scripts.test_environment_artifact_tasks`: 80 tests passed. `invoke==2.2.0` was isolated under `/tmp/rcc-review-pydeps`; one existing mock-gate test required a temporary ignored `build/rcc` fixture, removed on completion.
- Workflow YAML parsed successfully: 4 jobs.
- `git diff --check HEAD^ HEAD`: exit 0.
- Manual negative checks are recorded in `negative-cases.txt`.

See `test-yaml-diff.log` for the captured test/YAML/diff output. SHA-256 values for reviewed source files are recorded in `verification.json`.
