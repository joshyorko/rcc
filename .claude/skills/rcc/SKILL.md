---
name: rcc
description: Expert RCC (Repeatable, Contained Code) automation - create, manage, and distribute Python-based self-contained automation packages with isolated environments, work items, and enterprise deployment patterns
allowed-tools: Bash, Read, Write, Edit, Grep, Glob, Task
model: inherit
hooks:
  SessionStart:
    - hooks:
        - type: command
          command: "bash \"$CLAUDE_PROJECT_DIR/.claude/skills/rcc/hooks/setup-skill-env.sh\""
          timeout: 120
  PreToolUse:
    - matcher: "Bash"
      hooks:
        - type: command
          command: "rcc task script -r \"$CLAUDE_PROJECT_DIR/.claude/skills/rcc/robot.yaml\" --silent -- python hooks/validate-rcc-commands.py"
          timeout: 30
  PostToolUse:
    - matcher: "Bash"
      hooks:
        - type: command
          command: "rcc task script -r \"$CLAUDE_PROJECT_DIR/.claude/skills/rcc/robot.yaml\" --silent -- bash hooks/post-rcc-run.sh"
          timeout: 30
---

# RCC Skill - Repeatable, Contained Code

## Skill Environment Setup

This skill includes a pre-built holotree environment (`hololib.zip`) for instant setup:

```bash
# Instant restore (~5 seconds) - RECOMMENDED
rcc ht import .claude/skills/rcc/hololib.zip --silent

# Or build from scratch (~5-10 minutes)
rcc ht vars -r .claude/skills/rcc/robot.yaml --silent
```

The skill's hooks run in this isolated environment via `rcc task script`.

---

**Repository:** https://github.com/joshyorko/rcc (Community Fork)
**Maintainer:** JoshYorko
**Homebrew Tap:** https://github.com/joshyorko/homebrew-tools

RCC is a Go CLI tool for creating, managing, and distributing Python-based self-contained automation packages. It provides isolated Python environments without requiring Python installation on target machines.

## Installation (Homebrew Recommended)

```bash
# macOS & Linux (recommended)
brew install --cask joshyorko/tools/rcc

# Also install Action Server for MCP/AI actions
brew install --cask joshyorko/tools/action-server

# Verify
rcc version
```

See `installation.md` for alternative methods (direct download, Docker, Windows).

## Quick Reference

| Command | Purpose |
|---------|---------|
| `rcc robot init -t <template> -d <dir>` | Create new robot |
| `rcc ht vars -r robot.yaml` | Build/verify environment |
| `rcc run --task "Task Name"` | Run specific task |
| `rcc task shell` | Interactive shell |
| `rcc configure diagnostics` | System check |

## Pro Tip: Use --silent Flag

**Add `--silent` to suppress RCC progress output** and only see task logs:

```bash
# Without --silent: Shows all RCC loading progress
rcc run --task Main
# ####  Progress: 01/15  v18.16.0 ...
# ####  Progress: 02/15  v18.16.0 ...
# (lots of output)

# With --silent: Clean output, only task logs
rcc run --task Main --silent

# Works with all commands
rcc ht vars --silent              # Just env vars, no progress
rcc task script --silent -- cmd   # Just command output
rcc configure diagnostics --silent # Just results
```

## Creating Robots

**IMPORTANT:** After creating a robot, ALWAYS run `rcc ht vars` to pre-build the environment.

```bash
# 1. List available templates
rcc robot init --json

# 2. Create robot with template
rcc robot init -t 01-python -d my-robot

# 3. Pre-build environment (REQUIRED)
rcc ht vars -r my-robot/robot.yaml

# 4. Run the robot
rcc run -r my-robot/robot.yaml
```

**Available Templates:**
- `01-python` - Python Minimal
- `02-python-browser` - Browser automation with Playwright
- `03-python-workitems` - Producer-Consumer pattern
- `04-python-assistant-ai` - Assistant AI Chat

