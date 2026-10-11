# RCC Repository Skills

This directory contains durable procedures for agents developing RCC itself. Keep `AGENTS.md` concise and use this page to route work into the narrowest relevant skill.

Inside this checkout, repository-local guidance is authoritative for RCC implementation and verification. The external `rcc` plugin provides broader cross-repository discovery and orientation, including source work, and defers to this repository where their guidance overlaps. Repository-wide action limits live in [`docs/agent-boundaries.md`](../agent-boundaries.md).

## Skill Router

- [`rcc-development`](rcc-development/SKILL.md): source orientation, contained development, implementation boundaries, and verification for RCC changes.
- [`meta-skill-improvement`](meta-skill-improvement/SKILL.md): evidence-backed guidance correction and durable-learning capture.

Use `rcc-development` for source work. Load `meta-skill-improvement` only when evidence reveals stale or missing guidance, the task changes agent guidance, or closure produces durable learning. The documentation receipt remains mandatory for non-trivial work, and a no-change receipt is valid without loading the meta-skill.

## Maintenance Rules

- Prefer improving an existing skill over adding another.
- Add a skill only after repeated work demonstrates a distinct, reusable procedure.
- Keep operational truth close to the repository and verify it against current code, commands, tests, or runtime output.
- Replace stale guidance instead of preserving contradictory history.
- Do not store issue status, PR history, dates, one-off incidents, or session narratives here.
- Run `rcc run -r developer/toolkit.yaml --dev -t agentDocs` after changing repository-local agent guidance. `inv agentdocs` is the host-tooling equivalent.
