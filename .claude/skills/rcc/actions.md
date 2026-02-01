# Sema4ai Actions Framework

Build MCP tools and AI Actions that connect AI agents with the real world.

## Quick Start

```bash
# Build from source (recommended)
git clone https://github.com/joshyorko/actions.git
cd actions
rcc run -r action_server/developer/toolkit.yaml -t community

# Or install from PyPI
pip install sema4ai-action-server

# Create new project
action-server new

# Start server
cd my-project
action-server start  # Available at http://localhost:8080
```

## package.yaml (v2 Spec)

The modern configuration format for Actions:

```yaml
spec-version: v2

name: my-actions-package
description: Custom automation actions

dependencies:
  conda-forge:
    - python=3.12.10
    - uv=0.6.11
  pypi:
    - sema4ai-actions=1.3.15
    - sema4ai-mcp=0.0.1
    - requests>=2.32.0

pythonpath:
  - src
  - tests

dev-dependencies:
  pypi:
    - pytest=8.3.3
    - black=24.10.0

dev-tasks:
  test: pytest tests
  format: black src tests
  lint: ruff check src

packaging:
  exclude:
    - ./.git/**
    - ./output/**
    - ./**/__pycache__
    - ./.venv/**
```

## Creating Actions

### Basic Action

```python
from sema4ai.actions import action

@action
def greet_user(name: str) -> str:
    """
    Greets a user by name

    Args:
        name: The user's name to greet

    Returns:
        A personalized greeting message
    """
    return f"Hello, {name}! Welcome to the automation platform."
```

### Action with Complex Types

```python
from sema4ai.actions import action
from pydantic import BaseModel
from typing import List

class EmailData(BaseModel):
    subject: str
    body: str
    recipients: List[str]

class EmailResult(BaseModel):
    success: bool
    message_id: str | None
    error: str | None

@action
def send_email(email: EmailData) -> EmailResult:
    """
    Send an email through the automation platform

    Args:
        email: Email data including subject, body, and recipients

    Returns:
        Result indicating success or failure
    """
    try:
        # Send email logic here
        return EmailResult(success=True, message_id="msg-123", error=None)
    except Exception as e:
        return EmailResult(success=False, message_id=None, error=str(e))
```

## Creating MCP Tools

MCP (Model Context Protocol) tools enable AI agents to perform actions. Use `sema4ai.mcp` decorators:

### @tool Decorator

```python
from sema4ai.mcp import tool

@tool
def search_documents(query: str, max_results: int = 10) -> str:
    """
    Search through document archive

    Args:
        query: The search query string
        max_results: Maximum number of results to return

    Returns:
        JSON string with search results
    """
    results = perform_search(query, max_results)
    return json.dumps(results)
```

### Tool Hints (Behavior Descriptors)

The `@tool` decorator accepts hints to describe tool behavior:

```python
from sema4ai.mcp import tool

@tool(
    read_only_hint=True,      # Tool only reads data, doesn't modify
    destructive_hint=False,   # Tool doesn't permanently delete/modify
    idempotent_hint=True,     # Same inputs produce same results
    open_world_hint=False     # Tool operates in closed system
)
def get_user_profile(user_id: str) -> dict:
    """Fetch user profile (read-only operation)."""
    return fetch_profile(user_id)

@tool(destructive_hint=True)
def delete_record(record_id: str) -> bool:
    """Delete a record permanently."""
    return perform_delete(record_id)
```

### @prompt and @resource Decorators

```python
from sema4ai.mcp import prompt, resource

@prompt
def generate_summary_prompt(context: str) -> str:
    """Generate a prompt for LLM summarization."""
    return f"Summarize the following: {context}"

@resource
def get_system_status() -> dict:
    """Provide system status data to the LLM."""
    return {"status": "healthy", "uptime": get_uptime()}
```

### Full MCP Example

