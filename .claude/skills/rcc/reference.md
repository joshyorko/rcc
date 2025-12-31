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
| `--silent` | Suppress non-essential output |
| `--timeline` | Show execution timeline |
| `--pprof <file>` | Write profiling data to file |
| `--robocorp` | Use Robocorp product family settings |

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

### RCC Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `ROBOCORP_HOME` | RCC home directory | `~/.robocorp` (varies by OS) |
| `RCC_VERBOSE_ENVIRONMENT_BUILDING` | Verbose builds | unset |
| `RCC_NO_BUILD` | Prevent builds | unset |
| `RCC_VERBOSITY` | Output level: silent/debug/trace | unset |
| `RCC_NO_TEMP_MANAGEMENT` | Disable temp management | unset |
| `RCC_NO_PYC_MANAGEMENT` | Disable .pyc management | unset |
| `RCC_CREDENTIALS_ID` | Control Room credentials | unset |

### Custom Endpoints (Fork-Specific)

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

### Robot Environment Variables

Set automatically when running robots:

| Variable | Description |
|----------|-------------|
| `ROBOT_ROOT` | Robot root directory |
| `ROBOT_ARTIFACTS` | Artifacts directory |

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
