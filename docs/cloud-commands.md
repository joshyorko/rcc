# RCC Cloud Commands

This document provides a complete reference for all RCC cloud commands used to interact with Control Room (Robocorp Control Room or similar automation platforms).

## Command Structure

```bash
rcc cloud [subcommand]
rcc robocorp [subcommand]  # Alias
rcc c [subcommand]         # Alias
```

---

## Authentication & Connection

### `cloud authorize`

Converts API key to valid JWT token for authenticated operations.

```bash
# User-level authorization (15 min default)
rcc cloud authorize --account myaccount --granularity user --minutes 30

# Workspace-level authorization
rcc cloud authorize --account myaccount --granularity workspace --workspace ws-123 --minutes 60
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--minutes`, `-m` | 15 | Token validity in minutes (minimum 15) |
| `--graceperiod` | 5 | Buffer before expiry in minutes (minimum 5) |
| `--granularity`, `-g` | - | Authorization level: `user` or `workspace` |
| `--workspace`, `-w` | - | Workspace ID (required when granularity=workspace) |

**Account setup:** Credentials are stored in `$ROBOCORP_HOME/settings.yaml`

### Authentication Flow

1. **Account Resolution**: Uses `--account` flag or `RCC_CREDENTIALS_ID` environment variable
2. **Token Generation**: JWT token generated from API credentials with specified claims
3. **Token Caching**: Tokens cached locally per account/claims/URL combination
4. **API Requests**: Token passed in `Authorization: Bearer <token>` header

---

## Workspace Commands

### `cloud workspace`

List workspaces or get workspace details with robots and tasks.

```bash
# List all workspaces
rcc cloud workspace --account myaccount

# Get specific workspace tree (shows robots and tasks)
rcc cloud workspace --account myaccount --workspace ws-123

# JSON output
rcc cloud workspace --account myaccount --workspace ws-123 --json
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--workspace`, `-w` | Workspace ID (optional - omit to list all) |
| `--json`, `-j` | Output in JSON format |

### `cloud userinfo`

Query current user information from Control Room.

```bash
rcc cloud userinfo --account myaccount
```

---

## Robot Management

### `cloud new`

Create a new robot in Control Room.

```bash
rcc cloud new --account myaccount --robot my-bot --workspace ws-123
```

**Flags:**

| Flag | Required | Description |
|------|----------|-------------|
| `--robot`, `-r` | Yes | Name for the new robot |
| `--workspace`, `-w` | Yes | Workspace ID to create in |
| `--json`, `-j` | No | Output in JSON format |

### `cloud download`

Download robot as a zip file.

```bash
rcc cloud download --account myaccount --workspace ws-123 --robot robot-456 --zipfile my-bot.zip
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--workspace`, `-w` | - | Source workspace ID (required) |
| `--robot`, `-r` | - | Source robot ID (required) |
| `--zipfile`, `-z` | robot.zip | Output filename |

### `cloud pull`

Download and extract robot to local directory.

```bash
rcc cloud pull --account myaccount --workspace ws-123 --robot robot-456 --directory ./my-bot

# Force overwrite existing files
rcc cloud pull --account myaccount --workspace ws-123 --robot robot-456 --directory ./my-bot --force
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--workspace`, `-w` | Source workspace ID (required) |
| `--robot`, `-r` | Source robot ID (required) |
| `--directory`, `-d` | Target extraction directory (required) |
| `--force`, `-f` | Remove safety checks during unwrapping |

### `cloud push`

Wrap local directory and push to Control Room.

```bash
rcc cloud push --account myaccount --workspace ws-123 --robot robot-456 --directory .

# With ignore patterns
rcc cloud push --account myaccount --workspace ws-123 --robot robot-456 -i .gitignore -i .rccignore
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--directory`, `-d` | . | Local directory to wrap |
| `--workspace`, `-w` | - | Target workspace ID (required) |
| `--robot`, `-r` | - | Target robot ID (required) |
| `--ignore`, `-i` | - | Files with ignore patterns (repeatable) |

### `cloud upload`

Upload existing zip file to Control Room.

```bash
rcc cloud upload --account myaccount --workspace ws-123 --robot robot-456 --zipfile my-bot.zip
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--zipfile`, `-z` | robot.zip | Source zip file |
| `--workspace`, `-w` | - | Target workspace ID (required) |
| `--robot`, `-r` | - | Target robot ID (required) |

### `cloud prepare`

Pre-download and prepare robot environment for fast local startup.

```bash
rcc cloud prepare --account myaccount --workspace ws-123 --robot robot-456 --space user
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--workspace`, `-w` | - | Source workspace ID (required) |
| `--robot`, `-r` | - | Source robot ID (required) |
| `--space`, `-s` | user | Client-specific environment name |

---

