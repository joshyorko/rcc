---
name: deadcode-review
description: Turn RCC cross-target Go evidence or a selected reachable symbol into a source-bound simplification brief.
---

# Deadcode simplification review

Use the existing agent session for judgment. The Python tools collect and validate evidence; they do not call a model, create issues, edit source, or launch an implementation agent. A generated evidence report always starts with `review.status = not_performed`.

## 1. Collect evidence

From the repository root, run:

```sh
rcc run -r developer/toolkit.yaml --dev -t deadcode -- \
  --review \
  --output build/deadcode/evidence.txt
```

This writes human-readable evidence, `build/deadcode/evidence.json`, and `build/deadcode/review-template.json`. The default is the full release matrix: `linux/amd64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`. The official pinned `deadcode` analyzer and the Go-aware inventory are built once for the host, then run with explicit target package-loading environments. Focused non-main scopes add `./cmd/rcc` and `./cmd/rccremote` as analyzer entrypoints while `go list` confines reported findings to the requested packages. `CGO_ENABLED=0`, `GOFLAGS` is empty, and build tags are empty unless explicitly supplied. This is static cross-target analysis, not native Windows or macOS execution.

Focus a run only when that helps review; a focused run cannot support removal by itself:

```sh
rcc run -r developer/toolkit.yaml --dev -t deadcode -- \
  --target linux/amd64 --target windows/amd64 \
  --package ./pretty \
  --symbol github.com/joshyorko/rcc/pretty:csi \
  --review --output build/deadcode/evidence.txt
```

- Repeat `--target`, `--package`, and `--symbol` as needed. Use `IMPORT_PATH:FuncOrType.Method` for symbols. Selected reachable functions are included even when absent from analyzer findings.
- `--tags TAGS` is recorded and passed to both Go loading paths. `--summary` hides symbol details only in text, never in JSON. `--format json` makes the primary output structured; `--json PATH` always writes the full evidence record.
- Application reachability and `-test` reachability are separate facts. `test_only` means unreachable in the application but reachable with tests. `unreachable_both` means unreachable in both. A selected declaration absent from one target is `absent`, not reachable or dead. Marker/linkname ambiguity and failed loading are `unknown`.
- Every target records exact commands, environment, diagnostics, and completion state. Skipped and failed targets are unknown. Partial reports remain available and return nonzero for failed selected targets.
- The source fingerprint covers path, type, executable bit, and contents for tracked and untracked non-ignored files, including deletions. Metadata is captured before and after analysis; a change makes the report unstable. Re-run after any source or review-instruction change.

The inventory uses Go syntax and types to record declarations, incoming references, outgoing function uses, exports, generated files, test files, interface obligations, marker candidates, and `go:linkname`. Related unreachable functions are grouped through typed references. Internal edges and outside-group callees are facts to inspect, not automatic proof of an active alternative or a helper's dispensability.

## 2. Review source and write briefs

Copy the template to a new ignored file such as `build/deadcode/reviews.json`. Keep its `source` object unchanged. Choose candidate IDs from `candidates`; a selected `--symbol` also receives a blank entry in `reviews`. Do not change evidence IDs to make a preferred recommendation valid.

Read definitions and callers before recommending anything. Follow Ponytail's ladder: establish need, reuse existing code, prefer the standard library, then choose the smallest contract-preserving change. Check interface and export compatibility, generated sources, build constraints, and platform behavior. Group an abandoned path, name its active alternative, and identify shared helpers that must remain.

Each review object requires:

- `id`, `title`, and `finding_ids` linking one or more evidence records;
- `what_it_does` in ordinary language;
- `observations`, each with source-bound `text` and `evidence_ids` (`F001`, `F001@windows/amd64`, `T:windows/amd64`, or a command ID);
- `active_alternative`, `shared_helpers`, and a `decision`: `retain`, `remove`, `reuse_or_consolidate`, `simplify_in_place`, `repair_or_finish`, or `investigate_further`;
- `benefit`, `affected_callers`, `behavior_to_preserve`, `unresolved_questions`, and `smallest_change`;
- `verification` containing exact `rcc run -r developer/toolkit.yaml --dev -t ...` commands; and
- `requirement` when proposing `repair_or_finish`.

Keep observed facts in `observations`, separate from the recommendation. Do not invent confidence percentages. An unresolved question or investigation decision blocks the implementation handoff. Removal additionally requires a stable full-matrix, untagged `./...` snapshot and no exported-API, generated-code, marker/linkname, or interface-obligation blocker. A reviewed brief is a handoff for separately authorized work, never permission to implement in this read-only workflow.

## 3. Validate freshness and render the handoff

```sh
rcc run -r developer/toolkit.yaml --dev -t deadcodeReview -- \
  --evidence build/deadcode/evidence.json \
  --reviews build/deadcode/reviews.json \
  --output build/deadcode/review.md \
  --json build/deadcode/review.json
```

Text and JSON are rendered from the same validated brief records. The command rejects unknown evidence IDs, non-toolkit verification steps, unsupported removal, missing repair requirements, and source fingerprints that differ from the current checkout. A stale review must be rebound only after rerunning analysis and rechecking every recommendation.

## RCC review cases

Use these to judge reasoning and preservation, not deletion count. Names live here, not in analyzer heuristics:

- **Windows-only use:** `shell.Task.StderrOnly` has a Windows caller in `conda/platform_windows.go`. Linux-only or failed Windows analysis leaves a gap and cannot support removal. Static Windows reachability is not native Windows execution.
- **Parallel archive experiment:** Review `operations/zipper.go:loopExecutor` and `unzipper.Explode` together against active `Extract`. Notice that `loopExecutor` discards `Execute`'s result before suggesting enablement. Preserve error behavior, flattening, worker cleanup, and shared `WriteTarget.execute`/`zipEntryTarget` behavior. A PoC comment or useful-sounding name is not a requirement for parallel extraction; `repair_or_finish` needs an explicit requirement and verification plan, with no runtime-speed claim.
- **Wizard simplification:** Compare `wizard/common.go:memberValidation` with `wizard/create.go:choose`. Consider exact-membership reuse rather than deletion. Preserve default `1`, trimming and input/EOF behavior, empty-candidate behavior, and rejection of `0` and `N+1` with focused tests before refactoring.
- **Formatting convenience:** Review only `pretty/internal.go:csif` against `csi(fmt.Sprintf(...))` and existing callers. Do not claim formatting or `csi` is unused.
- **Test-only adapter:** Explain `buildcoord.ArtifactVerifierFunc` and the duplicate private test adapter. Moving tests to the public adapter can simplify test machinery and exercise that API, but does not prove production use or answer exported-API compatibility.

## Verification

Run the real fixture and review-contract checks inside the toolkit, and validate this guidance after editing it:

```sh
rcc run -r developer/toolkit.yaml --dev -t deadcodeTests
rcc run -r developer/toolkit.yaml --dev -t agentDocs
```

If Go implementation changes, also run affected package tests and `unitTests`. Finish with `git diff --check`. Do not delete or refactor reviewed RCC functions in this workflow.