## Best Practice: Always Use UV

**CRITICAL:** Always prefer `uv` over `pip` in conda.yaml for 10-100x faster builds:

```yaml
channels:
  - conda-forge

dependencies:
  - python=3.12.11
  - uv=0.9.28  # Fast package installer
  - pip:
      - requests==2.32.5
```

## robot.yaml Configuration

```yaml
tasks:
  Main:
    shell: python main.py

  Producer:
    shell: python -m robocorp.tasks run tasks.py -t producer

  Consumer:
    shell: python -m robocorp.tasks run tasks.py -t consumer

devTasks:
  Test:
    shell: pytest tests/ -v

  Setup:
    shell: python scripts/setup.py

# Environment files (priority order - first match wins)
environmentConfigs:
  - environment_linux_amd64_freeze.yaml
  - environment_windows_amd64_freeze.yaml
  - conda.yaml  # Fallback

artifactsDir: output

PATH:
  - .
  - bin

PYTHONPATH:
  - .
  - libraries

ignoreFiles:
  - .gitignore

# Pre-run scripts for secrets/private packages
preRunScripts:
  - setup_linux.sh
  - setup_windows.bat
```

## Environment Management

```bash
# Build/rebuild environment
rcc ht vars -r robot.yaml

# Get environment info as JSON
rcc ht vars -r robot.yaml --json

# List all holotree environments
rcc holotree list

# Delete specific space
rcc holotree delete --space my-space

# Interactive shell in environment
rcc task shell

# Run command in environment
rcc task script --silent -- python --version
rcc task script --silent -- pip list
```

## Environment Variables (Automatic)

RCC automatically injects these environment variables when running tasks:

| Variable | Description | Example |
|----------|-------------|---------|
| `ROBOT_ROOT` | Directory containing robot.yaml | `/home/user/my-robot` |
| `ROBOT_ARTIFACTS` | Artifact output directory | `/home/user/my-robot/output` |

**IMPORTANT:** All relative paths in robot.yaml (PATH, PYTHONPATH, artifactsDir) are resolved relative to `ROBOT_ROOT`.

```python
import os

# Access in your code
artifacts_dir = os.environ.get("ROBOT_ARTIFACTS", "output")
robot_root = os.environ.get("ROBOT_ROOT", ".")
```

## Dependency Freezing for Production

**CRITICAL:** Freeze files are **automatically generated** to `ROBOT_ARTIFACTS` (output/) every time you run a robot. They are NOT meant to be committed to the repo.

```bash
# 1. Run robot - freeze file is generated automatically to output/
rcc run --silent

# 2. View the generated freeze file
ls output/environment_*_freeze.yaml
# Output: environment_linux_amd64_freeze.yaml (platform-specific)

# 3. OPTIONAL: Copy to project root ONLY if you want reproducible builds
cp output/environment_*_freeze.yaml .

# 4. Update robot.yaml to prefer freeze files (falls back to conda.yaml)
```

**Freeze File Behavior:**
- Generated automatically on **every** `rcc run` (since v10.3.0)
- Contains exact pinned versions of all dependencies
- Platform-specific (linux_amd64, windows_amd64, darwin_amd64)
- Used for reproducibility when placed in `environmentConfigs`
- **DO NOT** flag "missing freeze files" as an error - they're runtime artifacts

## Holotree: Content-Addressed Environment Storage

RCC uses **Holotree** - a deduplication system that stores Python environments efficiently:

**Key Concepts:**
- **Library (Hololib)**: Content-addressed file store organized by hash. Files are stored once, referenced by content hash (SipHash-128)
- **Catalog**: JSON manifests describing complete environments with byte offsets for path relocation
- **Spaces**: Live working environments populated from catalogs

**Benefits:**
- 50MB binary appearing in 20 environments occupies disk space **exactly once**
- Environment restoration from cache: ~2-10 seconds (vs 5-15 minutes fresh)
- Surgical path relocation - enables environments to work from any location

