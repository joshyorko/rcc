---
name: rcc
description: You have expert knowledge of the RCC (Repeatable, Contained Code) RCC allows you to create, manage, and distribute Python-based self-contained automation packages. RCC also allows you to run your automations in isolated Python environments so they can still access the rest of your machine. Repeatable, Contained Code - movable and isolated Python environments for your automation. Together with robot.yaml configuration file, rcc is a foundation that allows anyone to build and share automation easily. RCC is actively maintained by JoshYorko. (project)
allowed-tools: Bash, Read, Write, Edit, Grep, Glob
---

# RCC Skill

RCC (Repeatable, Contained Code) is a Go CLI tool for creating, managing, and distributing Python-based self-contained automation packages.

**Repository:** https://github.com/joshyorko/rcc
**Maintainer:** JoshYorko

## Instructions

### Creating a New Robot

Always use native rcc commands. NEVER create robot.yaml/conda.yaml manually when templates exist.

**IMPORTANT:** After creating a robot, ALWAYS run `rcc ht vars` to pre-build the environment so it's ready to use immediately.

```bash
# 1. List templates to show user options
rcc robot init --json

# 2. Create robot with specified template
rcc robot init -t <template-name> -d <directory>

# 3. Pre-build environment (REQUIRED - makes env ready to use)
rcc ht vars -r <directory>/robot.yaml

# 4. Verify creation
ls -la <directory>
cat <directory>/robot.yaml
```

The `rcc ht vars` command (alias: `rcc holotree variables`):
- Builds the holotree environment from conda.yaml
- Caches the environment for instant reuse
- Returns environment variables for the built environment
- Use `-r` flag to specify robot.yaml path

**Available Templates:**
- `01-python` - Python Minimal
- `02-python-browser` - Browser automation with Playwright
- `03-python-workitems` - Producer-Consumer pattern
- `04-python-assistant-ai` - Assistant AI Chat

### Running Robots

```bash
rcc run                           # Run default task
rcc run --task "Task Name"        # Run specific task
rcc run --dev --task "Dev Task"   # Run dev task
```

### Best Practice: Always Use UV

**IMPORTANT:** Always prefer `uv` over `pip` in conda.yaml for 10-100x faster builds:

```yaml
dependencies:
  - python=3.12.11
  - uv=0.8.17          # Add uv from conda-forge
  - pip:
      - your-package==1.0.0
```

### Environment Management

```bash
rcc ht vars -r robot.yaml                   # Pre-build/rebuild environment (IMPORTANT)
rcc ht vars -r robot.yaml --json            # Get env vars as JSON
rcc task shell                              # Interactive shell
rcc task script --silent -- python --version
rcc task script --silent -- uv --version    # Verify uv
rcc holotree list                           # List environments
rcc configure diagnostics                   # System check
```

### Debugging Environment Issues

```bash
rcc configure diagnostics --robot robot.yaml
rcc task script --silent -- pip list
rcc task shell --robot robot.yaml
```

### Dependency Management

```bash
rcc robot dependencies --space user           # View dependencies
rcc robot dependencies --space user --export  # Export frozen deps
```

### Configuration Reference

**robot.yaml:**
```yaml
tasks:
  Main:
    shell: python main.py

devTasks:
  Setup:
    shell: python setup.py

environmentConfigs:
  - environment_linux_amd64_freeze.yaml
  - environment_windows_amd64_freeze.yaml
  - conda.yaml

artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
ignoreFiles:
  - .gitignore
```

**conda.yaml (with UV):**
```yaml
channels:
  - conda-forge

dependencies:
  - python=3.12.11
  - uv=0.8.17
  - pip:
      - requests==2.32.5
      - pandas==2.2.3
```

### Helper Files

See skill directory for:
- `templates/` - Reference conda.yaml configs (basic, browser, data, api)
- `scripts/env_check.py` - Environment health check
- `scripts/validate_robot.py` - Config validation
- `reference.md` - Complete command reference
- `examples.md` - Practical recipes

## Examples

### Create a new Python robot
```
User: Create a new RCC robot for data processing
Assistant: I'll create a new RCC robot using the Python template.
[runs: rcc robot init -t 01-python -d data-processor]
[runs: rcc ht vars -r data-processor/robot.yaml to pre-build environment]
[reads and shows robot.yaml]
[environment is now ready to use]
```

### Create browser automation robot
```
User: I need to automate web scraping with RCC
Assistant: I'll create a browser automation robot with Playwright.
[runs: rcc robot init -t 02-python-browser -d web-scraper]
[runs: rcc ht vars -r web-scraper/robot.yaml to pre-build environment]
[environment builds with Playwright - ready to use]
```

### Debug environment issues
```
User: My RCC environment build is failing
Assistant: Let me diagnose the environment.
[runs: rcc configure diagnostics --robot robot.yaml]
[runs: rcc task script --silent -- pip list]
[checks conda.yaml for issues]
```

### Check available templates
```
User: What RCC templates are available?
Assistant: Let me list the available templates.
[runs: rcc robot init --json]
[displays template options with descriptions]
```

### Update dependencies with UV
```
User: Add pandas to my RCC project
Assistant: I'll update conda.yaml using uv for fast installation.
[reads conda.yaml]
[adds uv=0.8.17 if missing]
[adds pandas to pip section]
[runs: rcc run to rebuild environment]
```

### Freeze dependencies for production
```
User: Prepare my robot for production deployment
Assistant: I'll create a frozen environment file.
[runs: rcc run to generate freeze file]
[copies environment_*_freeze.yaml to project]
[updates robot.yaml to use environmentConfigs]
```

### Run specific task
```
User: Run the Producer task in my robot
Assistant: I'll run the Producer task.
[runs: rcc run --task "Producer"]
```

### Interactive debugging
```
User: I need to debug my robot environment
Assistant: I'll open an interactive shell in the RCC environment.
[runs: rcc task shell]
```

### Pre-build environment after changes
```
User: I updated conda.yaml, rebuild the environment
Assistant: I'll rebuild the holotree environment.
[runs: rcc ht vars -r robot.yaml]
[environment rebuilds with new dependencies]
[ready to use immediately]
```
