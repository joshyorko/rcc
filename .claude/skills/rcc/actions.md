# Sema4AI Actions and MCP (Current Patterns)

Use this reference when creating or reviewing Action Packages, MCP tools, and typed action responses.

## What this stack provides

- `sema4ai-action-server`: hosts Python actions and MCP endpoints.
- `sema4ai-actions`: `@action`, `Response`, `ActionError`, `Secret`, `OAuth2Secret`, `Request`.
- `sema4ai-mcp`: `@tool`, `@resource`, `@prompt` decorators for MCP.
- `actions-work-items` (community fork package): drop-in replacement for `robocorp-workitems` API with `actions.work_items` import path.

Action Server exposes:
- `http://localhost:8080/openapi.json`
- `http://localhost:8080/docs`
- `http://localhost:8080/mcp`
- `http://localhost:8080/runs`

## Community fork specifics (`joshyorko/actions`, `community`)

From the `community` branch:
- keeps `sema4ai-actions` and `sema4ai-mcp` for Actions/MCP.
- adds `actions-work-items` as a standalone package for producer-consumer workflows.
- maintains dual-tier frontend build logic (`community` and `enterprise`) with vendored design-system packages for community builds.

Key commands:

```bash
# Build community binary from source
rcc run -r action_server/developer/toolkit.yaml -t community

# Frontend tier build
cd action_server/frontend
inv build-frontend --tier=community
```

## Quick start

```bash
# 1) Install Action Server
pip install sema4ai-action-server

# 2) Bootstrap package
action-server new
cd my-project

# 3) Start server
action-server start
```

You can also run actions directly without Action Server:

```bash
python -m sema4ai.actions run actions.py
python -m sema4ai.actions run . -t my_action --json-input input.json
```

## package.yaml (v2)

Use `package.yaml` for Action Packages. `conda.yaml`/`action-server.yaml` are legacy formats.

```yaml
name: My Action Package
description: Production actions for internal systems
version: 0.1.0
spec-version: v2

dependencies:
  conda-forge:
    - python=3.12.11
    - uv=0.9.28
  pypi:
    - sema4ai-actions=1.6.6
    - sema4ai-mcp=0.0.3
    - pydantic=2.11.7

dev-dependencies:
  pypi:
    - pytest=8.3.3
    - ruff=0.8.6

dev-tasks:
  test: pytest tests
  lint: ruff check .

pythonpath:
  - src

packaging:
  exclude:
    - ./.git/**
    - ./output/**
    - ./**/*.pyc
```

Version note: pins above are current as of 2026-02-07. Recheck PyPI before bumping.

## Action definitions

### Basic action

```python
from sema4ai.actions import action

@action(is_consequential=False)
def summarize_note(text: str) -> str:
    """Summarize a short note.

    Args:
        text: Input note.

    Returns:
        Concise summary.
    """
    return text[:140]
```

### Typed response models (recommended)

Prefer returning typed `Response` subclasses instead of untyped dict payloads.

```python
from typing import Any
from pydantic import Field
from sema4ai.actions import Response, action

class JobSearchResponse(Response[str]):
    run_id: str = ""
    total_jobs: int = 0
    easy_apply_count: int = 0
    filters: dict[str, bool] = Field(default_factory=dict)

class JobDetailsResponse(Response[dict[str, Any]]):
    job_id: str = ""
    company: str = ""
    title: str = ""

@action(is_consequential=False)
def search_jobs(query: str) -> JobSearchResponse:
    """Search jobs and return typed metadata."""
    return JobSearchResponse(
        result=f"Found jobs for: {query}",
        run_id="run_123",
        total_jobs=24,
        easy_apply_count=11,
        filters={"remote": True},
    )
```

Why this pattern is preferred:
- validates fields with Pydantic,
- exposes schemas cleanly to OpenAPI/MCP consumers,
- keeps outputs stable across action calls.

### Expected failures with `ActionError`

If an action returns `Response[...]`, raise `ActionError` for expected business failures.

