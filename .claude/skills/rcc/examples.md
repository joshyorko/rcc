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
  - uv=0.9.28 # Fast package installer (RECOMMENDED)
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
  - uv=0.9.28
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
  - uv=0.9.28
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
  - uv=0.9.28
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
  - uv=0.9.28
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
  - conda.yaml # Fallback

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

## Real-World GitHub Examples

### ⭐ Best Practice: Full Producer-Consumer-Reporter (from joshyorko/fetch-repos-bot)

**Source:** https://github.com/joshyorko/fetch-repos-bot

This is the definitive example of the producer-consumer pattern with work items, Redis/SQLite adapters, proper error handling, and reporting.

**robot.yaml:**
```yaml
tasks:
  Producer:
    shell: python -m robocorp.tasks run tasks.py -t producer
  Consumer:
    shell: python -m robocorp.tasks run tasks.py -t consumer
  Reporter:
    shell: python -m robocorp.tasks run tasks.py -t reporter
  GenerateConsolidatedDashboard:
    shell: python -m robocorp.tasks run generate_consolidated_dashboard.py -t generate_consolidated_dashboard
  AssistantOrg:
    shell: python -m robocorp.tasks run assistant.py -t assistant_org

devTasks:
  SeedSQLiteDB:
    shell: python scripts/seed_sqlite_db.py
  SeedRedisDB:
    shell: python scripts/seed_redis_db.py
  CheckSQLiteDB:
    shell: python scripts/check_sqlite_db.py
  RecoverOrphanedItems:
    shell: python scripts/recover_orphaned_items.py
  ListWorkItems:
    shell: python -m robocorp.tasks run listworkitems.py -t list_work_items

environmentConfigs:
  - environment_windows_amd64_freeze.yaml
  - environment_linux_amd64_freeze.yaml
  - environment_darwin_amd64_freeze.yaml
  - conda.yaml

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
  - uv=0.9.28
  - pip:
      - robocorp==3.0.0
      - robocorp-truststore==0.9.1
      - rpaframework-assistant==5.0.0
      - requests==2.32.5
      - gitpython==3.1.45
      - pandas==2.3.3
      - beautifulsoup4==4.14.3
      - jinja2==3.1.6
      - redis==7.1.0
      - SQLAlchemy==2.0.45
      - pymongo==4.15.5
      - robocorp_adapters_custom==0.1.4  # Custom work item adapters
```

**tasks.py - Producer (key excerpts):**
```python
from robocorp import log, workitems
from robocorp.tasks import get_output_dir, task

@task
def producer():
    """Fetches repositories from GitHub org and creates work items."""
    for item in workitems.inputs:
        try:
            payload = item.payload
            if not isinstance(payload, dict):
                item.fail("APPLICATION", code="INVALID_PAYLOAD",
                          message="Payload must be a dictionary")
                continue

            org_name = payload.get("org")
            if not org_name:
                item.fail("APPLICATION", code="MISSING_ORG_NAME",
                          message="Organization name is required")
                continue

            log.info(f"Processing organization: {org_name}")

            # Fetch data and create work items
            df = repos(org_name)
            if df is not None and not df.empty:
                for row in df.to_dict(orient="records"):
                    repo_payload = {
                        "org": org_name,
                        "Name": row.get("Name"),
                        "URL": row.get("URL"),
                        "Description": row.get("Description"),
                        # ... more fields
                    }
                    workitems.outputs.create(repo_payload)
                item.done()
            else:
                item.fail("BUSINESS", code="NO_DATA",
                          message="No repositories found")

        except Exception as e:
            log.exception(f"Unexpected error: {e}")
            item.fail("APPLICATION", code="UNEXPECTED_ERROR", message=str(e))
```

