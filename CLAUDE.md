# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RCC (Repeatable, Contained Code) is a Go CLI tool for creating, managing, and distributing Python-based self-contained automation packages. It provides isolated Python environments for automation without requiring Python installation on target machines.

## Build Commands

```bash
# List available Invoke tasks
inv -l

# Fast local build for current OS
GOARCH=amd64 go build -o build/ ./cmd/...

# Cross-platform build (runs assets first if needed)
inv build

# Prepare assets (micromamba, templates, docs) - required before building
inv assets

# Build for specific platforms
inv linux64
inv macos64
inv windows64
```

## Testing

```bash
# Unit tests
GOARCH=amd64 go test ./...

# Unit tests with coverage
inv test --cover
# Coverage report in tmp/cover.out

# Robot Framework acceptance tests
python3 -m robot -L DEBUG -d tmp/output robot_tests

# Alternative: run via rcc itself
rcc run -r developer/toolkit.yaml --dev -t robot
```

## Project Structure

- `cmd/` - CLI entrypoints and Cobra commands
- `operations/` - Core behaviors (auth, bundles, diagnostics)
- `common/`, `pathlib/`, `shell/` - Shared helpers
- `conda/` - Environment packaging
- `robot/`, `wizard/`, `templates/` - Robot automation helpers
- `robot_tests/` - Acceptance tests and fixtures
- `blobs/` - Generated assets (do not edit directly)
- `build/` - Build outputs
- `assets/` - Source files for blobs (edit here, then regenerate)

## Coding Conventions

- Go 1.20; format with `gofmt`
- Packages/files: lowercase without underscores
- Exported names: PascalCase; locals: mixedCaps
- CLI flags/commands follow verb-first patterns (`run`, `pull`, `configure`)
- Table-driven tests; avoid platform-specific logic leaks across `command_*.go` files
- Unit tests go beside code in `_test.go` files

## Configuration & Endpoints

Telemetry is disabled by default in this fork. Endpoints can be overridden via environment variables:
- `RCC_ENDPOINT_CLOUD_API`, `RCC_ENDPOINT_CLOUD_UI`, `RCC_ENDPOINT_CLOUD_LINKING`
- `RCC_ENDPOINT_DOWNLOADS`, `RCC_ENDPOINT_DOCS`
- `RCC_ENDPOINT_PYPI`, `RCC_ENDPOINT_PYPI_TRUSTED`, `RCC_ENDPOINT_CONDA`
- `RCC_AUTOUPDATES_TEMPLATES` - Override the templates.yaml URL
- `RCC_AUTOUPDATES_RCC_INDEX` - Override the index.json URL for version checking

Or via `settings.yaml` in `$ROBOCORP_HOME`.

## Asset Pipeline

- Source edits belong in `assets/` and Go packages
- Run `inv assets` to regenerate `blobs/`
- Micromamba is embedded and extracted at runtime; change download base via `RCC_DOWNLOADS_BASE`

## Active Technologies
- Go 1.20 + Cobra (CLI), go-isatty (terminal detection), existing `pretty` and `wizard` packages (002-ui-wizard-enhancements)
- N/A (no data persistence changes) (002-ui-wizard-enhancements)

## Recent Changes
- 002-ui-wizard-enhancements: Added Go 1.20 + Cobra (CLI), go-isatty (terminal detection), existing `pretty` and `wizard` packages