```python
from sema4ai.mcp import tool, prompt, resource
import json

@tool
def assign_ticket(ticket_id: str, user_id: str) -> bool:
    """
    Assign a ticket to a user.

    Args:
        ticket_id: The ID of the ticket to assign
        user_id: The ID of the user to assign the ticket to

    Returns:
        True if successful, False otherwise
    """
    # Ticket assignment logic
    return True

@resource
def get_open_tickets() -> str:
    """Get all open tickets as JSON."""
    tickets = fetch_open_tickets()
    return json.dumps(tickets)

@prompt
def ticket_summary_prompt(ticket_data: str) -> str:
    """Generate prompt for ticket summarization."""
    return f"Summarize this ticket: {ticket_data}"
```

## Running the Action Server

```bash
# Start with default settings
action-server start

# Custom port
action-server start --port 9000

# With specific actions directory
action-server start --actions-dir ./my_actions

# Development mode (auto-reload)
action-server start --reload
```

**Endpoints:**
- UI: `http://localhost:8080`
- API: `http://localhost:8080/api/actions`
- MCP: `http://localhost:8080/mcp`
- OpenAPI: `http://localhost:8080/openapi.json`

## Integration with Claude Code

Configure the action server as an MCP server in `.mcp.json`:

```json
{
  "mcpServers": {
    "my-actions": {
      "type": "http",
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

## Action Secrets

Handle sensitive configuration securely:

```python
from sema4ai.actions import action, Secret

@action
def fetch_data(api_key: Secret, endpoint: str) -> str:
    """
    Fetch data from external API

    Args:
        api_key: API key for authentication (handled securely)
        endpoint: API endpoint to call

    Returns:
        Response data from the API
    """
    headers = {"Authorization": f"Bearer {api_key.value}"}
    response = requests.get(endpoint, headers=headers)
    return response.text
```

## OAuth2 Integration (Typed Scopes)

Sema4ai Actions supports typed OAuth2 secrets with explicit provider and scope definitions:

```python
from typing import Literal
from sema4ai.actions import OAuth2Secret, action

@action
def read_google_spreadsheet(
    name: str,
    google_secret: OAuth2Secret[
        Literal["google"],  # Provider
        list[
            Literal[
                "https://www.googleapis.com/auth/spreadsheets.readonly",
                "https://www.googleapis.com/auth/drive.readonly",
            ]
        ],  # Required scopes
    ],
) -> str:
    """
    Read data from a Google Spreadsheet.

    Args:
        name: Spreadsheet name or ID
        google_secret: Google OAuth2 credentials (auto-managed)

    Returns:
        Spreadsheet data as JSON string
    """
    headers = {"Authorization": f"Bearer {google_secret.access_token}"}
    # Use Google Sheets API
    return fetch_spreadsheet(name, headers)
```

**OAuth2 Secret Payload Structure (sent by action server):**
```json
{
  "google_secret": {
    "provider": "google",
    "scopes": ["https://www.googleapis.com/auth/spreadsheets.readonly"],
    "access_token": "<managed-access-token>",
    "metadata": { "any": "additional info" }
  }
}
```

## Project Structure

```
my-actions/
├── package.yaml           # Dependencies and config
├── actions/
│   ├── __init__.py
│   ├── email_actions.py   # Email-related actions
│   ├── file_actions.py    # File processing actions
│   └── api_actions.py     # External API actions
├── tests/
│   ├── test_email.py
│   └── test_files.py
└── output/                # Generated artifacts
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Test Actions

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install RCC
        run: |
          curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
          chmod +x rcc && sudo mv rcc /usr/local/bin/

      - name: Run tests
        run: rcc run -t test

      - name: Lint
        run: rcc run -t lint
```

## Best Practices

1. **Type Hints**: Always use type hints for action parameters and returns
2. **Docstrings**: Write clear docstrings - they become action descriptions
3. **Pydantic Models**: Use Pydantic for complex input/output types
4. **Error Handling**: Return structured error responses, don't raise exceptions
5. **Secrets**: Never hardcode credentials, use Secret types
6. **Testing**: Write tests for all actions
7. **Versioning**: Pin dependency versions in package.yaml