## Assistant Commands

> **Note:** Assistant commands are only available in legacy product mode.

### `assistant list`

List robot assistants in a workspace.

```bash
rcc assistant list --workspace ws-123 --account myaccount
```

**Aliases:** `assist`, `a` for command group; `l` for list subcommand

### `assistant run`

Execute a robot assistant from Control Room.

```bash
rcc assistant run --workspace ws-123 --assistant asst-789 --account myaccount

# Copy artifacts to local directory
rcc assistant run --workspace ws-123 --assistant asst-789 --copy ./artifacts
```

**Aliases:** `r` for run subcommand

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--workspace`, `-w` | - | Workspace ID (required) |
| `--assistant`, `-a` | - | Assistant ID to execute (required) |
| `--copy`, `-c` | - | Directory to copy changed artifacts |
| `--space`, `-s` | user | Environment name |

**Execution Flow:**

```
assistant run command
    |
    v
Create cloud client connection
    |
    v
Authenticate using account credentials
    |
    v
StartAssistantRun() --> Get run ID, task zip, artifacts URL
    |
    v
Extract task zip to temporary directory
    |
    v
Load robot.yaml configuration
    |
    v
Create local conda environment
    |
    v
BackgroundAssistantHeartbeat() <-- Keeps run alive
    |
    v
Execute task (robot, Python script, etc.)
    |
    v
Collect artifacts from execution
    |
    v
ArtifactPublisher.Publish() --> Upload to cloud artifact URL
    |
    v
StopAssistantRun(status, reason) --> Complete run
    |
    v
Cleanup temporary files
```

---

## Common Flags

These flags are available across multiple cloud commands:

| Flag | Description |
|------|-------------|
| `--account`, `-a` | Account name from settings.yaml |
| `--workspace`, `-w` | Workspace ID |
| `--robot`, `-r` | Robot ID |
| `--json`, `-j` | JSON output format |
| `--directory`, `-d` | Local directory path |
| `--zipfile`, `-z` | Zip file path (default: robot.zip) |
| `--space`, `-s` | Environment namespace (default: user) |
| `--force`, `-f` | Skip safety checks |

---

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `RCC_CREDENTIALS_ID` | Default account name |
| `RCC_ENDPOINT_CLOUD_API` | Override Cloud API endpoint |
| `RCC_ENDPOINT_CLOUD_UI` | Override UI endpoint |
| `RCC_ENDPOINT_CLOUD_LINKING` | Override linking endpoint |
| `ROBOCORP_HOME` | RCC home directory (contains settings.yaml) |

---

## API Endpoints

| Command | Method | Endpoint |
|---------|--------|----------|
| authorize | POST | `/auth-v1/token` |
| workspace list | GET | `/workspace-v1/workspaces` |
| workspace tree | GET | `/workspace-v1/workspaces/{ws}` |
| userinfo | GET | `/auth-v1/user` |
| new robot | POST | `/robot-v1/workspaces/{ws}/robots` |
| download/pull | GET | `/robot-v1/workspaces/{ws}/robots/{robot}` |
| upload/push | PUT/POST | `/robot-v1/workspaces/{ws}/robots/{robot}` |
| list assistants | GET | `/assistant-v1/workspaces/{ws}/assistants` |
| start assistant | POST | `/assistant-v1/workspaces/{ws}/assistants/{aid}/runs` |
| stop assistant | POST | `/assistant-v1/workspaces/{ws}/assistants/{aid}/runs/{rid}/complete` |
| heartbeat | POST | `/assistant-v1/workspaces/{ws}/assistants/{aid}/runs/{rid}/heartbeat` |

---

## Typical Workflow Example

```bash
# 1. Authorize (get token)
rcc cloud authorize --account prod --granularity user --minutes 60

# 2. List available workspaces
rcc cloud workspace --account prod

# 3. View robots in a workspace
rcc cloud workspace --account prod --workspace ws-123

# 4. Pull robot locally for development
rcc cloud pull --account prod --workspace ws-123 --robot bot-456 --directory ./my-bot

# 5. Make changes locally, then push back
cd my-bot
# ... edit files ...
rcc cloud push --account prod --workspace ws-123 --robot bot-456 --directory .

# 6. Or create a brand new robot
rcc cloud new --account prod --robot new-automation --workspace ws-123
```

---

## Error Handling

Commands use exit codes for error reporting:

| Exit Code | Meaning |
|-----------|---------|
| 1 | Account not found |
| 2 | Client creation failed |
| 3 | API error |
| 4 | Unzip/extraction failed |
| 5 | Artifact upload failed |

---

## See Also

- [Features](features.md) - Overview of RCC capabilities
- [Recipes](recipes.md) - Common usage patterns
- [Troubleshooting](troubleshooting.md) - Common issues and solutions
