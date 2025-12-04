# Research: UI and Wizard Enhancements

**Date**: 2025-12-04
**Feature**: 002-ui-wizard-enhancements

## Research Questions

### RQ-001: How to implement progress indicators without external dependencies?

**Decision**: Use goroutine-based spinner with ANSI cursor control via existing `csif` function

**Rationale**:
- The `csif` function in `pretty/internal.go` already provides dynamic ANSI escape sequence generation
- Go's goroutines enable non-blocking animation updates
- Cursor movement sequences (`\r` for carriage return, `csi("?25l")` to hide cursor) are standard ANSI
- No external dependencies needed; builds on existing `go-isatty` for terminal detection

**Alternatives Considered**:
- Third-party libraries (e.g., `github.com/briandowns/spinner`): Rejected per out-of-scope constraint
- Simple status messages without animation: Rejected as doesn't meet "visual feedback within 500ms" requirement
- Polling-based updates: Less efficient than goroutine channel approach

**Implementation Pattern**:
```go
// Spinner using csif for cursor control
type Spinner struct {
    frames   []string
    message  string
    stop     chan struct{}
    done     chan struct{}
}

func (s *Spinner) Start() {
    if !pretty.Interactive { return }
    go s.animate()
}

func (s *Spinner) animate() {
    hideCursor := csif("?25l")  // Hide cursor
    showCursor := csif("?25h")  // Show cursor
    // Animation loop with \r for line overwrite
}
```

---

### RQ-002: How to implement confirmation prompts consistently across commands?

**Decision**: Create reusable `Confirm()` function in `wizard` package with `--yes` flag pattern

**Rationale**:
- The existing `ask()` function in `wizard/common.go` provides the input loop pattern
- `memberValidation` can validate y/n responses
- Cobra supports `PersistentFlags()` for global `--yes` flag or per-command flags
- Non-interactive detection via `pretty.Interactive` enables fail-safe behavior

**Alternatives Considered**:
- Per-command inline confirmation: Rejected as leads to inconsistent UX and code duplication
- Global `--force` flag: Rejected as `--yes` is more semantically clear for confirmations

**Implementation Pattern**:
```go
// In wizard/confirm.go
func Confirm(question string, force bool) (bool, error) {
    if force {
        return true, nil
    }
    if !pretty.Interactive {
        return false, errors.New("confirmation required: use --yes flag")
    }
    validator := memberValidation([]string{"y", "Y", "n", "N"}, "Please enter y or n")
    response, err := ask(question+" [y/N]", "n", validator)
    return response == "y" || response == "Y", err
}
```

---

### RQ-003: How are destructive commands currently structured?

**Decision**: Add confirmation calls at command execution entry points

**Rationale**:
- Holotree commands in `cmd/holotree.go` use `RunE` functions with early validation
- Configuration cleanup in `cmd/configuration.go` follows same pattern
- Confirmation should occur before any file operations begin
- The `--yes` flag should be defined at the parent command level where possible

**Relevant Commands Identified**:
| Command | File | Current Protection |
|---------|------|-------------------|
| `holotree delete` | `cmd/holotree.go` | None |
| `holotree remove` | `cmd/holotree.go` | None |
| `configuration cleanup` | `cmd/configuration.go` | None |
| `holotree shared --prune` | `cmd/holotree.go` | None |

---

### RQ-004: How to detect terminal width for progress bar sizing?

**Decision**: Use `golang.org/x/term` (already in go.mod) for terminal size detection with 80-char fallback

**Rationale**:
- `golang.org/x/term` is already a dependency (via `golang.org/x/term v0.13.0`)
- Provides `GetSize()` for width detection
- Fallback to 80 characters matches spec assumption
- Width caching avoids repeated syscalls during animation

**Implementation Pattern**:
```go
func getTerminalWidth() int {
    width, _, err := term.GetSize(int(os.Stdout.Fd()))
    if err != nil || width < 40 {
        return 80 // Default fallback
    }
    return width
}
```

---

### RQ-005: How to handle Ctrl+C during progress operations?

**Decision**: Use signal handler to clean up terminal state before exit

**Rationale**:
- Go's `os/signal` package provides `Notify()` for signal handling
- Must restore cursor visibility and reset terminal formatting
- Deferred cleanup ensures state restoration even on panic

**Implementation Pattern**:
```go
func (s *Spinner) Start() {
    s.setupSignalHandler()
    defer s.cleanup()
    // ...
}

func (s *Spinner) setupSignalHandler() {
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-sigChan
        s.cleanup()
        os.Exit(130) // Standard exit code for SIGINT
    }()
}
```

---

### RQ-006: How to integrate progress indicators with existing logging?

**Decision**: Progress indicators operate on stderr; logging remains on stdout via `common.Trace()`

**Rationale**:
- FR-015 requires trace-level logging for start/complete/error events
- `common.Trace()` already exists and respects verbosity settings
- Progress animation on stderr prevents interference with piped stdout
- Clear separation enables `rcc command | other-tool` pipelines

**Trace Integration**:
```go
func (s *Spinner) Start() {
    common.Trace("Progress started: %s", s.message)
}

func (s *Spinner) Stop(success bool) {
    if success {
        common.Trace("Progress completed: %s", s.message)
    } else {
        common.Trace("Progress failed: %s", s.message)
    }
}
```

---

## Technology Decisions Summary

| Decision | Choice | Dependency Impact |
|----------|--------|-------------------|
| Progress animation | Goroutine + ANSI via `csif` | None (existing) |
| Terminal detection | `go-isatty` + `pretty.Interactive` | None (existing) |
| Terminal width | `golang.org/x/term.GetSize()` | None (existing) |
| Input validation | `memberValidation` pattern | None (existing) |
| Signal handling | `os/signal` + `syscall` | None (stdlib) |
| Confirmation prompts | `wizard.Confirm()` with `--yes` flag | None (existing) |

**All NEEDS CLARIFICATION items resolved. No new dependencies required.**
