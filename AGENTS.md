# AGENTS.md

RCC (Repeatable, Contained Code) is a Go CLI that creates isolated, reproducible Python environments for automation packages. It embeds micromamba binaries so target machines don't need Python installed.

## Quick Context

- **Language:** Go 1.23 with Cobra/Viper CLI framework
- **Build System:** Python Invoke (`tasks.py`) orchestrates Go builds and asset generation
- **Testing:** Go unit tests (`_test.go`) + Robot Framework acceptance tests (`robot_tests/`)
- **Key Innovation:** Holotree—content-addressed environment caching with delta transfers

## Capabilities & Limits

### You Can
- Modify Go source code in `cmd/`, `operations/`, `common/`, `conda/`, and other packages
- Add or update CLI commands (each command is a separate file in `cmd/`)
- Run build and test commands via `inv` (Invoke) or direct Go commands
- Update embedded templates by editing `assets/` and regenerating with `inv assets`

### Escalate When
- Changes affect Holotree caching (`htfs/`, `hamlet/`) — dragons here
- Modifying micromamba integration or conda environment creation
- Security-sensitive changes (auth, TLS, credential handling)
- Breaking changes to CLI command interfaces

### Never
- Edit `blobs/` or `build/` directly—these are generated artifacts
- Hardcode endpoints, credentials, or API keys—use `RCC_ENDPOINT_*` environment variables
- Add telemetry, tracking, or background metrics—this fork has telemetry disabled
- Make live network calls in tests without stubbing
- Commit secrets or credentials

## Development Workflow

### Prerequisites
Assets must be generated before building. If you see `pattern assets/*.py: no matching files`:
```bash
inv assets    # Zips templates, copies configs, prepares embedded micromamba → blobs/
```

### Build Commands
```bash
inv local                                  # Build for current platform
inv build                                  # Cross-platform build (linux64, macos64, macosarm64, windows64)
GOARCH=amd64 go build -o build/ ./cmd/...  # Direct Go build
```

### Test Commands
```bash
# Unit tests (GOARCH=amd64 is required)
GOARCH=amd64 go test ./...
inv test                                   # Via Invoke
inv test --cover                           # With coverage → tmp/cover.out

# Acceptance tests (Robot Framework)
inv robot                                  # Full suite → tmp/output/log.html

# Single test file
python3 -m robot -L DEBUG -d tmp/output robot_tests/holotree.robot
```

## Architecture

```
rcc/
├── cmd/                    # CLI commands (Cobra). Entry: cmd/rcc/main.go
│   ├── *.go                # One file per command (run.go, pull.go, configure.go)
│   └── command_*.go        # Platform-specific (darwin, linux, windows)
├── operations/             # Core business logic (auth, bundling, diagnostics, running)
├── common/                 # Shared utilities (logging, caching, version, platform detection)
├── conda/                  # Conda/micromamba environment management
├── htfs/, hamlet/          # Holotree virtual filesystem and directory utilities 🐉
├── pathlib/, shell/        # Cross-platform path and shell execution
├── settings/, xviper/      # Configuration management (Viper-based)
├── fail/                   # Error handling package (use instead of if err != nil)
├── assets/                 # Source files for embedded assets (edit these)
├── blobs/                  # Generated embedded assets (DO NOT edit)
├── build/                  # Build outputs (DO NOT edit)
├── robot_tests/            # Robot Framework acceptance tests
└── developer/              # Dev environment bootstrap (toolkit.yaml, setup.yaml)
```

### Task → Location Mapping

| Task | Look In |
|------|---------|
| Add/modify CLI command | `cmd/` (each command is a file) |
| Platform-specific CLI logic | `cmd/command_darwin.go`, `command_linux.go`, `command_windows.go` |
| Change core behavior | `operations/` (auth, bundling, diagnostics, running, zipping) |
| Modify environment creation | `conda/` |
| Update Holotree caching | `htfs/`, `hamlet/` — 🐉 dragons here |
| Add shared utility | `common/`, `pathlib/`, `shell/` |
| Update embedded templates | `assets/` then run `inv assets` |
| Add acceptance test | `robot_tests/*.robot` |