**tasks.py - Consumer (key excerpts):**
```python
@task
def consumer():
    """Clones repositories from work items, zips them."""
    output = get_output_dir() or Path("output")
    shard_id = os.getenv("SHARD_ID", "0")  # For parallel processing
    processed_repos = []

    for item in workitems.inputs:
        try:
            payload = item.payload
            url = payload.get("URL")
            repo_name = payload.get("Name")

            if not url:
                item.fail("APPLICATION", code="MISSING_URL",
                          message="URL is missing")
                continue

            repo_path = repos_dir / repo_name
            log.info(f"[Shard {shard_id}] {org_name}/{repo_name} - cloning...")

            try:
                # Clone with GitPython
                repo = Repo.clone_from(clone_url, repo_path, depth=1)
                log.info(f"[Shard {shard_id}] {org_name}/{repo_name} - ✓")

                # Create output work item for success
                workitems.outputs.create({
                    "name": repo_name, "url": url, "org": org_name,
                    "status": "success",
                    "commit_hash": repo.head.commit.hexsha[:8]
                })
                item.done()

            except GitCommandError as git_err:
                # Handle network errors differently (release for retry)
                if "network" in str(git_err).lower():
                    log.warn(f"Network error, releasing for retry")
                    workitems.outputs.create({
                        "name": repo_name, "status": "released",
                        "error": str(git_err)
                    })
                    # Don't mark done/failed - allows retry
                else:
                    item.fail("BUSINESS", code="GIT_ERROR", message=str(git_err))

        except Exception as e:
            item.fail("APPLICATION", code="UNEXPECTED_ERROR", message=str(e))

    # Generate report and zip
    # ...
```

**tasks.py - Reporter (key excerpts):**
```python
@task
def reporter():
    """Generate comprehensive reports on work item processing status."""
    summary_stats = {
        "total_items_processed": 0,
        "successful_items": 0,
        "failed_items": 0,
        "released_items": 0,
        "organizations": set(),
        "repositories": [],
    }

    for item in workitems.inputs:
        try:
            payload = item.payload
            status = payload.get("status", "unknown")

            summary_stats["total_items_processed"] += 1
            if status == "success":
                summary_stats["successful_items"] += 1
            elif status == "failed":
                summary_stats["failed_items"] += 1
            elif status == "released":
                summary_stats["released_items"] += 1

            item.done()
        except Exception as e:
            item.fail("APPLICATION", code="UNEXPECTED_ERROR", message=str(e))

    # Calculate success rate and log summary
    success_rate = (summary_stats["successful_items"] /
                    summary_stats["total_items_processed"] * 100)
    log.info(f"✅ Successful: {summary_stats['successful_items']}")
    log.info(f"❌ Failed: {summary_stats['failed_items']}")
    log.info(f"📊 Success rate: {success_rate:.1f}%")
```

**Key Patterns Demonstrated:**
- **Work item validation** - Check payload type and required fields
- **Error classification** - BUSINESS vs APPLICATION errors
- **Graceful retry** - Release items for retry on transient errors
- **Parallel processing** - Shard ID for scaling across workers
- **Output chaining** - Producer → Consumer → Reporter pipeline
- **Custom adapters** - Redis/SQLite/MongoDB work item backends

---

### Producer-Consumer Pattern (from robocorp/example-advanced-python-template)

**Source:** https://github.com/robocorp/example-advanced-python-template

**robot.yaml:**
```yaml
tasks:
  Produce:
    shell: python -m robocorp.tasks run tasks -t "producer"

  Consume:
    shell: python -m robocorp.tasks run tasks -t "consumer"

  Report:
    shell: python -m robocorp.tasks run tasks -t "reporter"

  UNIT TESTS:
    shell: python -m pytest -v tests

devTasks: {}

environmentConfigs:
  - environment_windows_amd64_freeze.yaml
  - environment_linux_amd64_freeze.yaml
  - environment_darwin_amd64_freeze.yaml
  - conda.yaml

ignoreFiles:
  - .gitignore
artifactsDir: output
PATH:
  - .
PYTHONPATH:
  - .
```

**tasks/producer_tasks.py:**
```python
"""Producer task using robocorp.tasks and robocorp.workitems."""
from pathlib import Path

from robocorp import log, workitems, excel
from robocorp.tasks import task
from robocorp.excel import tables

from . import ARTIFACTS_DIR, setup_log

INPUT_FILE_NAME = "orders.csv"

@task
def producer():
    setup_log()
    log.info("Producer task started.")

    # Get input file from current work item
    work_item = workitems.inputs.current
    destination_path = Path(ARTIFACTS_DIR) / INPUT_FILE_NAME
    input_filepath = work_item.get_file(INPUT_FILE_NAME, destination_path)
    log.info(f"Reading orders from {input_filepath}")

    # Parse CSV and group by customer
    table = tables.Tables().read_table_from_csv(
        str(input_filepath), encoding="utf-8-sig"
    )
    customers = table.group_by_column("Name")
    log.info(f"Found {len(table)} rows. Creating work items.")

    # Create work items for each customer
    for customer in customers:
        work_item_vars = {
            "Name": customer.get_column("Name")[0],
            "Zip": customer.get_column("Zip")[0],
            "Items": [row["Item"] for row in customer],
        }
        workitems.outputs.create(work_item_vars, save=True)

    log.info("Producer task completed.")
```

