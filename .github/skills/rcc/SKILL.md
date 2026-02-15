---
name: rcc
description: RCC (Repeatable, Contained Code) CLI workflows for creating and running robots, managing holotree environments, bundling/distribution, and editing robot.yaml/conda.yaml. Use when planning or executing RCC commands, troubleshooting environments, configuring work items, or setting up Actions/MCP.
---

# RCC

## Overview
Use RCC to build and run self-contained automation robots with isolated Python environments and holotree caching. Provide command sequences, configuration guidance for `robot.yaml` and `conda.yaml`, and references/templates as needed.

## Core Workflow
1. Create or clone a robot: `rcc robot init --json`, `rcc robot init -t <template> -d <dir>`, or `rcc pull <github-url>`.
2. Pre-build the environment (required): `rcc ht vars -r <dir>/robot.yaml`.
3. Run tasks: `rcc run -r <dir>/robot.yaml --task "<Task>"`; use `--dev` for `devTasks`; add `--silent` for clean output.
4. Inspect or execute in the environment: `rcc task shell`; `rcc task script --silent -- <cmd>`; `rcc ht vars -r robot.yaml --json`.
5. Bundle for distribution: `rcc robot bundle --robot robot.yaml --output my-robot.py`; run with `rcc robot run-from-bundle my-robot.py --task <Task>`.

## Configuration
- Start from `assets/templates/robot.yaml` and `assets/templates/conda.yaml`.
- For human-in-the-loop flows, start from `assets/templates/hitl-assistant/` (producer/consumer + Assistant UI + custom adapter).
- Prefer `uv` in `conda.yaml` for faster installs and pin versions.
- Before updating any `uv` pin, check the latest release on PyPI and update all templates consistently.
- Update `assets/templates/conda*.yaml`, `assets/templates/package.yaml`, and any example/reference snippets that show `uv` so they stay in sync.
- PyPI release page:
```text
https://pypi.org/project/uv/
```
- Use `environmentConfigs` with freeze files before `conda.yaml` for reproducibility.
- Treat `output/environment_*_freeze.yaml` as runtime artifacts; only copy to the project root when intentionally freezing.
- Use `ROBOT_ROOT` and `ROBOT_ARTIFACTS` for path resolution; RCC resolves relative paths from `ROBOT_ROOT`.
- In `robocorp.tasks` runtime, prefer `get_output_dir()` and `get_current_task()` over direct env-var reads; keep `ROBOT_ARTIFACTS`/`ROBOT_ROOT` as fallback for non-task contexts.

## Holotree
- List environments: `rcc holotree list`.
- Delete a space: `rcc holotree delete --space <name>`.
- Enable shared cache (admin): `rcc holotree shared --enable`.

## Troubleshooting
- Run diagnostics: `rcc configure diagnostics`.
- Add `--debug`, `--trace`, or `--timeline` when investigating issues.
- Prefer `--silent` when you only want task output.
- Run `scripts/env_check.py --skip-network` for a quick environment health check.
- Run `scripts/validate_robot.py path/to/robot.yaml` to validate `robot.yaml` and its `conda.yaml` (requires PyYAML).

## References
- `references/reference.md`: complete CLI reference.
- `references/examples.md`: recipes and patterns.
- `references/installation.md`: installation options.
- `references/deployment.md`: CI/CD, Docker, and remote patterns.
- `references/workitems.md`: work item APIs and patterns.
- `references/actions.md`: Actions/MCP framework usage.
- `references/robocorp-python.md`: Robocorp Python libraries overview.
- `references/rpaframework-assistant.md`: RPA Framework Assistant (human-in-the-loop UI).
