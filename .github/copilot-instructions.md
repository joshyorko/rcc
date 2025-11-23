# RCC (Robocorp Control Client) Developer Guide

RCC is a Go-based CLI tool for creating, managing, and distributing Python-based automation packages with isolated environments. It uses conda/micromamba for Python environment management.

## 🏗️ Architecture Overview

- **CLI Framework**: Built with `spf13/cobra` and `spf13/viper`. Entry point is `cmd/root.go`.
- **Component Structure**:
  - `cmd/`: CLI command implementations. Each command is typically in its own file (e.g., `cmd/create.go`).
  - `common/`: Core utilities, logging (`common.Log`), and platform abstraction.
  - `conda/`: Logic for managing Python environments and Micromamba interactions.
  - `blobs/`: **Critical** embedded assets (Micromamba binaries, templates, docs).
  - `cloud/`: API client for Control Room (Subject to major updates in this fork).
- **Data Flow**: Commands in `cmd/` parse flags -> call logic in `conda/` or `cloud/` -> use `common/` for I/O.

## ⚠️ Critical Build Requirements (Asset Embedding)

RCC **requires** embedded assets to build. The `blobs/` directory must be populated before `go build`.

**If `inv assets` fails or you see "pattern assets/*.py: no matching files found":**
1. **Manual Asset Prep**:
   ```bash
   mkdir -p blobs/assets blobs/assets/man blobs/docs
   cp assets/*.py assets/*.txt assets/*.yaml blobs/assets/ 2>/dev/null || true
   cp assets/man/*.txt blobs/assets/man/
   cp docs/*.md blobs/docs/
   ```
2. **Template Zipping** (Required):
   Run this Python snippet to zip templates into `blobs/assets/`:
   ```python
   import os, glob, zipfile
   for d in glob.glob('templates/*/'):
       base = os.path.basename(os.path.dirname(d))
       with zipfile.ZipFile(f'blobs/assets/{base}.zip', 'w', zipfile.ZIP_DEFLATED) as z:
           for r, _, fs in os.walk(d):
               for f in fs: z.write(os.path.join(r, f), os.path.relpath(os.path.join(r, f), d))
   ```
3. **Micromamba Placeholders**: If downloads fail, create empty files to bypass build errors:
   `touch blobs/assets/micromamba.linux_amd64.gz` (and for other OSs).

## 🛠️ Development Workflow

**Build System**: Uses `invoke` (`tasks.py`).
- **Full Build**: `inv build` (Cross-platform, slow ~35s). **NEVER CANCEL**.
- **Local Build**: `inv local` (Current OS, faster ~10s).
- **Direct Go Build**: `go build -o build/rcc ./cmd/...` (Fastest, but requires assets prepped).

**Testing**:
- **Unit Tests**: `inv test` or `go test ./...`. Fast.
- **Acceptance Tests**: `inv robot`. Runs Robot Framework tests in `robot_tests/`. Slow (5-15m). **NEVER CANCEL**.
  - Logs found in `tmp/output/log.html`.

## 🧰 Developer Toolkit (`developer/`)

Use the bundled toolkit to bootstrap a consistent environment and run tasks using an existing `rcc` binary.

- **Config**: `developer/toolkit.yaml` (tasks), `developer/setup.yaml` (environment).
- **Usage**: `rcc run -r developer/toolkit.yaml --dev -t <task>`
- **Common Tasks**:
  - `unitTests`: Run unit tests (sets `GOARCH=amd64`).
  - `local`: Build for current OS.
  - `robot`: Run Robot Framework smoke tests (logs in `tmp/output/log.html`).
  - `tools`: Show available tools.

## 🧩 Project Conventions

- **Telemetry**: Disabled by default in this fork (`joshyorko/rcc`). Do not re-enable without explicit instruction.
- **Platform Files**: Use `_linux.go`, `_windows.go`, `_darwin.go` for OS-specific logic.
- **Output**: Use `common.Stdout` for data and `common.Log` for status/debug info.
- **Dependencies**: Managed via `go.mod`. Go 1.20+.

## 🔍 Debugging

- **Verbose Output**: Most commands support `--trace` or `--debug`.
- **Environment**: `rcc` creates environments in `~/.rcc/` (or `%USERPROFILE%\.rcc\`).
- **Diagnostics**: `inv what` shows build info. `inv tooling` checks environment.
