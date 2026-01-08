# Repository Guidelines

## Project Structure & Module Organization
- Go module lives at the repo root; CLI entrypoints and Cobra commands are in `cmd/`.
- Core behaviors (auth, bundles, diagnostics) sit under `operations/`; shared helpers in `common/`, `pathlib/`, and `shell/`.
- Environment packaging is in `conda/`; robot automation helpers live in `robot/`, `wizard/`, and `templates/`.
- Acceptance tests and fixtures are under `robot_tests/`; generated assets land in `blobs/` and build outputs in `build/`.
- Documentation is under `docs/` and `developer/`; keep `assets/` as the source for files copied into `blobs/`.

## Build, Test, and Development Commands
- `inv -l`: list available Invoke tasks.
- `GOARCH=amd64 go build -o build/ ./cmd/...`: fast local build for the current OS.
- `inv build`: cross-platform binaries (runs with `CGO_ENABLED=0`, `GOARCH=amd64`); run `inv assets` first if blobs are stale.
- `GOARCH=amd64 go test ./...` or `inv test --cover`: unit tests; coverage report in `tmp/cover.out`.
- `python3 -m robot -L DEBUG -d tmp/output robot_tests` or `rcc run -r developer/toolkit.yaml --dev -t robot`: robot acceptance suites.

## Coding Style & Naming Conventions
- Use Go 1.20 tooling; format with `gofmt` before committing.
- Packages and files stay lowercase without underscores; exported names use PascalCase, locals use mixedCaps.
- CLI flag and command names follow verb-first patterns (e.g., `run`, `pull`, `configure`).
- Prefer small, composable functions and table-driven tests; avoid platform-specific logic leaks across `command_*.go` files.

## Testing Guidelines
- Place unit tests beside code in `_test.go` files; mirror package names and use clear subtests for scenarios.
- Keep tests deterministic (no live network unless stubbed) and clean temporary files under `tmp/` or `tmp/output/`.
- Robot suites generate HTML logs; inspect `tmp/output/log.html` on failures and keep new suites platform-aware.

## Commit & Pull Request Guidelines
- Write short, imperative commit subjects (~72 chars), e.g., `Update permissions in create-release-tag workflow`.
- For PRs, link related issues, describe behavioral changes, and include sample commands/output when the CLI UX shifts; add screenshots only for doc-facing changes.
- Note which tests you ran (`go test`, `inv test --cover`, robot suites) and any produced artifacts.

## Security & Configuration Notes
- Do not hardcode endpoints or credentials; prefer the environment variable overrides in `README.md` (e.g., `RCC_ENDPOINT_*`) or `settings.yaml` under `ROBOCORP_HOME`.
- Treat `blobs/` and `build/` as generated outputs; source edits belong in `assets/` and Go packages, then regenerate via Invoke tasks.
