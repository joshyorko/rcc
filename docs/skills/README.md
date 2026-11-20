# RCC Repository Skills

This directory contains durable procedures for agents developing RCC itself. Keep `AGENTS.md` concise and use this page to route work into the narrowest relevant skill.

Inside this checkout, repository-local guidance is authoritative for RCC implementation and verification. The external `rcc` plugin provides broader cross-repository discovery and orientation, including source work, and defers to this repository where their guidance overlaps. Repository-wide action limits live in [`docs/agent-boundaries.md`](../agent-boundaries.md).

## Skill Router

- [`rcc-development`](rcc-development/SKILL.md): source orientation, contained development, implementation boundaries, and verification for RCC changes.
- [`deadcode-review`](deadcode-review/SKILL.md): evidence-bound, read-only simplification review of Go code.
- [`meta-skill-improvement`](meta-skill-improvement/SKILL.md): evidence-backed guidance correction and durable-learning capture.
- [`verify-execution-lifecycle`](verify-execution-lifecycle/SKILL.md): artifact admission, strict policy retention, and the shared bounded TLC checker.
- [`verify-authenticated-metadata`](verify-authenticated-metadata/SKILL.md): authenticated monotonic metadata, restart retention, and operator distribution.
- [`verify-build-coordination`](verify-build-coordination/SKILL.md): owner/expiry fencing before first authoritative publication.
- [`verify-materialization`](verify-materialization/SKILL.md): verified-content intent, restore, runtime validation, ready publication and crash retry.
- [`verify-lease-reclamation`](verify-lease-reclamation/SKILL.md): active/ambiguous lease protection, dry-run and ordered reclamation.
- [`verify-process-execution`](verify-process-execution/SKILL.md): executable selection, direct-child supervision, reaping and lease cleanup.
- [`verify-bundle-flow`](verify-bundle-flow/SKILL.md): task result versus output finalization and command result.
- [`verify-provider-recovery`](verify-provider-recovery/SKILL.md): committed journal replay and deferred provider initialization.

These skills partition the artifact execution pipeline; they do not prove all RCC
commands. Each owns a feature map, source bindings, finite model bounds, and drive
recipes. Existing admission/metadata models remain under `docs/formal/`;
new models live inside their owning skills. Run each fixed/broken/restored gate
and use `$statechart` to inspect its view. Cross-process tests are implementation
evidence; model exploration is an abstract safety check, not a proof of Go.
Legacy conda solving/holotree construction (`conda/`, `htfs/`), cloud authentication
(`cloud/`), and legacy robot preparation (`operations/running.go:LoadTaskWithEnvironment`)
are outside these finite models. Treat their behavior as unmodeled, not verified.

Use `rcc-development` for source work. Load `meta-skill-improvement` only when evidence reveals stale or missing guidance, the task changes agent guidance, or closure produces durable learning. The documentation receipt remains mandatory for non-trivial work, and a no-change receipt is valid without loading the meta-skill.

## Maintenance Rules

- Prefer improving an existing skill over adding another.
- Add a skill only after repeated work demonstrates a distinct, reusable procedure.
- Keep operational truth close to the repository and verify it against current code, commands, tests, or runtime output.
- Replace stale guidance instead of preserving contradictory history.
- Do not store issue status, PR history, dates, one-off incidents, or session narratives here.
- Run `rcc run -r developer/toolkit.yaml --dev -t agentDocs` after changing repository-local agent guidance. `inv agentdocs` is the host-tooling equivalent.
