# Repository Guidelines

## Project Shape

This is Josh's maintained fork of RCC, a Go CLI for creating, caching, and running contained automation environments from `robot.yaml` and `conda.yaml`. Treat this checkout as the current RCC source of truth. Upstream Robocorp/Sema4.ai docs are compatibility history unless a task explicitly asks for upstream alignment.

Key areas:

- `cmd/`: Cobra commands and platform command wiring.
- `operations/`: user-visible run, pull, auth, diagnostics, bundle, and release behavior.
- `conda/`, `htfs/`, `remotree/`: environment creation, holotree storage, archives, and remote cache behavior.
- `common/`, `settings/`, `xviper/`, `pathlib/`, `shell/`: shared platform, config, logging, path, and process helpers.
- `assets/`, `templates/`, `docs/`: source inputs embedded into the binary.
- `blobs/` and `build/`: generated output. Do not hand-edit these.
- `developer/`: contained toolkit; `robot_tests/`: Robot Framework acceptance tests.

## Environment And Commands

Josh is usually on Bluefin Linux. Do not mutate the host just to get Go, Python, or Robot Framework. This repo does not currently ship a devcontainer, so prefer the checked-in toolkit when host tools are missing:

```bash
rcc run -r developer/toolkit.yaml --dev -t tools
rcc run -r developer/toolkit.yaml --dev -t unitTests
rcc run -r developer/toolkit.yaml --dev -t local
rcc run -r developer/toolkit.yaml -t robot
```

If the host already has the pinned toolchain, `inv` is fine. `developer/setup.yaml`, CI, and `go.mod` pin Go `1.25.7`; `developer/setup.yaml` also pins Python `3.10.15`, Invoke `2.2.0`, Robot Framework `6.1.1`, and Git `2.46.0`.

```bash
inv assets
GOARCH=amd64 CGO_ENABLED=0 go test ./...
inv local
inv robot
```

`inv test` sets `GOARCH=amd64` and `CGO_ENABLED=0`; set them yourself for direct `go test`. `inv local` builds into `build/`; `inv build` cross-builds Linux, Windows, and macOS artifacts. `inv robot` writes reports to `tmp/output/`.

## Editing Rules

Edit `assets/*`, `assets/man/*`, `templates/*`, or `docs/*.md`, then run `inv assets` or `rcc run -r developer/toolkit.yaml --dev -t assets`. Do not manually patch generated files under `blobs/`. Micromamba assets are prepared through `tasks.py`; use `RCC_MICROMAMBA_BASE` only when that download source must be overridden.

Keep existing RCC idioms:

```go
defer fail.Around(&err)
fail.On(condition, "message: %v", err)
fail.Fast(err)

common.Log("message: %s", value)
common.Debug("debug: %s", value)
common.Trace("trace: %s", value)

must_be, wont_be := hamlet.Specifications(t)
must_be.Equal(expected, actual)
```

Prefer `fail` for error handling, `common.Log/Debug/Trace` for CLI output, and `hamlet` assertions in tests. Run `gofmt` on changed Go files.

## Fork-Specific Rules

- Telemetry stays disabled. Do not add background metrics, tracking, or installation identifiers.
- Endpoints must stay configurable through `RCC_ENDPOINT_*` and `RCC_AUTOUPDATES_*`; do not hardcode service URLs.
- Treat `ROBOCORP_HOME` as the primary RCC home/cache boundary unless current source proves otherwise.
- Keep platform-specific behavior in the existing platform files and packages.
- Avoid live network behavior in tests unless explicitly scoped and isolated.

## Verification

Run the narrowest relevant checks and report them: docs-only `git diff --check`; Go logic `GOARCH=amd64 CGO_ENABLED=0 go test ./...` or toolkit `unitTests`; build-sensitive changes `inv local` or toolkit `local`; runtime/environment changes `inv robot` or toolkit `robot`, then inspect `tmp/output/log.html` on failure.
