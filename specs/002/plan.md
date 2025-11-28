# Implementation Plan: Robot Bundle Unpack

**Branch**: `001-robot-bundle-unpack` | **Date**: 2025-11-23 | **Spec**: [specs/001-robot-bundle-unpack/spec.md](spec.md)
**Input**: Feature specification from `/specs/001-robot-bundle-unpack/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Implement a new CLI command `rcc robot unpack` that allows users to extract the contents of the `robot/` directory from a robot bundle (zip file) to a specified local directory. This feature enables inspection and modification of bundled code. The implementation will reuse existing bundle extraction logic where possible, ensuring cross-platform compatibility and safe file handling.

## Technical Context

**Language/Version**: Go 1.20+
**Primary Dependencies**: `github.com/spf13/cobra` (CLI), `archive/zip` (Standard Lib), `github.com/robocorp/rcc/common` (Logging/Utils)
**Storage**: Filesystem (Read zip, Write files)
**Testing**: Go `testing` package (Unit tests for extraction logic and command execution)
**Target Platform**: Linux, Windows, macOS
**Project Type**: CLI Tool
**Performance Goals**: Comparable to standard unzip tools.
**Constraints**: Must handle Zip Slip vulnerabilities (already handled in existing logic). Must support Windows paths.
**Scale/Scope**: Single command implementation.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

*   **I. Environment Isolation**: N/A (File operation only).
*   **II. Cross-Platform Compatibility**: **PASS**. Will use `path/filepath` for OS-agnostic path handling.
*   **III. Build Patience**: N/A.
*   **IV. Embedded Assets**: N/A.
*   **V. CLI-First**: **PASS**. Feature is a new CLI command.

## Project Structure

### Documentation (this feature)

```text
specs/001-robot-bundle-unpack/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output (N/A for this feature)
├── quickstart.md        # Phase 1 output (Usage guide)
├── contracts/           # Phase 1 output (CLI Interface)
└── tasks.md             # Phase 2 output
```

### Source Code (repository root)

```text
cmd/
├── robotUnpack.go       # NEW: Implementation of 'rcc robot unpack'
├── robotRunFromBundle.go # MODIFY: Refactor to share extraction logic
└── bundle_utils.go      # NEW/MODIFY: Shared logic for bundle operations (optional, or keep in robotRunFromBundle.go if exported)
```