```bash
# List all holotree environments
rcc holotree list

# Delete specific space
rcc holotree delete --space my-space

# Enable shared holotree (multiple users, same machine)
sudo rcc holotree shared --enable  # Linux/macOS
rcc holotree shared --enable       # Windows (admin)
```

## Robocorp Python Libraries

### robocorp.tasks - Task Decorator

```python
from robocorp.tasks import task

@task
def my_task():
    """Task entry point - discovered and run by RCC."""
    process_data()
```

### robocorp.log - Structured Logging

```python
from robocorp import log

# Basic logging
log.info("Processing started")
log.warn("Rate limit approaching")
log.critical("Connection failed")
log.debug("Detailed info for debugging")

# Configure logging
log.setup_log(
    max_value_repr_size="200k",
    log_level="info",           # Minimum for log.html
    output_log_level="info",    # Minimum for console
)

# Protect sensitive data (auto-redacted)
log.add_sensitive_variable_name("password")
log.add_sensitive_variable_name("secret")
log.add_sensitive_variable_name("api_key")
```

### robocorp.workitems - Producer-Consumer Pattern

```python
from robocorp import workitems
from robocorp.tasks import task

@task
def producer():
    """Create work items for processing."""
    for data in get_data_to_process():
        workitems.outputs.create(payload=data)

@task
def consumer():
    """Process work items with proper error handling."""
    for item in workitems.inputs:
        try:
            process(item.payload)
            item.done()  # Mark successful
        except BusinessError as e:
            # Business error - don't retry
            item.fail(exception_type="BUSINESS", message=str(e))
        except Exception as e:
            # Application error - may retry
            item.fail(exception_type="APPLICATION", message=str(e))
```

**Work Item Methods:**
- `item.payload` - JSON payload data
- `item.files` - List of attached file paths
- `item.done()` - Mark as successfully processed
- `item.fail(exception_type, message)` - Mark as failed
- `workitems.outputs.create(payload, files)` - Create output item

## Self-Contained Bundles

Create single-file executables for distribution:

```bash
# Create bundle
rcc robot bundle --robot robot.yaml --output my-robot.py

# Run bundle on another machine
rcc robot run-from-bundle my-robot.py --task Main
```

## Debugging

```bash
# System diagnostics
rcc configure diagnostics --robot robot.yaml

# Debug output
rcc run --debug

# Trace output (very verbose)
rcc run --trace

# Timeline
rcc run --timeline
```

## Custom Endpoints (Air-Gapped Environments)

```bash
export RCC_ENDPOINT_PYPI="https://pypi.internal.com/simple/"
export RCC_ENDPOINT_CONDA="https://conda.internal.com/"
rcc run
```

## Files in This Skill

- `installation.md` - Installation guide (Homebrew, direct download, Docker)
- `reference.md` - Complete command reference
- `examples.md` - Practical recipes and patterns
- `actions.md` - Sema4ai Actions framework & MCP tools
- `deployment.md` - Docker, RCC Remote, CI/CD patterns
- `workitems.md` - Work items & custom adapters
- `hooks.md` - Claude Code hooks integration
- `templates/` - Reference conda.yaml configs
- `hooks/` - Ready-to-use hook scripts
- `scripts/` - Validation and health check utilities

## Examples

### Create a data processing robot
```
User: Create an RCC robot for data processing
Assistant: [runs: rcc robot init -t 01-python -d data-processor]
          [runs: rcc ht vars -r data-processor/robot.yaml]
          [environment ready]
```

### Debug environment issues
```
User: My RCC environment build is failing
Assistant: [runs: rcc configure diagnostics --robot robot.yaml]
          [runs: rcc task script --silent -- pip list]
          [checks conda.yaml for issues]
```

### Freeze dependencies for production
```
User: Prepare my robot for production
Assistant: [runs: rcc run to generate freeze file]
          [copies environment_*_freeze.yaml to project]
          [updates robot.yaml environmentConfigs]
```
