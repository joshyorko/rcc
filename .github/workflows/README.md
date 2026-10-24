# GitHub Actions Workflows

This directory contains the CI/CD workflows for the RCC (Repeatable, Contained Code) project. Below is an overview of each workflow, its purpose, and how it integrates into the development lifecycle.

## Workflow Overview

`recover-rcc-185.yml` is a retired, read-only manual hold. It exits without
creating a tag or release and without dispatching Homebrew. The historical
recovery script and evidence remain in the repository for audit; they are not
an executable publication route. Do not dispatch this workflow as a release
operation.

| Workflow | Purpose | Trigger |
|----------|---------|---------|
| [rcc.yaml](#rccyaml) | Main CI and manual release preparation | Push to `main`, PRs, manual |
| [create-release-tag.yml](#create-release-tagyml) | Read-only migration hold | Manual |
| [codeql-analysis.yml](#codeql-analysisyml) | Security scanning | Push/PR to `master`, weekly |
| [dagger.yaml](#daggeryaml) | Dagger-based testing | Manual |
| [copilot-setup-steps.yml](#copilot-setup-stepsyml) | Copilot environment setup | Manual, push/PR to this file |
| [codex-pr-review.txt](#codex-pr-reviewtxt) | AI-powered PR reviews | PR events (template/disabled) |

---

## rcc.yaml

**Main CI/CD Pipeline**

The primary workflow for building and testing RCC across multiple platforms and
for preparing an immutable release-review artifact.

### Triggers
- **Push** to `main` branch
- **Pull requests** to `main` branch; the release-candidate gate is opt-in through
  the `release-candidate` label. Adding the label runs it, and later PR commits
  rerun it while the label remains. This is the full release acceptance suite,
  so apply the label when that hosted-runner cost is useful.

> Note: Changes to `.github/workflows/` and `.dagger/` directories are ignored.

### Jobs

#### 1. Build (`build`)
- **Runner:** `ubuntu-latest`
- **Condition:** Runs on pushes and pull requests to `main`
- **Steps:**
  - Checkout code
  - Set up Go 1.26.9 and Python 3.10
  - Install Invoke build tool
  - Build RCC using `inv build`
  - Upload the eight `rcc` and `rccremote` binaries for Linux, Windows, and macOS

#### 2. Release Candidate Verification (`release-candidate`)
- **Runner:** `ubuntu-latest`
- **Condition:** Runs for manual preparation from `main` and pull requests
  labeled `release-candidate`. It checks out the exact source and does not
  publish a release or use publication credentials.
- **Steps:**
  - Download and checksum the pinned N-1 RCC v18.19.5 binary
  - Run `releaseCandidate` through `developer/toolkit.yaml`
  - Prove the real A/B artifact vertical, full Robot suite, binary inventory,
    race tests, and two-generation self-host
  - Upload the built candidate binary, gate exit/log, N-1 archive, Robot output,
    and reviewable self-host and acceptance receipts (never private homes)

#### 3. Robot Tests (`robot`)
- **Matrix:** Ubuntu, macOS, and Windows runners
- **Steps:**
  - Set up development environment
  - Install dependencies
  - Run Robot Framework acceptance tests
  - Upload test reports as artifacts

#### 4. Assemble Release Preparation (`assemble-release-preparation`)
- **Condition:** Manual dispatch from `main` after build, candidate, and native
  gates succeed
- **Output:** An immutable artifact with eight binaries, `index.json`, manifest,
  checksums, provenance, and source-bound test receipts. This job does not
  create a tag or publish a release.

### Required Secrets
- `HOMEBREW_TOOLS_PAT` - required before publication so the release can dispatch
  the `rcc-daily` Homebrew handoff without leaving a published partial release

---

## create-release-tag.yml

**Read-only migration hold**

This legacy manual entry exits with a migration message. It has read-only
permissions and cannot create, move, or recreate a tag. Use the separately
reviewed preparation and protected publication procedure in
[`docs/release-process.md`](../docs/release-process.md).

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
  - Run Dagger pipeline: `dagger call run-robot-tests --source .`
  - Uses Dagger engine v0.21.3 from `dagger.json` and Go 1.26.9

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
  2. Download and checksum the pinned RCC v18.17.7 Linux64 binary
  3. Verify installation with `rcc version`
  4. Display environment info using `rcc ht vars developer/setup.yaml`

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

Follow [`docs/release-process.md`](../docs/release-process.md) for exact-source
preparation, artifact review, and the separate protected publication gate.
Neither a tag push nor the legacy manual hold publishes a release.

## Environment Requirements

| Requirement | Version | Used By |
|-------------|---------|---------|
| Go | 1.26.9 | rcc.yaml and Dagger |
| Python | 3.10 | rcc.yaml |
| Invoke | 2.2.0 | rcc.yaml |
| Dagger | v0.21.3 | `dagger.json` |



## Troubleshooting

### Build failures
- Ensure `inv assets` has been run to generate required blobs
- Check Go and Python versions match requirements

### Robot test failures
- Review test reports in workflow artifacts
- Run tests locally: `python3 -m robot -L DEBUG -d tmp/output robot_tests`

### Release preparation or publication blocked
- Confirm the selected main SHA, source version, and changelog heading match.
- Review the preparation artifact and its full native, consumer, and
  self-host receipts before considering the protected publication workflow.

## Contributing

When modifying workflows:
1. Test changes in a fork first
2. Use `workflow_dispatch` for manual testing
3. Keep workflow changes in separate PRs from code changes
