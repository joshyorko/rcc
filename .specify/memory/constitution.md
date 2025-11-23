<!--
Sync Impact Report:
- Version change: 0.0.0 -> 1.0.0
- Added principles: Environment Isolation, Cross-Platform Compatibility, Build Patience & Stability, Embedded Assets, CLI-First
- Added sections: Technical Stack, Development Workflow
- Templates requiring updates: None (templates are generic and reference constitution dynamically)
-->
# RCC Constitution

## Core Principles

### I. Environment Isolation & Reproducibility
RCC's primary purpose is to create, manage, and distribute Python-based automation packages with isolated environments. It MUST ensure that automations run in self-contained environments (using conda/micromamba) that are reproducible across different machines. "Works on my machine" is the problem RCC solves.

### II. Cross-Platform Compatibility
RCC MUST support Linux, Windows, and macOS. All features and builds MUST be verified on all three platforms. Platform-specific code is permitted but must be handled via build tags or runtime checks to ensure no regression on other platforms.

### III. Build Patience & Stability (NON-NEGOTIABLE)
Build and test processes MUST NOT be cancelled prematurely. Go builds, while generally fast, can be slow on first run. Robot Framework tests can take 5-30 minutes. Timeouts MUST be set to at least 2-3x the estimated duration (e.g., 60+ minutes for full builds). "NEVER CANCEL" is a strict operational rule.

### IV. Embedded Assets
RCC relies on embedded assets (in `blobs/`) to function as a single binary. The build process MUST include the preparation of these assets (using `inv support` or equivalent). Changes to assets MUST be reflected in the embedded blobs.

### V. CLI-First
RCC is a command-line tool. All functionality MUST be exposed via the CLI. The interface should be consistent, using standard flags and subcommands. Output should be machine-readable where appropriate (e.g., JSON output for automation integration).

## Technical Stack

**Language**: Go 1.20+ (Core), Python 3.10+ (Environment Management/Scripting).
**Build System**: Invoke (`tasks.py`).
**Testing**: Go `testing` package (Unit), Robot Framework (Acceptance).
**Dependencies**: Micromamba (for Python envs), Cobra (CLI), Viper (Config).

## Development Workflow

**Build**: Use `inv build` (or `rcc run -r developer/toolkit.yaml --dev -t build`).
**Test**: Use `inv test` (Unit) and `inv robot` (Acceptance).
**Assets**: Ensure `blobs/` are updated if assets change.
**CI/CD**: GitHub Actions workflows must pass on all platforms.

## Governance

This constitution supersedes all other practices. Amendments require documentation, approval, and a migration plan.

**Compliance**: All PRs and reviews must verify compliance with these principles.
**Complexity**: Any deviation from the standard stack or workflow must be justified.
**Guidance**: Refer to `.github/copilot-instructions.md` for detailed runtime development guidance.

**Version**: 1.0.0 | **Ratified**: 2025-11-23 | **Last Amended**: 2025-11-23
