# Task 5 report

Status: complete on Linux amd64 (Bluefin/Dudley OS).

Commit: `bd43ae73e0bbdbfa51af52f7b868856632fe4876`

Implemented the bounded named-provider acceptance in `robot_tests/environment_artifacts.robot`:

- registers `office` independently in A and B homes with `RCC_TEST_PROVIDER_AUTHORIZATION`;
- structurally validates each `provider add --json` result;
- publishes and cold-acquires through `--provider office`;
- stops the provider, removes B's authorization variable, and warm-acquires through `--provider office`;
- preserves exact artifact/materialization/path identity and local/offline/package-manager invariants.

Documentation covers the shipped provider lifecycle and exact URL, authorization-header, redirect, local-root, warm-reuse, direct-URL, artifact-identity, and v18 `rccremote` compatibility contracts in `README.md`, `docs/holotree.md`, and `docs/changelog.md`.

Verification:

- `rcc run -r developer/toolkit.yaml --dev -t artifactFocused` — PASS (Go packages and 31 Python checks).
- `rcc run -r developer/toolkit.yaml --dev -t artifactVertical` — PASS (`TestRealCurrentRCCAtoBVertical`).
- `rcc run -r developer/toolkit.yaml --dev -t artifactRobot` — PASS (1 Robot test, 1 passed).
- `git diff --check` — PASS.

The requested `--dryrun` preflight could not run: installed RCC v18.18.1 rejects `--dryrun` as an unknown flag. `agentDocs` was not run because repository-local agent guidance was unchanged. The requested legacy-focused Go command was not run in this task lane; `artifactFocused` included `cmd/rccremote` tests.

Documentation receipt

- Canonical guidance: README.md, docs/holotree.md, docs/changelog.md; added exact shipped provider contracts.
- Durable learning: named profiles are independently persisted under each `ROBOCORP_HOME` and remain usable for local warm acquisition without provider authorization.
- Evidence: bounded focused, vertical, and Robot tasks all passed.
- Stale guidance removed: none.
- Remaining uncertainty: none for the Linux acceptance path; `--dryrun` is unavailable in installed RCC.