## RCC Code Patterns

### Error Handling — Use `fail` Package

**Do NOT use `if err != nil` everywhere.** Use the `fail` package:

```go
func SomeOperation() (err error) {
    defer fail.Around(&err)                    // Recover at function boundary

    fail.On(err != nil, "context: %v", err)    // Panic with wrapped error
    fail.Fast(err)                             // Panic if err != nil
    return nil
}
```

### Logging — Use `common` Package

**Do NOT use `fmt.Print`.** Use the `common` package:

```go
common.Log("Normal: %s", msg)      // Always shown (unless silent)
common.Debug("Debug: %s", msg)     // Only with --debug flag
common.Trace("Trace: %s", msg)     // Only with --trace flag
common.Timeline("op start %s", k)  // Performance timeline
```

### Testing — Use `hamlet` Package

```go
func TestSomething(t *testing.T) {
    must_be, wont_be := hamlet.Specifications(t)

    must_be.Nil(err)
    must_be.Equal("expected", actual)
    must_be.True(condition)
    wont_be.True(badCondition)
}
```

### Table-Driven Tests

```go
func TestOperation(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"normal case", "input", "output"},
        {"edge case", "", ""},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Operation(tt.input)
            must_be, _ := hamlet.Specifications(t)
            must_be.Equal(tt.expected, result)
        })
    }
}
```

## Code Style

```go
// Package names: lowercase, no underscores
package operations

// Exported names: PascalCase
func SummonCache() {}

// Local variables: mixedCaps
func example() {
    myVar := 42
}

// CLI commands: verb-first
// Good: run, pull, configure, push
// Bad: runner, puller, configuration
```

- Run `gofmt` before committing
- Prefer small, composable functions
- Platform-specific code in `command_darwin.go`, `command_linux.go`, `command_windows.go`
- Tests colocated in `_test.go` files

## Critical Rules

1. **Generated files are read-only.** Never edit `blobs/` or `build/`. Edit sources in `assets/` and Go packages, then regenerate with `inv assets`.

2. **Configuration over hardcoding.** Use environment variables for endpoints:
   ```
   RCC_ENDPOINT_CLOUD_API    RCC_ENDPOINT_PYPI       RCC_ENDPOINT_CONDA
   RCC_ENDPOINT_CLOUD_UI     RCC_ENDPOINT_DOWNLOADS  RCC_ENDPOINT_DOCS
   ```

3. **No telemetry.** This fork disables all tracking. Do not add metrics, analytics, or background reporting.

4. **Platform isolation.** Keep OS-specific logic in `command_darwin.go`, `command_linux.go`, `command_windows.go`. Do not leak platform conditionals elsewhere.

5. **Test determinism.** No live network calls in tests. Stub external dependencies. Write temp files under `tmp/`.

6. **GOARCH requirement.** Always set `GOARCH=amd64` when running `go test` directly.

## Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `pattern assets/*.py: no matching files` | Assets not generated | Run `inv assets` |
| Tests fail with GOARCH mismatch | Missing env var | Set `GOARCH=amd64` |
| Micromamba download fails | Network restriction | Set `RCC_MICROMAMBA_BASE` to override URL |
| Build fails after template change | Stale blobs | Run `inv assets` then rebuild |

## PR Checklist

- [ ] `gofmt` applied to all changed files
- [ ] `GOARCH=amd64 go test ./...` passes
- [ ] `inv robot` passes (or relevant subset for the change)
- [ ] Uses `fail` package for error handling (not raw `if err != nil`)
- [ ] Uses `common.Log/Debug/Trace` for output (not `fmt.Print`)
- [ ] No hardcoded endpoints or credentials
- [ ] No telemetry or tracking code added
- [ ] Commit message: imperative mood, ~72 chars
- [ ] If CLI UX changed: include sample command output in PR description
