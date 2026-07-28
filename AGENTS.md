# Repository Guidelines

## Project Structure & Module Organization

RCC is a Go CLI for building, caching, and running contained automation environments. Treat this maintained fork as the release source of truth.

- `cmd/` contains Cobra commands; `operations/` implements user-facing workflows.
- `conda/`, `htfs/`, and `remotree/` manage environments, holotree storage, and remote caches.
- `common/`, `settings/`, `pathlib/`, and `shell/` provide shared infrastructure.
- `assets/`, `templates/`, and `docs/` are embedded source inputs. `blobs/` and `build/` are generated; do not hand-edit them.
- `developer/` defines the contained development toolkit. `robot_tests/` holds Robot Framework acceptance tests; Go unit tests live beside source as `*_test.go`.

For historical archaeology, [`admariner/rcc`](https://github.com/admariner/rcc) preserves the public `robocorp/rcc` Git history through its last open-source revision. Use it for lineage and blame, not as this fork’s current release authority.

## Build, Test, and Development Commands

Prefer the checked-in toolkit for a contained development environment:

```sh
rcc run -r developer/toolkit.yaml --dev -t unitTests
rcc run -r developer/toolkit.yaml --dev -t local
rcc run -r developer/toolkit.yaml -t robot
```

With Go 1.26.5, Python 3.10+, and Invoke available:

```sh
inv assets   # regenerate embedded blobs
inv test     # run Go unit tests
inv local    # build build/rcc
inv robot    # run acceptance tests after inv robotsetup
```

Run `inv assets` after changing embedded inputs. Use `inv build` only when cross-platform artifacts are required.

## Coding Style & Naming Conventions

Run `gofmt` on changed Go files. Prefer small functions, table-driven tests, lowercase package names, and conventional Go `CamelCase` identifiers. Follow existing `fail` error handling, `common.Log/Debug/Trace` output, and `hamlet` test assertions. Keep OS-specific behavior in platform-specific files. Never hardcode credentials or service URLs; preserve `RCC_ENDPOINT_*` overrides and disabled telemetry.

## Testing Guidelines

Name Go tests `TestXxx` in `*_test.go`; name acceptance suites `*.robot`. Start with the affected package, for example `GOARCH=amd64 CGO_ENABLED=0 go test ./cmd/...`, then run `inv test`. Use `inv robot` for runtime, environment, or CLI workflow changes. Avoid live network dependencies unless explicitly isolated.

## Commit & Pull Request Guidelines

Recent history follows Conventional Commits: `fix:`, `feat(scope):`, and `chore(scope):`. Keep subjects imperative and commits focused. Search existing issues; open one first for non-trivial work. Target PRs at `main`, link the issue, summarize behavior changes, and list exact verification commands plus relevant logs or before/after CLI output.

## Repository-Local Skills & Durable Learning

Use [`docs/skills/README.md`](docs/skills/README.md) to route recurring RCC development work and [`docs/agent-boundaries.md`](docs/agent-boundaries.md) to determine allowed actions. Apply the RCC development skill for source changes. Load the meta-skill-improvement skill only when evidence reveals stale or missing guidance, the task changes agent guidance, or closure produces durable learning.

Inside this checkout, repository-local guidance is authoritative for RCC implementation and verification. The external `rcc` plugin provides broader cross-repository discovery and orientation, including source-work guidance, but defers to this repository when the two overlap.

Treat verified, reusable knowledge as part of the deliverable. Update existing guidance when evidence proves it incomplete or stale, but do not create session diaries, issue backlogs, speculative rules, or cosmetic documentation churn. A documentation edit is required only when durable knowledge was learned. The documentation receipt remains mandatory for non-trivial work; a no-change receipt is valid.

Close non-trivial work with:

```text
Documentation receipt
- Canonical guidance: <file changed, exact proposed delta, or "no change">
- Durable learning: <reusable fact or "none">
- Evidence: <code, test, command, runtime output, or immutable history>
- Stale guidance removed: <what was replaced or "none">
- Remaining uncertainty: <specific unknown or "none">
```
