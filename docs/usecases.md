# RCC Use Cases

## Core Use Cases

* run robots in Robocorp Worker locally or in cloud containers
* run robots in Robocorp Assistant
* provide commands for Robocorp Code to develop robots locally and
  communicate to Robocorp Control Room
* provide commands that can be used in CI pipelines (Jenkins, Gitlab CI, ...)
  to push robots into Robocorp Control Room
* can also be used to run robot tests in CI/CD environments
* provide isolated environments to run python scripts and applications
* to use other scripting languages and tools available from conda-forge (or
  conda in general) with isolated and easily installed manner (see list below
  for ideas what is available)
* provide above things in computers, where internet access is restricted or
  prohibited (using pre-made hololib.zip environments, or importing prebuild
  environments build elsewhere)
* pull and run community created robots without Control Room requirement
* use rcc provided holotree environments as soft-containers (they are isolated
  environments, but also have access to rest of your machine resources)

---

## Development & Debugging

### Run Arbitrary Scripts in Robot Environments
Execute any command within a robot's isolated environment without running the full robot task.

```bash
# List installed packages
rcc task script -- pip list

# Check Python version
rcc task script -- python --version

# Run a custom Python script
rcc task script -- python myscript.py

# Interactive Python session (development only)
rcc task script --interactive -- python
```

### Interactive CLI Mode
User-friendly terminal interface for environment management and robot creation.

```bash
# Launch interactive TUI
rcc interactive
```

### System Diagnostics
Comprehensive system health checks for troubleshooting automation failures.

```bash
# Run full diagnostics
rcc diagnostics

# Quick diagnostics only
rcc diagnostics --quick

# Save diagnostics to file
rcc diagnostics --file report.txt

# JSON output for automation
rcc diagnostics --json

# Production-level robot checks
rcc diagnostics --robot robot.yaml --production
```

---

## Environment Management

### Create Traditional Python Virtual Environments
Create standard Python venvs using holotree as the base environment, compatible with pip and standard Python tooling.

```bash
# Create venv from conda.yaml
rcc holotree venv conda.yaml

# Force recreation of existing venv
rcc holotree venv --force conda.yaml

# Include dev dependencies
rcc holotree venv --devdeps conda.yaml
```

### Export/Import Environments for Team Sharing
Share exact environments between team members or transfer to air-gapped machines.

```bash
# Export robot's environment
rcc holotree export -r robot.yaml

# Export specific catalogs
rcc holotree export my_catalog another_catalog

# Export with custom filename
rcc holotree export -z myenv.zip my_catalog

# Import from local zip
rcc holotree import hololib.zip

# Import from URL
rcc holotree import https://example.com/hololib.zip
```

### Manage Environment Catalogs and Blueprints
View and manage cached environment definitions.

```bash
# List available catalogs
rcc holotree catalogs

# List blueprints
rcc holotree blueprints

# Pre-build environment for faster startup
rcc holotree prebuild conda.yaml

# Delete unused environments
rcc holotree delete <catalog>
```

### Robot Dependency Analysis
Analyze and validate robot dependencies before execution.

```bash
# Check robot dependencies
rcc robot dependencies -r robot.yaml
```

---

## Distribution & Deployment

### Create Self-Contained Robot Bundles
Package robot code with complete environment into a single distributable file.

```bash
# Create bundle from robot
rcc robot bundle -r robot.yaml -o mybundle.py

# Bundle includes:
# - Robot source code
# - Environment configuration (conda.yaml)
# - Complete holotree environment (hololib.zip)
```

### Run Robots from Bundles
Execute self-contained bundles without additional setup.

```bash
# Run specific task from bundle
rcc robot run-from-bundle mybundle.py -t task_name

# Force environment update
rcc robot run-from-bundle mybundle.py -t task_name -f
```

### Package Robots from Directories
Create robot.zip packages for distribution or CI/CD.

```bash
# Package current directory
rcc robot wrap

# Custom output filename
rcc robot wrap -z myrobot.zip

# Package specific directory
rcc robot wrap -d /path/to/robot

# With ignore patterns
rcc robot wrap -i .gitignore
```

### Unpack Robot Packages
Extract robot packages for inspection or modification.

```bash
# Unpack robot.zip
rcc robot unwrap -z myrobot.zip -d ./output
```

---

## Enterprise & Operations

### Event Journaling and Auditing
Track automation events for compliance and debugging.

```bash
# View event history
rcc configure events

# JSON output for monitoring integration
rcc configure events --json
```

### TLS Certificate Management
Manage certificates for corporate environments with custom CAs.

```bash
# Export TLS certificates
rcc configure tls-export

# Probe SSL configuration
rcc configure tls-probe https://example.com
```

### Profile Management
Switch between different configurations for multi-environment setups.

```bash
# Manage configuration profiles
rcc configure profile
```

### Air-Gapped Deployment Workflow
Complete workflow for environments without internet access:

1. **On connected machine**: Export environment
   ```bash
   rcc holotree export -r robot.yaml -z transfer.zip
   ```

2. **Transfer**: Copy `transfer.zip` via USB or internal network

3. **On air-gapped machine**: Import environment
   ```bash
   rcc holotree import transfer.zip
   ```

4. **Run robot**: Execute without internet
   ```bash
   rcc run -r robot.yaml
   ```

---

## Monitoring & Troubleshooting

### Network Diagnostics
Troubleshoot network-related automation failures.

```bash
# Network connectivity checks
rcc configure netdiagnostics
```

### Robot Cache Management
Optimize caching for reduced download times and offline capability.

* Automatic caching of robot packages
* Auto-cleanup of oldest entries
* Bandwidth optimization for repeated runs

### Process Tree Management
Track and manage subprocess lifecycles for complex automations.

* Monitor child processes during task execution
* Ensure clean termination of all subprocesses
* Support for long-running automation tasks

---

## What is available from conda-forge?

* python and libraries
* ruby and libraries
* perl and libraries
* lua and libraries
* r and libraries
* julia and libraries
* make, cmake and compilers (C++, Fortran, ...)
* nodejs
* nginx
* rust
* php
* go
* gawk, sed, and emacs, vim
* ROS libraries (robot operating system)
* firefox