**tasks/consumer_tasks.py:**
```python
"""Consumer task with proper work item context management."""
from robocorp import log, workitems
from robocorp.tasks import task

from . import setup_log, get_secret
from libs.web.swaglabs import Swaglabs

def process_order(swaglabs: Swaglabs, work_item: workitems.Input) -> None:
    """Process a single order (work item)."""
    log.info(f"Processing work item {work_item.id}")
    swaglabs.clear_cart()
    swaglabs.go_to_order_screen()

    payload = work_item.payload
    set_items = set(payload.get("Items", []))
    log.info(f"Ordering {len(set_items)} items for {payload.get('Name')}")

    for item in set_items:
        swaglabs.add_item_to_cart(item)

    # Submit order
    first_name = payload.get("Name", "").split(" ")[0]
    last_name = payload.get("Name", "").split(" ")[1]
    order_number = swaglabs.submit_order(first_name, last_name, payload.get("Zip", ""))
    log.info(f"Order submitted for work item {work_item.id}")

    # Create output work item for reporter step
    output = work_item.create_output()
    output.payload = {
        "Name": payload.get("Name"),
        "Items": list(set_items),
        "OrderNumber": order_number,
    }
    output.save()

@task
def consumer():
    setup_log()
    log.info("Consumer task started.")
    credentials = get_secret("swaglabs")

    with Swaglabs(
        credentials["username"], credentials["password"], credentials["url"]
    ) as swaglabs:
        # Context manager ensures proper work item release
        for work_item in workitems.inputs:
            with work_item:
                process_order(swaglabs, work_item)
            log.info(f"Work item released with state '{work_item.state}'.")
```

---

### Sema4AI Actions - Google Sheets (from sema4ai/gallery)

**Source:** https://github.com/sema4ai/gallery/tree/main/actions/google-sheets

The gallery package is a good baseline for OAuth2 + Google Sheets workflows.
For new packages, prefer current dependency pins and typed `Response` subclasses.

**package.yaml (modernized baseline):**
```yaml
name: Google Sheets
description: Create and read spreadsheets. Add or update rows.
version: 1.2.0
spec-version: v2

dependencies:
  conda-forge:
    - python=3.12.11
    - python-dotenv=1.1.1
    - uv=0.9.28
  pypi:
    - sema4ai-actions=1.6.6
    - pydantic=2.11.7
    - gspread=6.2.1

external-endpoints:
  - name: Google API
    description: Access Google API resources.
    additional-info-link: https://developers.google.com/explorer-help
    rules:
      - host: www.googleapis.com
        port: 443

packaging:
  exclude:
    - ./.git/**
    - ./devdata/**
    - ./output/**
    - ./**/*.pyc
```

Version note: pins above are current as of 2026-02-07.

