# RCC Command Reference

Complete reference for all RCC commands and options.

## Table of Contents

- [Global Flags](#global-flags)
- [rcc run](#rcc-run)
- [rcc create](#rcc-create)
- [rcc pull](#rcc-pull)
- [rcc task](#rcc-task)
- [rcc robot](#rcc-robot)
- [rcc holotree](#rcc-holotree)
- [rcc configure](#rcc-configure)
- [rcc docs](#rcc-docs)
- [rcc cloud](#rcc-cloud)
- [rcc venv](#rcc-venv)

---

## Global Flags

Available on all commands:

| Flag | Description |
|------|-------------|
| `--debug` | Enable debug output |
| `--trace` | Enable trace output (more verbose than debug) |
| `--silent` | **Suppress RCC progress output** - shows only task/command output |
| `--timeline` | Show execution timeline |
| `--pprof <file>` | Write profiling data to file |
| `--robocorp` | Use Robocorp product family settings |

### Recommended: Always Use --silent

The `--silent` flag is highly recommended for cleaner output:

```bash
# Noisy (default) - shows all RCC progress
rcc run --task Main
# ####  Progress: 01/15  v18.16.0 ...
# ####  Progress: 02/15  v18.16.0 ...

# Clean - only shows your task's output
rcc run --task Main --silent

# Useful for all commands
rcc ht vars --silent           # Just env vars export statements
rcc task script --silent -- python script.py  # Just script output
rcc configure diagnostics --silent  # Just diagnostic results
```

---

## rcc run

Run a robot task in an isolated environment.

```bash
rcc run [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--robot` | `-r` | Path to robot.yaml (default: ./robot.yaml) |
| `--task` | `-t` | Task name to run |
| `--space` | `-s` | Holotree space name |
| `--controller` | `-c` | Controller identity |
| `--interactive` | `-i` | Enable interactive mode |
| `--dev` | | Run devTasks instead of tasks |
| `--no-build` | | Don't build environment if missing |
| `--timeline` | | Show execution timeline |

### Examples

```bash
# Run default task
rcc run

# Run specific task
rcc run --task "Process Data"

# Run with custom robot.yaml location
rcc run --robot path/to/robot.yaml

# Run in named space
rcc run --space my-space

# Run development task
rcc run --dev --task "Editor setup"

# Pass arguments to robot (after --)
rcc run --task scripting -- --loglevel TRACE --variable answer:42 tasks.robot

# Interactive mode
rcc run --interactive
```

---

## rcc create

Create a new robot from templates.

```bash
rcc create [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--directory` | Target directory (default: current) |
| `--template` | Template name to use |

### Examples

```bash
# Interactive template selection
rcc create

# Create in specific directory
rcc create --directory my-robot

# Use specific template
rcc create --template python
```

### Available Templates

- `python` - Basic Python template
- `extended` - Extended Robot Framework template
- `standard` - Standard Robot Framework template

---

## rcc pull

Pull a robot from GitHub.

```bash
rcc pull <github-url> [flags]
```

### Examples

```bash
# Pull from GitHub
rcc pull github.com/joshyorko/template-python-browser

# Pull to specific directory
rcc pull github.com/user/repo --directory my-robot
```

---

## rcc task

Task and environment operations.

### rcc task run

Run a task (alias for `rcc run`).

```bash
rcc task run [flags]
```

### rcc task script

Run arbitrary commands inside robot environment.

```bash
rcc task script [flags] -- <command>
```

### Flags

| Flag | Description |
|------|-------------|
| `--robot` | Path to robot.yaml |
| `--space` | Holotree space name |
| `--silent` | Suppress rcc output |
| `--interactive` | Enable interactive mode |

### Examples

```bash
# Check Python version
rcc task script --silent -- python --version

# Get pip list
rcc task script --silent -- pip list

# Run ipython interactively
rcc task script --interactive -- ipython

# Run pytest
rcc task script --silent -- pytest tests/

# Install additional package (temporary)
rcc task script --silent -- pip install pandas
```

### rcc task shell

Open interactive shell in robot environment.

```bash
rcc task shell [flags]
```

### Examples

```bash
# Open shell with default robot.yaml
rcc task shell

# Open shell with specific robot
rcc task shell --robot path/to/robot.yaml

# Open shell in specific space
rcc task shell --space my-space
```

---

## rcc robot

Robot management commands.

### rcc robot dependencies

Show or export dependency information.

```bash
rcc robot dependencies [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--robot` | Path to robot.yaml |
| `--space` | Holotree space name |
| `--export` | Export as dependencies.yaml |

### Examples

```bash
# Show dependencies
rcc robot dependencies --space user

# Export dependencies
rcc robot dependencies --space user --export

# Compare with specific robot
rcc robot dependencies --robot robot.yaml --space user
```

### rcc robot bundle

Create self-contained robot bundle.

```bash
rcc robot bundle [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--robot` | Path to robot.yaml |
| `--output` | Output bundle filename |

### Examples

```bash
# Create bundle
rcc robot bundle --robot robot.yaml --output my-robot.py
```

### rcc robot run-from-bundle

Run a task from a bundle file.

```bash
rcc robot run-from-bundle <bundle> [flags]
```

### Examples

```bash
# Run from bundle
rcc robot run-from-bundle my-robot.py --task Producer
```

### rcc robot wrap

Package robot for deployment.

```bash
rcc robot wrap [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--robot` | Path to robot.yaml |
| `--zipfile` | Output zip filename |

### Examples

```bash
# Create robot.zip
rcc robot wrap --robot robot.yaml --zipfile robot.zip
```

---

## rcc holotree

Holotree environment management.

### rcc holotree list

List all holotree environments.

```bash
rcc holotree list
```

Output columns:
- **Identity** - Unique environment identifier
- **Controller** - Controller that created it
- **Space** - Space name
- **Blueprint** - Environment blueprint hash
- **Full path** - Filesystem location

### rcc holotree variables

Get environment variables for activation.

```bash
rcc holotree variables [flags] <conda.yaml>
```

### Flags

| Flag | Description |
|------|-------------|
| `--robot` | Path to robot.yaml |
| `--space` | Holotree space name |
| `--controller` | Controller identity |
| `--json` | Output as JSON |

### Examples

```bash
# Activate environment (Linux/macOS)
source <(rcc holotree variables --space mine conda.yaml)

# With robot.yaml
source <(rcc holotree variables --space mine --robot robot.yaml)

# Windows - save to file first
rcc holotree variables --space mine conda.yaml > activate.bat
call activate.bat
```

### rcc holotree shared

Manage shared holotree settings.

```bash
rcc holotree shared [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--enable` | Enable shared holotree |
| `--disable` | Disable shared holotree |

### Examples

```bash
# Enable shared holotree (requires admin)
# Windows:
rcc holotree shared --enable

# macOS/Linux:
sudo rcc holotree shared --enable
```

### rcc holotree init

Initialize user for shared holotree.

```bash
rcc holotree init [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--revoke` | Revert to private holotrees |

### Examples

```bash
# Use shared holotrees
rcc holotree init

# Revert to private
rcc holotree init --revoke
```

### rcc holotree delete

Delete holotree spaces.

```bash
rcc holotree delete [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--space` | Space name to delete |
| `--controller` | Controller identity |
| `--all` | Delete all spaces |

### Examples

```bash
# Delete specific space
rcc holotree delete --space my-space

# Delete all spaces for controller
rcc holotree delete --controller myapp --all
```

---

## rcc configure

Configuration and diagnostics.

### rcc configure diagnostics

Run system diagnostics.

```bash
rcc configure diagnostics [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--robot` | Include robot diagnostics |
| `--quick` | Quick diagnostics only |
| `--json` | Output as JSON |

### Examples

```bash
# Full diagnostics
rcc configure diagnostics

# With robot
rcc configure diagnostics --robot robot.yaml

# Quick check as JSON
rcc configure diagnostics --quick --json
```

### rcc configure netdiagnostics

Advanced network diagnostics.

```bash
rcc configure netdiagnostics [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--show` | Show default configuration |
| `--checks` | Custom checks file |

### Examples

```bash
# Run network diagnostics
rcc configure netdiagnostics

# Export default config
rcc configure netdiagnostics --show > netdiag.yaml

# Run custom checks
rcc configure netdiagnostics --checks netdiag.yaml
```

### rcc configure speedtest

Test RCC performance.

```bash
rcc configure speedtest
```

### rcc configure identity

Show installation identity.

```bash
rcc configure identity
```

---

## rcc docs

View built-in documentation.

### rcc docs changelog

View changelog.

```bash
rcc docs changelog
```

### rcc docs recipes

View tips, tricks, and recipes.

```bash
rcc docs recipes
```

---

## rcc cloud

Cloud/Control Room operations.

### rcc cloud push

Push robot to Control Room.

```bash
rcc cloud push [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--account` | Account credentials |
| `--workspace` | Workspace ID |
| `--robot` | Robot ID |
| `--directory` | Robot directory |

### Examples

```bash
rcc cloud push --account ${ACCOUNT_ID} --workspace ${WORKSPACE_ID} --robot ${ROBOT_ID} --directory ./my-robot
```

---

## rcc venv

Virtual environment support.

### rcc venv create

Create a virtual environment from holotree.

```bash
rcc venv create [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--robot` | Path to robot.yaml |
| `--space` | Holotree space name |
| `--venv` | Output venv directory |

### Examples

```bash
# Create venv
rcc venv create --robot robot.yaml --venv .venv

# Activate it
source .venv/bin/activate  # Linux/macOS
.venv\Scripts\activate     # Windows
```

---

## Configuration Files

### robot.yaml Schema

```yaml
# Task definitions (required)
tasks:
  Task Name:
    shell: python task.py           # Shell command
    # OR
    robotTaskName: Task Name        # Robot Framework task
    # OR
    command:                        # Command as list
      - python
      - -m
      - robot
      - tasks.robot

# Development tasks (optional)
devTasks:
  Dev Task:
    shell: python scripts/setup.py

# Environment file (required)
condaConfigFile: conda.yaml

# OR priority list (preferred)
environmentConfigs:
  - environment_linux_amd64_freeze.yaml
  - environment_windows_amd64_freeze.yaml
  - conda.yaml

# Pre-run scripts (optional)
preRunScripts:
  - setup.sh

# Artifacts directory (optional, default: output)
artifactsDir: output

# Ignore patterns files (optional)
ignoreFiles:
  - .gitignore

# Path additions (optional)
PATH:
  - .
  - bin

# Python path additions (optional)
PYTHONPATH:
  - .
  - libraries
```

### conda.yaml Schema

```yaml
# Channels (required)
channels:
  - conda-forge

# Dependencies (required)
dependencies:
  # Conda packages
  - python=3.9.13
  - pip=22.1.2

  # Pip packages
  - pip:
    - package==1.0.0

# Post-install scripts (optional)
rccPostInstall:
  - rfbrowser init
```

---

## Environment Variables Reference

### Robot Environment Variables (Automatically Injected)

**CRITICAL:** These are set automatically by RCC when running tasks. Do NOT set these manually.

| Variable | Description | Example Value |
|----------|-------------|---------------|
| `ROBOT_ROOT` | Directory containing robot.yaml - the "center of the universe" for the robot | `/home/user/my-robot` |
| `ROBOT_ARTIFACTS` | Artifact output directory (from `artifactsDir` in robot.yaml) | `/home/user/my-robot/output` |

**Path Resolution:** All relative paths in robot.yaml (PATH, PYTHONPATH, artifactsDir) are resolved relative to `ROBOT_ROOT`.

**Access in Python:**
```python
import os

# Get artifact directory for output files
artifacts = os.environ.get("ROBOT_ARTIFACTS", "output")

# Get robot root for relative path resolution
root = os.environ.get("ROBOT_ROOT", ".")
```

### Work Items Environment Variables

For local development with `robocorp.workitems`:

| Variable | Description |
|----------|-------------|
| `RC_WORKITEM_ADAPTER` | Custom adapter class (e.g., `FileAdapter`) |
| `RC_WORKITEM_INPUT_PATH` | Path to input work items JSON |
| `RC_WORKITEM_OUTPUT_PATH` | Path for output work items JSON |
| `RC_WORKITEMS_PATH` | Local work items directory (devdata) |

**Local Development Setup:**
```bash
export RC_WORKITEM_ADAPTER=FileAdapter
export RC_WORKITEM_INPUT_PATH=/path/to/input.json
export RC_WORKITEM_OUTPUT_PATH=/path/to/output.json
```

### RCC Configuration Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ROBOCORP_HOME` | RCC home directory | `~/.robocorp` (varies by OS) |
| `RCC_VERBOSE_ENVIRONMENT_BUILDING` | Verbose builds | unset |
| `RCC_NO_BUILD` | Prevent builds | unset |
| `RCC_VERBOSITY` | Output level: silent/debug/trace | unset |
| `RCC_NO_TEMP_MANAGEMENT` | Disable temp management | unset |
| `RCC_NO_PYC_MANAGEMENT` | Disable .pyc management | unset |
| `RCC_CREDENTIALS_ID` | Control Room credentials | unset |

### Custom Endpoints (Air-Gapped/Private Networks)

| Variable | Description |
|----------|-------------|
| `RCC_ENDPOINT_CLOUD_API` | Cloud API endpoint |
| `RCC_ENDPOINT_CLOUD_UI` | Cloud UI endpoint |
| `RCC_ENDPOINT_CLOUD_LINKING` | Cloud linking endpoint |
| `RCC_ENDPOINT_DOWNLOADS` | Downloads endpoint |
| `RCC_ENDPOINT_DOCS` | Documentation endpoint |
| `RCC_ENDPOINT_TELEMETRY` | Telemetry endpoint |
| `RCC_ENDPOINT_PYPI` | PyPI mirror |
| `RCC_ENDPOINT_PYPI_TRUSTED` | PyPI trusted host |
| `RCC_ENDPOINT_CONDA` | Conda channel |

**Air-Gapped Example:**
```bash
export RCC_ENDPOINT_PYPI="https://pypi.internal.com/simple/"
export RCC_ENDPOINT_CONDA="https://conda.internal.com/"
rcc run --silent
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Environment build failure |
| 3 | Task execution failure |

---

## Shared Holotree Locations

| OS | Shared Location |
|----|-----------------|
| Windows | `C:\ProgramData\robocorp` |
| macOS | `/Users/Shared/robocorp` |
| Linux | `/opt/robocorp` |

---

## Holotree Deep Dive

### Architecture

Holotree is RCC's content-addressed storage system for Python environments:

**Three Components:**
1. **Library (Hololib)** - Content-addressed file store organized by hash
   - Files stored once, referenced by SipHash-128 content hash
   - Storage structure: `{hash_prefix}/{hash}/{full_hash}`
   - 50MB binary in 20 environments = 50MB disk (not 1GB)

2. **Catalog** - JSON manifests describing environments
   - Lists every file with hash, permissions, size, relocation offsets
   - Platform-specific: `{blueprint_hash}v12.{platform}`
   - Enables surgical path relocation

3. **Spaces** - Live working environments
   - Populated from catalogs by linking files from library
   - Each space is pristine copy that can become "dirty"

### Path Relocation

Python installations embed absolute paths in:
- Shebangs (`#!/path/to/python`)
- `__pycache__` files
- Compiled extensions

**Holotree solution:** Records byte offsets during catalog creation, performs surgical rewrites at exact offsets when restoring to new location. No regex, no pattern matching - direct byte manipulation.

### Performance

| Operation | Time |
|-----------|------|
| Fresh build from scratch | 5-15 minutes |
| Restore from cache | 2-10 seconds |

### Freeze Files (Environment Snapshots)

**Generated automatically** to `ROBOT_ARTIFACTS` (output/) on every `rcc run`:

| File | Platform |
|------|----------|
| `environment_linux_amd64_freeze.yaml` | Linux x64 |
| `environment_windows_amd64_freeze.yaml` | Windows x64 |
| `environment_darwin_amd64_freeze.yaml` | macOS Intel |
| `environment_darwin_arm64_freeze.yaml` | macOS Apple Silicon |

**Key Points:**
- Generated since RCC v10.3.0 (June 2021)
- Contains exact pinned versions for reproducibility
- NOT meant to be committed unless you want locked builds
- Listed in `environmentConfigs` with conda.yaml as fallback

```yaml
# robot.yaml - environmentConfigs with freeze file priority
environmentConfigs:
  - environment_linux_amd64_freeze.yaml    # Use if exists
  - environment_windows_amd64_freeze.yaml  # Use if exists
  - conda.yaml                              # Fallback
```
