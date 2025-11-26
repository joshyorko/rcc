# Repository Guidelines

## Project Structure & Module Organization
- Go module lives at the repo root; CLI entrypoints and Cobra commands are in `cmd/`.
- Core behaviors (cloud auth, bundle handling, diagnostics) sit under `operations/`; shared helpers such as versioning, logging, and path utilities are in `common/`, `pathlib/`, and `shell/`.
- Environment packaging logic is in `conda/`; robot-automation helpers live in `robot/`, `wizard/`, and `templates/`.
- Acceptance tests and fixtures are under `robot_tests/`; generated assets land in `blobs/` and build outputs in `build/`.
- Documentation lives in `docs/` and `developer/`; keep `assets/` as the source for files copied into `blobs/`.

## Build, Test, and Development Commands
- List available Invoke tasks: `inv -l`.
- Fast local build (current OS): `go build -o build/ ./cmd/...` (set `GOARCH=amd64` if not already).
- Cross-platform binaries and assets: `inv build` (runs go builds under `CGO_ENABLED=0`, `GOARCH=amd64`) after `inv assets` if blobs are stale.
- Unit tests: `GOARCH=amd64 go test ./...` or `inv test --cover` for a coverage report in `tmp/cover.out`.
- Robot acceptance tests (needs Python deps from `robot_requirements.txt`): `python3 -m robot -L DEBUG -d tmp/output robot_tests` or `rcc run -r developer/toolkit.yaml --dev -t robot`.

## Coding Style & Naming Conventions
- Use Go 1.20 tooling; format with `gofmt` before committing.
- Keep packages and files lowercase with underscores avoided; exported names follow Go’s PascalCase, locals use mixedCaps.
- Prefer small, composable functions and table-driven tests; avoid platform-specific logic leaks across `command_*.go` files.
- CLI flag and command names should stay consistent with existing verb-first patterns (`run`, `pull`, `configure`, etc.).

## Testing Guidelines
- Place unit tests beside code in `_test.go` files; mirror package names and use clear subtests for scenarios.
- Keep tests deterministic (no network calls unless explicitly stubbed) and clean temporary files under `tmp/` or `tmp/output`.
- Robot suites in `robot_tests/` produce HTML logs; inspect `tmp/output/log.html` on failures and keep new suites platform-aware.

## Commit & Pull Request Guidelines
- Follow the existing short, imperative commit style (e.g., `Update permissions in create-release-tag workflow`, `Implement robot bundle unpack command`); keep subjects under ~72 characters.
- Before opening a PR, note the tests you ran (`go test`, `inv test --cover`, robot suites) and any artifacts produced.
- Link related issues, describe behavioral changes, and include sample commands/output when the change affects CLI UX; attach screenshots only when altering user-facing docs.

## Security & Configuration Notes
- Do not hardcode endpoints or credentials; prefer the environment variable overrides documented in `README.md` (`RCC_ENDPOINT_*`) or `settings.yaml` under `ROBOCORP_HOME`.
- Treat `blobs/` and `build/` as generated; source edits belong in `assets/` and Go packages, then regenerated via Invoke tasks.
