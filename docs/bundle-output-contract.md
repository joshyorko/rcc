# Robot Bundle Output Contract

`robot run-from-bundle` runs the selected task in an extracted workarea and
copies its configured `artifactsDir` back to the caller's working directory.
Both source-only and source+`.rcca` bundles preserve the task's stdout and
stderr as separate streams. Unless `--no-outputs` is set, those streams are
also written to `stdout.log` and `stderr.log` in `artifactsDir`.

Artifact handoff runs after the child has exited and been reaped, including on
nonzero exit and cancellation, and before the temporary workarea is removed.
The execution lease ends after child reap; it does not cover the later copy.
Only completed, flushed files present at child reap are eligible for handoff.
Partial task writes are not guaranteed to survive cancellation. Unsafe or
outside-workarea artifact paths, symlinked sources, and symlinked destinations
fail closed before task execution or during safe copy-back.

The wrapper exit code remains 10 for a task that exits nonzero, matching the
existing source-only CLI behavior; the child exit code remains in the failure
diagnostic. `robot run-from-bundle` does not accept `--json`; its stdout belongs
to the task stream.

## Rerunnable CLI Acceptance

From the repository root, build `build/rcc` first. The fixture publishes and
exports a real local `.rcca`, creates both bundle modes, then runs success,
failure, cancellation, denied traversal, stream-capture, and JSON-flag cases.
Artifact preparation can download the pinned Python environment; execution
cases use private RCC homes with package managers offline.

```sh
EVIDENCE_ROOT="${TMPDIR:-/var/tmp}/codex-rcc-bundle-output-20261106-$(date +%s)"
mkdir -p "$EVIDENCE_ROOT"
uv run --offline --no-project --with robotframework==6.1.1 \
  python -m robot -L DEBUG -d "$EVIDENCE_ROOT/robot" \
  robot_tests/bundle_output.robot
```

The Robot output directory retains the generated `fixture.rcca`, source-only
and source+artifact bundles, per-command stdout/stderr/exit files, and a summary
with binary and archive digests. The suite is Linux CLI evidence; it does not
prove native Windows/macOS behavior or a published release.

## Issue #256 Evidence Snapshot

Linux x86_64, Bluefin-derived host; source baseline `f7960076843500d15835b476c07e60d7264027c1`.
The installed published `rcc` was v18.19.8 (SHA-256
`ea8dc96e66d7a81536b03a344b4eaf44bbd94122585954e70290bd466f5de4b6`); the
unchanged-main source build was v18.19.8 (SHA-256
`a5e0c5d982546404b6232ce1209f5fbff488183512f16907025598907f69de91`). Both
returned 0 for the source-only success fixture and 10 for its child-exit-23
fixture. On failure, the caller output directory had no copied result file.

Focused verification passed:

```sh
GOTOOLCHAIN=local GOARCH=amd64 CGO_ENABLED=0 go test ./cmd ./environmentlifecycle
GOTOOLCHAIN=local GOARCH=amd64 CGO_ENABLED=0 go build -o build/ ./cmd/...
```

The dedicated Robot run at
`/var/tmp/codex-rcc-output-20261106-Tf6W1G/robot-run4` passed source-only
success, child-exit-23 failure (outer exit 10), cancellation (outer exit 10),
and denied artifact traversal (exit 7 with no task marker). Its real fixture is
retained at:

- `/var/tmp/codex-rcc-output-20261106-Tf6W1G/robot-run4/bundle-output/fixture.rcca`
  SHA-256 `5854770752c0e93553f5331a1e10c6e8bb562799e3af7d1e2b194ab9dd409f02`.
- `/var/tmp/codex-rcc-output-20261106-Tf6W1G/robot-run4/bundle-output/source-artifact.py`
  SHA-256 `b38eba57c571db8e01e615b88a6b17b510893354ce1384a546f7c211130f538d`.
  The embedded artifact digest is `sha256:230f09e988d4f076e2636b961f044dd5919bdda22115590dd8bce84ef1838970`.
  The source binary used to generate/run this fixture has SHA-256
  `712c29dc6e98e83a0a39b5a4e15673c9f7a4c0f9a1d2adfa8db0cec1f9072902`.

Artifact-mode runtime checks, the JSON-flag case, and the complete Robot result
remain unverified. The coordinator's storage/memory serialization steer stopped
the Robot process during `source-artifact-success` (recorded exit `-15`) to
release its environment acquisition slot. Native Windows/macOS and N-1 runtime
gates were not run. These retained fixtures are for coordinator acceptance.