**actions.py (typed response model pattern):**
```python
from typing import Annotated, Literal

import gspread
from pydantic import BaseModel, Field
from sema4ai.actions import ActionError, OAuth2Secret, Response, action


class Row(BaseModel):
    columns: Annotated[list[str], Field(description="Columns that make up one row")]


class RowData(BaseModel):
    rows: Annotated[list[Row], Field(description="Rows to add or update")]

    def to_raw_data(self) -> list[list[str]]:
        return [row.columns for row in self.rows]


class SpreadsheetMutationResponse(Response[str]):
    spreadsheet: str = ""
    worksheet: str | None = None
    rows_written: int = 0


class SheetContentResponse(Response[str]):
    spreadsheet: str = ""
    worksheet: str = ""
    from_row: int = 1
    limit: int = 100
    row_count: int = 0


@action(is_consequential=True)
def create_spreadsheet(
    oauth_access_token: OAuth2Secret[
        Literal["google"],
        list[
            Literal[
                "https://www.googleapis.com/auth/spreadsheets",
                "https://www.googleapis.com/auth/drive.file",
            ]
        ],
    ],
    name: str,
) -> SpreadsheetMutationResponse:
    gc = gspread.authorize(_Credentials.from_oauth2_secret(oauth_access_token))
    spreadsheet_obj = gc.create(name)
    return SpreadsheetMutationResponse(
        result=f"Created spreadsheet: {spreadsheet_obj.url}",
        spreadsheet=spreadsheet_obj.title,
    )


@action(is_consequential=False)
def get_sheet_content(
    oauth_access_token: OAuth2Secret[
        Literal["google"],
        list[
            Literal[
                "https://www.googleapis.com/auth/spreadsheets.readonly",
                "https://www.googleapis.com/auth/drive.readonly",
            ]
        ],
    ],
    spreadsheet: str,
    worksheet: str,
    from_row: int = 1,
    limit: int = 100,
) -> SheetContentResponse:
    gc = gspread.authorize(_Credentials.from_oauth2_secret(oauth_access_token))
    spreadsheet_obj = _open_spreadsheet(gc, spreadsheet)
    ws = spreadsheet_obj.worksheet(worksheet)

    content = _get_sheet_content(ws, from_row, limit)
    row_count = len([line for line in content.splitlines() if line.strip()])
    return SheetContentResponse(
        result=content,
        spreadsheet=spreadsheet,
        worksheet=worksheet,
        from_row=from_row,
        limit=limit,
        row_count=row_count,
    )


@action(is_consequential=True)
def add_sheet_rows(
    oauth_access_token: OAuth2Secret[
        Literal["google"],
        list[
            Literal[
                "https://www.googleapis.com/auth/spreadsheets",
                "https://www.googleapis.com/auth/drive.file",
            ]
        ],
    ],
    spreadsheet: str,
    worksheet: str,
    rows_to_add: RowData,
) -> SpreadsheetMutationResponse:
    if not rows_to_add.rows:
        raise ActionError("rows_to_add must contain at least one row")

    gc = gspread.authorize(_Credentials.from_oauth2_secret(oauth_access_token))
    spreadsheet_obj = _open_spreadsheet(gc, spreadsheet)
    ws = spreadsheet_obj.worksheet(worksheet)
    ws.append_rows(values=rows_to_add.to_raw_data())

    return SpreadsheetMutationResponse(
        result="Rows appended successfully",
        spreadsheet=spreadsheet,
        worksheet=worksheet,
        rows_written=len(rows_to_add.rows),
    )
```

This style mirrors your LinkedIn example: each action returns a concrete response model that adds validated metadata in addition to `result`/`error`.

---

### Community Fork: `actions-work-items` Drop-In Pattern

**Source:** `joshyorko/actions` `community` branch (`work-items/` package)

Use this when you want `robocorp-workitems` style producer-consumer flows outside classic robot-only contexts.

**conda.yaml (pip section):**
```yaml
dependencies:
  - python=3.12.11
  - uv=0.9.28
  - pip:
      - actions-work-items==0.2.1
```

**Python usage:**
```python
from actions.work_items import inputs, outputs

def process_queue() -> None:
    for item in inputs:
        with item:
            payload = item.payload
            outputs.create(payload={"processed": True, "source": payload})
```

**Migration map:**
- before: `from robocorp import workitems`
- after: `from actions.work_items import inputs, outputs`

---

### Available Gallery Actions (sema4ai/gallery)

The Sema4ai Gallery provides production-ready actions for many integrations:

| Action Package | Description |
|----------------|-------------|
| `google-sheets` | Create/read spreadsheets, add/update rows |
| `google-mail` | Send/read Gmail with OAuth2 |
| `google-calendar` | Manage Google Calendar events |
| `google-drive` | File operations on Google Drive |
| `microsoft-mail` | Outlook email operations |
| `microsoft-excel` | Excel file operations via OneDrive |
| `microsoft-calendar` | Outlook calendar management |
| `microsoft-sharepoint` | SharePoint document operations |
| `microsoft-teams` | Teams messaging |
| `slack` | Slack messaging and channel management |
| `hubspot` | CRM operations |
| `salesforce` | Salesforce CRM operations |
| `servicenow` | ITSM ticket management |
| `zendesk` | Support ticket operations |
| `snowflake-*` | Snowflake data/AI operations |
| `pdf` | PDF manipulation |
| `excel` | Local Excel file operations |
| `browsing` | Web automation |

**Browse all:** https://github.com/sema4ai/gallery/tree/main/actions

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
