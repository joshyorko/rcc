# RCC Examples and Recipes

Practical examples for common RCC use cases.

## Table of Contents

- [Quick Start](#quick-start)
- [Project Templates](#project-templates)
- [Environment Management](#environment-management)
- [Dependency Management](#dependency-management)
- [CI/CD Integration](#cicd-integration)
- [Advanced Patterns](#advanced-patterns)
- [Troubleshooting Recipes](#troubleshooting-recipes)

---

## Quick Start

### Create Your First Robot

```bash
# Create a new robot interactively
rcc create
# Select: python
# Enter project name

cd my-robot
rcc run
```

### Pull and Run Existing Robot

```bash
rcc pull github.com/joshyorko/template-python-browser
cd template-python-browser
rcc run
```

---

## Project Templates

### Minimal Python Robot (with UV)

> **Note:** All examples use `uv` for faster package installation. Always include `uv` in your conda.yaml dependencies.

**robot.yaml:**
```yaml
tasks:
  Main:
    shell: python main.py

condaConfigFile: conda.yaml
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
ignoreFiles:
  - .gitignore
```

**conda.yaml:**
```yaml
channels:
  - conda-forge

dependencies:
  - python=3.12.11
  - uv=0.8.17      # Fast package installer (RECOMMENDED)
  - pip:
      - requests==2.32.5
```

**main.py:**
```python
def main():
    print("Hello from RCC!")

if __name__ == "__main__":
    main()
```

**.gitignore:**
```
output/
__pycache__/
*.pyc
.env
```

---

### Web Scraping Robot

**robot.yaml:**
```yaml
tasks:
  Scrape:
    shell: python scraper.py

  Process:
    shell: python processor.py

  Full Pipeline:
    shell: python main.py

condaConfigFile: conda.yaml
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
  - libraries
ignoreFiles:
  - .gitignore
```

**conda.yaml:**
```yaml
channels:
  - conda-forge

dependencies:
  - python=3.12.11
  - uv=0.8.17
  - pip:
      - requests==2.32.5
      - beautifulsoup4==4.12.3
      - lxml==5.3.0
      - pandas==2.2.3
```

---

### Browser Automation Robot

**robot.yaml:**
```yaml
tasks:
  Run Browser Tests:
    shell: python -m robot --outputdir output tests/

  Debug Single Test:
    shell: python -m robot --outputdir output --loglevel DEBUG tests/

condaConfigFile: conda.yaml
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - keywords
  - libraries
ignoreFiles:
  - .gitignore
```

**conda.yaml:**
```yaml
channels:
  - conda-forge

dependencies:
  - python=3.12.11
  - nodejs=18.17.1
  - uv=0.8.17
  - pip:
      - robotframework==7.1.1
      - robotframework-browser==18.9.1
      - rpaframework==28.6.1

rccPostInstall:
  - rfbrowser init
```

---

### Data Science Robot

**robot.yaml:**
```yaml
tasks:
  Train Model:
    shell: python train.py

  Predict:
    shell: python predict.py

  Jupyter:
    shell: jupyter notebook

devTasks:
  Explore Data:
    shell: jupyter notebook --port 8888

condaConfigFile: conda.yaml
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
  - src
ignoreFiles:
  - .gitignore
```

**conda.yaml:**
```yaml
channels:
  - conda-forge

dependencies:
  - python=3.12.11
  - uv=0.8.17
  - pandas=2.2.3
  - numpy=2.1.3
  - scikit-learn=1.5.2
  - matplotlib=3.9.2
  - seaborn=0.13.2
  - jupyter=1.1.1
  - pip:
      - joblib==1.4.2
```

---

### FastAPI Service Robot

**robot.yaml:**
```yaml
tasks:
  Run Server:
    shell: uvicorn main:app --host 0.0.0.0 --port 8000

  Run Dev Server:
    shell: uvicorn main:app --reload --host 0.0.0.0 --port 8000

devTasks:
  Test API:
    shell: pytest tests/ -v

condaConfigFile: conda.yaml
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
ignoreFiles:
  - .gitignore
```

**conda.yaml:**
```yaml
channels:
  - conda-forge

dependencies:
  - python=3.12.11
  - uv=0.8.17
  - pip:
      - fastapi==0.115.5
      - uvicorn==0.32.1
      - pydantic==2.10.0
      - httpx==0.28.0
      - pytest==8.3.3
```

---

## Environment Management

### Activate Environment in Shell

**Linux/macOS:**
```bash
# With robot.yaml
source <(rcc holotree variables --space dev --robot robot.yaml)

# With just conda.yaml
source <(rcc holotree variables --space dev conda.yaml)

# Now you can use the environment
python --version
pip list
```

**Windows:**
```cmd
:: Save to file first
rcc holotree variables --space dev --robot robot.yaml > activate.bat
call activate.bat

:: Now use the environment
python --version
```

### Run Ad-hoc Commands

```bash
# Check Python version
rcc task script --silent -- python --version

# List installed packages
rcc task script --silent -- pip list

# Run a quick Python script
rcc task script --silent -- python -c "import sys; print(sys.executable)"

# Interactive Python
rcc task script --interactive -- python

# Interactive IPython
rcc task script --interactive -- ipython

# Run tests
rcc task script --silent -- pytest tests/ -v
```

### Multiple Environments

```bash
# Create separate spaces for different purposes
rcc holotree variables --space dev conda.yaml
rcc holotree variables --space test conda-test.yaml
rcc holotree variables --space prod conda-prod.yaml

# List all environments
rcc holotree list
```

### Clean Up Environments

```bash
# List current environments
rcc holotree list

# Delete specific space
rcc holotree delete --space old-space

# Delete all spaces for a controller
rcc holotree delete --controller myapp --all
```

---

## Dependency Management

### Check for Dependency Drift

```bash
# Show current dependencies
rcc robot dependencies --space user

# Export as wanted list
rcc robot dependencies --space user --export

# This creates dependencies.yaml - commit it!
git add dependencies.yaml
git commit -m "Pin dependencies"
```

### Freeze Dependencies for Production

```bash
# 1. Run your robot to create the environment
rcc run

# 2. Find the freeze file in output/
ls output/environment_*_freeze.yaml

# 3. Copy to project root
cp output/environment_*_freeze.yaml .

# 4. Update robot.yaml to use freeze file
```

**Updated robot.yaml:**
```yaml
environmentConfigs:
  - environment_linux_amd64_freeze.yaml
  - environment_windows_amd64_freeze.yaml
  - environment_darwin_amd64_freeze.yaml
  - conda.yaml  # Fallback

tasks:
  Main:
    shell: python main.py
```

### Add New Dependencies

```bash
# 1. Edit conda.yaml to add package
# 2. Run to rebuild environment
rcc run

# 3. Verify package is installed
rcc task script --silent -- pip show new-package

# 4. Export updated freeze file
rcc robot dependencies --space user --export
```

---

## CI/CD Integration

### GitHub Actions Example

**.github/workflows/robot.yml:**
```yaml
name: Run Robot

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  run-robot:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Install RCC
        run: |
          curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
          chmod +x rcc
          sudo mv rcc /usr/local/bin/

      - name: Run Diagnostics
        run: rcc configure diagnostics --quick

      - name: Run Robot
        run: rcc run

      - name: Upload Artifacts
        uses: actions/upload-artifact@v3
        if: always()
        with:
          name: robot-output
          path: output/
```

### GitLab CI Example

**.gitlab-ci.yml:**
```yaml
stages:
  - test

run_robot:
  stage: test
  image: ubuntu:22.04

  before_script:
    - apt-get update && apt-get install -y curl
    - curl -o /usr/local/bin/rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
    - chmod +x /usr/local/bin/rcc

  script:
    - rcc configure diagnostics --quick
    - rcc run

  artifacts:
    when: always
    paths:
      - output/
    expire_in: 1 week
```

### Jenkins Pipeline Example

**Jenkinsfile:**
```groovy
pipeline {
    agent any

    stages {
        stage('Setup') {
            steps {
                sh '''
                    curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
                    chmod +x rcc
                '''
            }
        }

        stage('Run Robot') {
            steps {
                sh './rcc run'
            }
        }
    }

    post {
        always {
            archiveArtifacts artifacts: 'output/**/*', allowEmptyArchive: true
        }
    }
}
```

### Push to Control Room

```bash
#!/bin/bash
# ci-deploy.sh

# Get RCC
curl -o rcc https://github.com/joshyorko/rcc/releases/latest/download/rcc-linux64
chmod +x rcc

# Push to Control Room
./rcc cloud push \
    --account "${ACCOUNT_CREDENTIALS}" \
    --workspace "${WORKSPACE_ID}" \
    --robot "${ROBOT_ID}" \
    --directory .
```

---

## Advanced Patterns

### Multiple Tasks with Shared Code

**Project structure:**
```
my-robot/
├── robot.yaml
├── conda.yaml
├── main.py
├── tasks/
│   ├── __init__.py
│   ├── task_a.py
│   └── task_b.py
└── shared/
    ├── __init__.py
    └── utils.py
```

**robot.yaml:**
```yaml
tasks:
  Task A:
    shell: python -m tasks.task_a

  Task B:
    shell: python -m tasks.task_b

  All Tasks:
    shell: python main.py

condaConfigFile: conda.yaml
artifactsDir: output
PYTHONPATH:
  - .
  - shared
```

### Pre-Run Scripts for Private Packages

**robot.yaml:**
```yaml
tasks:
  Main:
    shell: python main.py

preRunScripts:
  - install_private.sh

condaConfigFile: conda.yaml
```

**install_private.sh:**
```bash
#!/bin/bash
# Install from private PyPI
pip install --extra-index-url https://pypi.private.com/simple/ my-private-package

# Or from a private git repo
pip install git+https://${GIT_TOKEN}@github.com/org/private-lib.git
```

### Platform-Specific Scripts

**robot.yaml:**
```yaml
preRunScripts:
  - setup_linux.sh
  - setup_windows.bat
  - setup_darwin.sh
```

RCC automatically skips scripts that don't match the current platform based on filename keywords.

### Self-Contained Bundle

```bash
# Create bundle
rcc robot bundle --robot robot.yaml --output my-robot.py

# The bundle includes:
# - Robot code
# - Environment (holotree export)
# - Everything needed to run

# Run on another machine
rcc robot run-from-bundle my-robot.py --task Main

# Or directly (shows usage instructions)
./my-robot.py
```

### Custom Endpoints

**Using environment variables:**
```bash
export RCC_ENDPOINT_PYPI="https://pypi.internal.com/simple/"
export RCC_ENDPOINT_CONDA="https://conda.internal.com/"
rcc run
```

**Using settings.yaml ($ROBOCORP_HOME/settings.yaml):**
```yaml
endpoints:
  pypi: https://pypi.internal.com/simple/
  pypi-trusted: https://pypi.internal.com/
  conda: https://conda.internal.com/
```

---

## Troubleshooting Recipes

### Environment Build Fails

```bash
# Enable verbose output
export RCC_VERBOSE_ENVIRONMENT_BUILDING=1
rcc run

# Check diagnostics
rcc configure diagnostics --robot robot.yaml

# Test network connectivity
rcc configure netdiagnostics
```

### Dependency Conflicts

```bash
# See what's installed
rcc task script --silent -- pip list

# Check specific package
rcc task script --silent -- pip show package-name

# See dependency tree
rcc task script --silent -- pip install pipdeptree
rcc task script --silent -- pipdeptree
```

### Long Path Issues (Windows)

```bash
# Option 1: Set shorter ROBOCORP_HOME
set ROBOCORP_HOME=C:\rcc

# Option 2: Enable long paths in Windows
# Run as admin:
reg add "HKLM\SYSTEM\CurrentControlSet\Control\FileSystem" /v LongPathsEnabled /t REG_DWORD /d 1 /f
```

### Shared Holotree Permission Issues

**Windows:**
```cmd
icacls "C:\ProgramData\robocorp" /grant "*S-1-5-32-545:(OI)(CI)M" /T
```

**Linux/macOS:**
```bash
sudo chmod -R 777 /opt/robocorp  # or /Users/Shared/robocorp on macOS
```

### Clean Slate Recovery

```bash
# Remove all holotree data
rm -rf ~/.robocorp/holotree
rm -rf ~/.robocorp/hololib

# Or on Windows
rmdir /s /q %USERPROFILE%\.robocorp\holotree
rmdir /s /q %USERPROFILE%\.robocorp\hololib

# Rebuild environment
rcc run
```

### Debug Mode

```bash
# Debug output
rcc run --debug

# Trace output (very verbose)
rcc run --trace

# Timeline
rcc run --timeline

# Profiling
rcc run --pprof profile.out
# Analyze with: go tool pprof profile.out
```

### Check RCC Version and Updates

```bash
# Current version
rcc version

# Check for updates
# Visit: https://github.com/joshyorko/rcc/releases
```

---

## Real-World Examples

### Web Scraping with Error Handling

**main.py:**
```python
import os
import json
from datetime import datetime
import requests
from bs4 import BeautifulSoup

def scrape_data():
    """Scrape data with proper error handling."""
    artifacts_dir = os.environ.get('ROBOT_ARTIFACTS', 'output')

    try:
        response = requests.get('https://example.com', timeout=30)
        response.raise_for_status()

        soup = BeautifulSoup(response.text, 'lxml')
        data = {
            'title': soup.title.string if soup.title else None,
            'timestamp': datetime.now().isoformat()
        }

        # Save to artifacts
        output_path = os.path.join(artifacts_dir, 'data.json')
        with open(output_path, 'w') as f:
            json.dump(data, f, indent=2)

        print(f"Data saved to {output_path}")
        return 0

    except requests.RequestException as e:
        print(f"Request failed: {e}")
        return 1
    except Exception as e:
        print(f"Unexpected error: {e}")
        return 2

if __name__ == "__main__":
    exit(scrape_data())
```

### Scheduled Task with Logging

**main.py:**
```python
import os
import logging
from datetime import datetime

# Setup logging to artifacts
artifacts = os.environ.get('ROBOT_ARTIFACTS', 'output')
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler(os.path.join(artifacts, 'robot.log')),
        logging.StreamHandler()
    ]
)
logger = logging.getLogger(__name__)

def main():
    logger.info("Robot started")

    try:
        # Your automation code here
        logger.info("Processing...")

        logger.info("Robot completed successfully")
        return 0

    except Exception as e:
        logger.error(f"Robot failed: {e}", exc_info=True)
        return 1

if __name__ == "__main__":
    exit(main())
```

---

## Quick Reference Card

```bash
# === Create & Run ===
rcc create                          # New robot from template
rcc run                             # Run default task
rcc run --task "Name"               # Run specific task
rcc run --dev --task "Dev Task"     # Run dev task

# === Environment ===
rcc task shell                      # Interactive shell
rcc task script -- <cmd>            # Run command
rcc holotree list                   # List environments
rcc holotree delete --space name    # Delete environment

# === Dependencies ===
rcc robot dependencies --space user           # Show deps
rcc robot dependencies --space user --export  # Export deps

# === Diagnostics ===
rcc configure diagnostics           # System check
rcc configure diagnostics --robot robot.yaml  # Robot check
rcc configure speedtest             # Performance test
rcc run --debug                     # Debug output
rcc run --trace                     # Trace output

# === Bundles ===
rcc robot bundle --output robot.py  # Create bundle
rcc robot run-from-bundle robot.py  # Run bundle
```
