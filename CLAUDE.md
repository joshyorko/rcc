# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

RCC (Robocorp Control Client) is a Go-based CLI tool for creating, managing, and distributing Python-based automation packages with isolated environments. It uses conda/micromamba for Python environment management and supports cross-platform builds (Linux, Windows, macOS).

## Build Commands

Prerequisites: Go 1.20+, Python 3.10+, invoke (`python3 -m pip install invoke`)

| Command | Description |
|---------|-------------|
| `inv build` | Full cross-platform build (Linux, Windows, macOS) |
| `inv local` | Build for current platform only |
| `go build -o build/ ./cmd/...` | Direct Go build |
| `inv clean` | Remove build directory and asset files |
| `inv assets` | Prepare embedded asset files |

## Testing Commands

| Command | Description |
|---------|-------------|
| `inv test` | Run all Go unit tests |
| `inv test --cover` | Run tests with coverage |
| `GOARCH=amd64 go test ./common ./pathlib` | Run specific package tests |
| `inv robot` | Run Robot Framework acceptance tests (5-30 min) |
| `inv robotsetup` | Install Robot Framework dependencies |
| `robot -L DEBUG -d tmp/output robot_tests/some_test.robot` | Run single Robot test |

**Important:** Always set `GOARCH=amd64` when running Go tests directly, as some tests assume it.

## Repository Structure

- `cmd/` - CLI command implementations (entry point for understanding RCC functionality)
- `blobs/` - Embedded assets (micromamba binaries, templates, docs)
- `assets/` - Source files that get embedded into blobs/
- `templates/` - Robot project templates (python, standard, extended)
- `robot_tests/` - Robot Framework acceptance tests
- `developer/` - Developer toolkit for self-bootstrapping builds via rcc

## Architecture

RCC embeds micromamba binaries and templates at build time. The build process:
1. Downloads platform-specific micromamba binaries (`inv micromamba`)
2. Compresses assets and templates into blobs/
3. Go embeds blobs/ into the final binary

Key packages:
- `cmd/` - Cobra CLI commands
- `conda/` - Conda/micromamba environment management
- `htfs/` - Holotree filesystem (RCC's isolated environment system)
- `pathlib/` - Path utilities
- `common/` - Shared utilities and version info

## Telemetry

This fork disables all internal telemetry by default. No metrics are sent unless explicitly re-enabled.

## Endpoint Configuration

Override default endpoints via environment variables:
- `RCC_ENDPOINT_CLOUD_API`, `RCC_ENDPOINT_CLOUD_UI`, `RCC_ENDPOINT_CLOUD_LINKING`
- `RCC_ENDPOINT_DOWNLOADS`, `RCC_ENDPOINT_PYPI`, `RCC_ENDPOINT_CONDA`

Or configure via `$(ROBOCORP_HOME)/settings.yaml` with an `endpoints:` section.

## Build Requirements

If builds fail with "pattern assets/*.py: no matching files found":
```bash
mkdir -p blobs/assets blobs/assets/man blobs/docs
cp assets/*.py assets/*.txt assets/*.yaml blobs/assets/
cp assets/man/*.txt blobs/assets/man/
cp docs/*.md blobs/docs/
```

If micromamba downloads fail (network restrictions), create placeholders:
```bash
touch blobs/assets/micromamba.linux_amd64.gz blobs/assets/micromamba.darwin_amd64.gz blobs/assets/micromamba.windows_amd64.gz
```

## Developer Toolkit

Use rcc to self-bootstrap a consistent dev environment:
```bash
rcc run -r developer/toolkit.yaml --dev -t unitTests   # Run unit tests
rcc run -r developer/toolkit.yaml --dev -t local       # Local build
rcc run -r developer/toolkit.yaml --dev -t build       # Cross-platform build
```

## Validation After Changes

1. Build succeeds: `inv build`
2. Binary works: `./build/rcc --help` and `./build/rcc version`
3. Unit tests pass: `inv test`
4. For significant changes: `inv robot` (full acceptance tests)
