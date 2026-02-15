# Robocorp Python Libraries (`robocorp.*`)

Use this reference for Python task packages built on the Robocorp library stack.

## Choose the right layer

- `robocorp.*`: Python task automation (`robocorp.tasks`, `workitems`, `vault`, etc.).
- `sema4ai-actions` + `sema4ai-action-server`: AI Actions and MCP-serving runtime.
- `actions-work-items`: community fork drop-in replacement for `robocorp-workitems` style flows (`actions.work_items` import path).
- `rpaframework` (`RPA.*`): Robot Framework oriented libraries and keywords.

## Core package set

Install either the metapackage or individual packages.

- Metapackage: `robocorp`
- Included core libs: `robocorp.tasks`, `robocorp.log`, `robocorp.workitems`, `robocorp.vault`, `robocorp.storage`
- Common add-on libs (not included by default): `robocorp.browser`, `robocorp.windows`, `robocorp.excel`

Version snapshot (PyPI, 2026-02-07):
- `robocorp=3.0.0`
- `robocorp-tasks=4.0.0`
- `robocorp-workitems=1.4.7`
- `robocorp-vault=1.3.9`
- `robocorp-storage=1.0.5`
- `robocorp-browser=2.3.5`
- `robocorp-windows=1.0.4`
- `robocorp-excel=0.4.5`

## Minimal task + work items pattern

```python
from robocorp import log, workitems
from robocorp.tasks import task

@task
def process_queue() -> None:
    log.info("Starting queue processing")

    for item in workitems.inputs:
        with item:
            payload = item.payload
            log.info("Processing one input item")
            workitems.outputs.create({"processed": True, "source": payload})
```

## Dependency placement in RCC `conda.yaml`

```yaml
dependencies:
  - python=3.12.11
  - uv=0.9.28
  - pip:
      - robocorp==3.0.0
      - robocorp-browser==2.3.5
```

Use explicit add-on dependencies for any package not covered by `robocorp` metapackage.

## Typical imports by capability

- Entry points: `from robocorp.tasks import task`
- Task runtime helpers: `from robocorp.tasks import get_output_dir, get_current_task`
- Logging: `from robocorp import log`
- Queues: `from robocorp import workitems`
- Secrets: `from robocorp import vault`
- Assets: `from robocorp import storage`
- Browser automation: `from robocorp import browser`
- Windows desktop: `from robocorp import windows`
- Excel files: `from robocorp import excel`

## Task runtime helpers (`robocorp.tasks` >= 4.0.0)

```python
import os
from pathlib import Path

from robocorp.tasks import get_current_task, get_output_dir

def resolve_output_dir() -> Path:
    output = get_output_dir()
    if output is not None:
        return output.resolve()
    # Fallback for execution outside robocorp.tasks.
    return Path(os.environ.get("ROBOT_ARTIFACTS", "output")).resolve()

def current_task_name() -> str:
    current = get_current_task()
    return current.name if current is not None else "<outside-task>"
```

## Practical guidance

1. Prefer `robocorp.tasks` for Python-first task entry points.
2. Prefer `get_output_dir()` for artifact paths and `get_current_task()` for task-aware logging.
3. Keep `ROBOT_ARTIFACTS`/`ROBOT_ROOT` fallback logic only for outside-task contexts.
4. Use `workitems.inputs`/`workitems.outputs` for queue-based producer-consumer flow.
5. Pull credentials from Vault, not environment literals in code.
6. Add non-metapackage dependencies explicitly.
7. Keep `robocorp.*` and `RPA.*` dependencies separate and intentional.

## Community fork note (`actions-work-items`)

If you are standardizing on `joshyorko/actions` `community`:
- package: `actions-work-items` (PyPI `0.2.1` as of 2026-02-07)
- import style: `from actions.work_items import inputs, outputs`
- migration from classic API:
  - before: `from robocorp import workitems`
  - after: `from actions.work_items import inputs, outputs`

## References

- https://github.com/robocorp/robocorp
- https://robocorp.com/docs/python
- https://rpaframework.org/
