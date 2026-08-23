---
name: rcc-development
description: Use when developing and verifying RCC source changes in Josh's maintained fork.
---

# RCC Development

Use this skill when modifying RCC itself. It complements `AGENTS.md`; it does not replace task-specific investigation or the repository's tests.

## When to Use

- RCC Go source, CLI wiring, platform behavior, holotree, cache, or environment-engine changes
- embedded assets, templates, release metadata, or developer-toolkit changes
- local builds and Robot Framework acceptance validation

## When Not to Use

- Using RCC in an automation project without changing this checkout: use the relevant external `rcc` plugin skill.
- Editing a robot's tasks, dependencies, or work items without changing RCC: use the corresponding external operator-facing skill.

The external `rcc` plugin also supports broader cross-repository discovery, orientation, and source work. Inside this checkout, this repository-local skill is authoritative for RCC implementation and verification.

## Orient

1. Confirm the active checkout, branch, and working-tree scope.
2. Preserve unrelated changes. Never reset, stash, or overwrite user work to make the tree clean.
3. Map the smallest relevant surface before editing:
   - CLI wiring: `cmd/`
   - workflows and user-visible behavior: `operations/`
   - environments and packages: `conda/`
   - holotree and caches: `htfs/`, `remotree/`
   - shared behavior: `common/`, `settings/`, `pathlib/`, `shell/`
   - embedded inputs: `assets/`, `templates/`, `docs/`
4. Use `admariner/rcc` only for upstream lineage. This checkout defines current behavior and release direction.

## Implement

1. State the intended behavior, supported platforms, and verification boundary.
2. Make the smallest cohesive change that satisfies the behavior.
3. Follow existing RCC idioms for errors, output, assertions, and platform-specific files.
4. Preserve the fork invariants:
   - keep `RCC_ENDPOINT_*` and `RCC_AUTOUPDATES_*` overrides configurable;
   - treat `ROBOCORP_HOME` as the primary RCC cache and home boundary;
   - keep telemetry disabled;
   - keep tests offline by default and never add unisolated live-network tests; and
   - keep host, container, and CI boundaries explicit.
5. Change embedded source inputs rather than generated `blobs/`, then regenerate assets.
6. Keep version and changelog work separate from feature implementation until the release scope is known.

## Verify

Start narrow and expand in proportion to risk:

```sh
GOARCH=amd64 CGO_ENABLED=0 go test ./path/to/package
rcc run -r developer/toolkit.yaml --dev -t unitTests
rcc run -r developer/toolkit.yaml --dev -t local
rcc run -r developer/toolkit.yaml -t robot
```

- Use Go tests for isolated logic and package behavior.
- Use Robot Framework tests for executable CLI, environment, caching, or cross-process workflows.
- Build `build/rcc` when the user needs a local binary to test.
- Exercise the built binary, not an installed `rcc`, when validating a source change.
- Run asset generation after changing embedded inputs.
- Report exactly what was tested and distinguish source changes, builds, runtime validation, commits, pushes, and releases.

## Review and classify

- Classify the PR using [`docs/change-classification.md`](../../change-classification.md); the highest-risk behavior wins.
- Review the exact submitted head with [`docs/review-rubric.md`](../../review-rubric.md).
- Treat unavailable platform or runtime gates as missing evidence, not implicit success.
- Keep implementation, build, runtime, push, merge, and release status distinct.

## Close

Provide the documentation receipt required by `AGENTS.md`. Load [`meta-skill-improvement`](../meta-skill-improvement/SKILL.md) only when evidence reveals stale or missing guidance, the task changes agent guidance, or closure produces durable learning. A no-change receipt is valid.

## Red Flags

- A Linux-only test is presented as Windows or macOS proof.
- A generated `blobs/` file was hand-edited.
- A host-installed tool silently replaces the contained developer task.
- A unit test is the only proof for a user-visible multi-process workflow.
- A changelog or version bump claims behavior that was not validated.
