# RCC Agent Boundaries

RCC is environment infrastructure that agents may develop and consume. RCC is not an agent runtime. This file defines repository-side authority and verification boundaries for human-directed agents and external orchestration systems.

## Sources of Authority

Authority is explicit and does not flow from an agent role, model, prompt, label, or maturity score.

| Activity | Repository default | Additional authority required |
| --- | --- | --- |
| Read source, history, issues, and test output | Allowed | None |
| Propose a plan, finding, or documentation delta | Allowed | None |
| Edit the active task's files and run local tests | Allowed when the user requested implementation | User request |
| Create branches, commits, or pull requests | Not implied by implementation | Explicit publish request or controller policy |
| Modify issues, reviews, labels, or repository settings | Not implied | Explicit target and mutation authority |
| Merge, tag, release, publish binaries, or update downstream consumers | Never implied | Explicit release authority |
| Delete holotree/cache data or mutate the Bluefin host | Never implied | Explicit target and destructive-action authority |

Repository instructions constrain an agent but cannot grant GitHub, host, network, credential, or release authority. The orchestrator must enforce those capabilities independently.

## Change Classification

- **Documentation-only:** validate links, skill structure, and `git diff --check`.
- **Go logic:** run focused package tests, then the contained `unitTests` task.
- **Build-sensitive:** build `build/rcc` and exercise that binary.
- **Runtime or environment:** add or run Robot Framework acceptance coverage.
- **Cross-platform:** keep platform code isolated and prove each claimed target independently. Native Linux evidence is not Windows or macOS evidence.
- **Release:** require version, changelog, asset, artifact, and downstream-consumer review as a separate lane.

Unknown or mixed changes use the stricter classification.

Orchestrator configuration, maturity levels, agent rosters, credentials, budgets, knowledge indexing, and mutation policy are owned outside this repository. RCC supplies reviewed repository knowledge and verification commands.

## Required Evidence

Every mutating run reports:

- exact files changed;
- commands and tests run, including skipped or unavailable gates;
- platforms actually exercised;
- source/build/runtime/publish status as separate facts;
- the documentation receipt defined in `AGENTS.md`; and
- remaining uncertainty.

A heartbeat, green unit test, or agent claim is not proof that a user-visible workflow works. Prefer externally observable outcomes such as a built binary's output, Robot Framework evidence, artifact identity, or a GitHub state read after mutation.
