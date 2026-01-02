# GitHub Actions Workflows

This directory contains the CI/CD workflows for the RCC (Repeatable, Contained Code) project. Below is an overview of each workflow, its purpose, and how it integrates into the development lifecycle.

## Workflow Overview

| Workflow | Purpose | Trigger |
|----------|---------|---------|
| [rcc.yaml](#rccyaml) | Main CI/CD pipeline | Push to `main`, version tags, PRs |
| [create-release-tag.yml](#create-release-tagyml) | Trigger release process | Manual |
| [codeql-analysis.yml](#codeql-analysisyml) | Security scanning | Push/PR to `master`, weekly |
| [dagger.yaml](#daggeryaml) | Dagger-based testing | Manual |
| [copilot-setup-steps.yml](#copilot-setup-stepsyml) | Copilot environment setup | Manual, push/PR to this file |
| [codex-pr-review.txt](#codex-pr-reviewtxt) | AI-powered PR reviews | PR events (template/disabled) |

---

## rcc.yaml

**Main CI/CD Pipeline**

The primary workflow for building, testing, and releasing RCC across multiple platforms.

### Triggers
- **Push** to `main` branch
- **Version tags** matching `v*` pattern
- **Pull requests** to `main` branch
- **Manual dispatch** with optional version input

> Note: Changes to `.github/workflows/` and `.dagger/` directories are ignored.

### Jobs

#### 1. Build (`build`)
- **Runner:** `ubuntu-latest`
- **Condition:** Only runs on version tag pushes or manual dispatch
- **Steps:**
  - Checkout code
  - Set up Go 1.23 and Python 3.10
  - Install Invoke build tool
  - Build RCC using `inv build`
  - Upload artifacts for Linux, Windows, and macOS

#### 2. Robot Tests (`robot`)
- **Matrix:** Kubernetes and Windows runners
- **Steps:**
  - Set up development environment
  - Install dependencies
  - Run Robot Framework acceptance tests
  - Upload test reports as artifacts

#### 3. Release (`release`)
- **Condition:** Runs after successful build and robot test jobs
- **Steps:**
  - Download built RCC binaries
  - Generate `index.json` with version metadata
  - Publish GitHub Release with all platform binaries

### Required Secrets
None explicitly required (uses GitHub token for releases).

---

## create-release-tag.yml

**Release Trigger Workflow**

A manual workflow that extracts the version from source code and triggers the main release pipeline.

### Triggers
- **Manual dispatch only** (`workflow_dispatch`)

### How It Works
1. Checks out the repository with full Git history
2. Extracts version from `common/version.go`
3. Triggers the `rcc.yaml` workflow with the extracted version

### Permissions
- `contents: write` - For repository access
- `actions: write` - For triggering other workflows

### Usage
1. Navigate to Actions > "Create Release Tag"
2. Click "Run workflow"
3. The workflow will read the version and trigger a release

---

## codeql-analysis.yml

**Security Code Scanning**

Automated security vulnerability detection using GitHub's CodeQL analysis engine.

### Triggers
- **Push** to `master` branch
- **Pull requests** to `master` branch
- **Scheduled:** Every Monday at 10:24 UTC

### Configuration
- **Language:** Go
- **Analysis:** Autobuild with CodeQL defaults
- **Fail-fast:** Disabled (continues even if analysis encounters issues)

### Permissions
- `actions: read`
- `contents: read`
- `security-events: write`

### Results
Security findings appear in the repository's Security tab under "Code scanning alerts."

---

## dagger.yaml

**Dagger CI Pipeline**

Container-based testing using [Dagger](https://dagger.io/), a portable CI/CD engine.

### Triggers
- **Manual dispatch only** (`workflow_dispatch`)

> Note: Push and PR triggers are commented out but can be enabled.

### Jobs

#### Test (`test`)
- **Runner:** `ubuntu-latest`
- **Steps:**
  - Checkout code
  - Run Dagger pipeline: `dagger call test --source .`
  - Uses latest Dagger version

### Enabling Automatic Runs
To enable automatic testing, uncomment the push/PR triggers in the workflow file:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  workflow_dispatch:
```

---

## copilot-setup-steps.yml

**GitHub Copilot Environment Setup**

Validates that RCC can be installed and run in a clean CI environment. Useful for Copilot coding agent integration.

### Triggers
- **Manual dispatch** (`workflow_dispatch`)
- **Push/PR** to this workflow file

### Jobs

#### Setup (`copilot-setup-steps`)
- **Runner:** `ubuntu-latest`
- **Steps:**
  1. Checkout repository
  2. Download and install latest RCC Linux64 binary
  3. Verify installation with `rcc version`
  4. Display environment info using `rcc run -r developer/toolkit.yaml holotree vars`

### Permissions
- `contents: read`

---

## codex-pr-review.txt

**AI-Powered PR Review (Template)**

> Note: This file has a `.txt` extension, indicating it may be disabled or serve as a template.

Uses OpenAI's Codex to automatically review pull requests and provide feedback.

### Triggers (if enabled)
- **Pull request events:** `opened`, `reopened`, `ready_for_review`, `synchronize`

### Jobs

#### 1. Codex (`codex`)
- Reviews PR changes focusing on:
  - Behavior changes to RCC CLI
  - Breaking changes across platforms
  - Potential bugs and missing tests
- Uses `openai/codex-action@v1`

#### 2. Post Feedback (`post_feedback`)
- Posts Codex review as a PR comment

### Required Secrets
- `OPENAI_API_KEY` - OpenAI API key for Codex access

### Enabling This Workflow
Rename the file from `.txt` to `.yml`:
```bash
mv codex-pr-review.txt codex-pr-review.yml
```

---

## Release Process

The recommended release process uses these workflows:

```
┌─────────────────────────┐
│  Update version in      │
│  common/version.go      │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│  Run "Create Release    │
│  Tag" workflow manually │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│  rcc.yaml triggers      │
│  - Build all platforms  │
│  - Run robot tests      │
│  - Create GitHub release│
└─────────────────────────┘
```

## Environment Requirements

| Requirement | Version | Used By |
|-------------|---------|---------|
| Go | 1.23.x | rcc.yaml |
| Python | 3.10 | rcc.yaml |
| Invoke | latest | rcc.yaml |
| Dagger | latest | dagger.yaml |



## Troubleshooting

### Build failures
- Ensure `inv assets` has been run to generate required blobs
- Check Go and Python versions match requirements

### Robot test failures
- Review test reports in workflow artifacts
- Run tests locally: `python3 -m robot -L DEBUG -d tmp/output robot_tests`

### Release not triggering
- Verify version tag matches `v*` pattern
- Check that `create-release-tag.yml` successfully triggered `rcc.yaml`

## Contributing

When modifying workflows:
1. Test changes in a fork first
2. Use `workflow_dispatch` for manual testing
3. Keep workflow changes in separate PRs from code changes