```python
from sema4ai.actions import ActionError, Response, action

@action(is_consequential=True)
def cancel_run(run_id: str) -> Response[str]:
    if not run_id:
        raise ActionError("run_id is required")
    return Response(result=f"Run {run_id} canceled")
```

## Secrets and request context

### `Secret`

```python
from sema4ai.actions import Secret, action

@action
def call_api(secret: Secret, endpoint: str) -> str:
    token = secret.value
    return f"Calling {endpoint} with token length {len(token)}"
```

### `OAuth2Secret`

```python
from typing import Literal
from sema4ai.actions import OAuth2Secret, action

@action(is_consequential=False)
def read_sheet(
    sheet_id: str,
    google_secret: OAuth2Secret[
        Literal["google"],
        list[
            Literal[
                "https://www.googleapis.com/auth/spreadsheets.readonly",
                "https://www.googleapis.com/auth/drive.readonly",
            ]
        ],
    ],
) -> str:
    return f"Read {sheet_id} using provider={google_secret.provider}"
```

### `Request`

```python
from sema4ai.actions import Request, action

@action
def inspect_request(request: Request) -> dict[str, str | None]:
    return {
        "user_agent": request.headers.get("user-agent"),
        "session_cookie": request.cookies.get("session"),
    }
```

## MCP tools/resources/prompts

Use `sema4ai.mcp` when building MCP-native interfaces.

```python
from sema4ai.mcp import prompt, resource, tool

@tool(read_only_hint=True, destructive_hint=False, idempotent_hint=True, open_world_hint=False)
def get_ticket(ticket_id: str) -> dict[str, str]:
    """Fetch ticket data by id."""
    return {"id": ticket_id, "status": "open"}

@resource("tickets://{ticket_id}")
def ticket_resource(ticket_id: str) -> dict[str, str]:
    """Provide ticket resource payload."""
    return {"id": ticket_id, "summary": "Login issue"}

@prompt
def summarize_ticket(ticket_text: str) -> str:
    """Create prompt text for LLM summarization."""
    return f"Summarize ticket:\n{ticket_text}"
```

## Work items in community builds (`actions-work-items`)

If you are using the community fork's published library (`actions-work-items`, PyPI `0.2.1` as of 2026-02-07), use:

```bash
pip install actions-work-items
```

```python
from actions.work_items import inputs, outputs

for item in inputs:
    with item:
        outputs.create(payload=item.payload)
```

Migration mapping:
- old: `from robocorp import workitems`
- new: `from actions.work_items import inputs, outputs`

Behavior goal is drop-in compatibility for core producer-consumer patterns.

## Multi-package serving pattern

When serving actions from multiple directories, import each into a shared datadir and start with sync disabled.

```bash
action-server import --dir=./actions-a --datadir=./.action-server
action-server import --dir=./actions-b --datadir=./.action-server
action-server start --actions-sync=false --datadir=./.action-server
```

## Practical quality rules

1. Use explicit return types for every action.
2. Prefer typed `Response` subclasses for non-trivial outputs.
3. Keep docstrings in Google style so schemas stay understandable.
4. Mark side-effecting actions with `is_consequential=True`.
5. Keep `package.yaml` pins intentional and date-stamped.
6. Never hardcode secrets; use `Secret`/`OAuth2Secret`.
7. Keep MCP tools small and deterministic unless open-world behavior is intentional.

## Primary references

- https://github.com/Sema4AI/actions
- https://github.com/Sema4AI/actions/blob/master/action_server/docs/guides/00-startup-command-line.md
- https://github.com/Sema4AI/actions/blob/master/action_server/docs/guides/01-package-yaml.md
- https://github.com/Sema4AI/actions/blob/master/action_server/docs/guides/07-secrets.md
- https://github.com/Sema4AI/actions/blob/master/actions/docs/api/sema4ai.actions.md
- https://github.com/Sema4AI/actions/blob/master/mcp/docs/api/sema4ai.mcp.md
