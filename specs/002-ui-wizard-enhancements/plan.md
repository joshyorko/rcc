# Implementation Plan: UI and Wizard Enhancements

**Branch**: `002-ui-wizard-enhancements` | **Date**: 2025-12-04 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/002-ui-wizard-enhancements/spec.md`

## Summary

Enhance RCC's terminal UI and interactive wizard by leveraging currently unused utility functions (`csif` in `pretty/internal.go` and `memberValidation` in `wizard/common.go`).

**Phase 1 (COMPLETE)**: Basic progress indicators (spinners/bars), wizard input validation, confirmation prompts, and color/formatting conventions.

**Phase 2 (NEW)**: Rich dashboard displays with box-drawing characters, scroll regions, multi-step progress tracking, cursor control functions, advanced color support (256-color, TrueColor), and enhanced wizard action selection.

All enhancements must use only existing dependencies (no new external libraries).

## Technical Context

**Language/Version**: Go 1.20
**Primary Dependencies**: Cobra (CLI), go-isatty (terminal detection), existing `pretty` and `wizard` packages
**Storage**: N/A (no data persistence changes)
**Testing**: Go `testing` package (unit), Robot Framework (acceptance)
**Target Platform**: Linux, Windows, macOS (cross-platform CLI)
**Project Type**: Single CLI binary
**Performance Goals**: Progress indicators must appear within 500ms of operation start; no noticeable latency impact
**Constraints**: No new Go module dependencies; must use existing `csif` and `memberValidation` functions
**Scale/Scope**: Affects ~10-15 commands for progress feedback, ~5 wizard prompts for validation, ~4 commands for confirmation prompts

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Environment Isolation & Reproducibility | PASS | No impact on environment management |
| II. Cross-Platform Compatibility | PASS | Terminal handling via go-isatty already cross-platform |
| III. Build Patience & Stability | PASS | No build process changes |
| IV. Embedded Assets | PASS | No asset changes required |
| V. CLI-First | PASS | Enhances CLI user experience |
| VI. Privacy & Telemetry | PASS | No telemetry; trace logging only at user request |

**All gates pass. No violations to justify.**

## Project Structure

### Documentation (this feature)

```text
specs/002-ui-wizard-enhancements/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (internal Go interfaces)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
# Existing RCC structure - modifications only
pretty/
├── internal.go          # Contains csif (to be utilized)
├── variables.go         # Color definitions (to be extended)
├── progress.go          # DONE: Progress indicator implementation (spinners, bars)
├── cursor.go            # NEW: Cursor control functions (save, restore, move, scroll regions)
├── colors.go            # NEW: Advanced color support (256-color, TrueColor, severity/status)
├── box.go               # NEW: Box drawing characters and functions
└── dashboard.go         # NEW: Dashboard framework and layouts A-F

wizard/
├── common.go            # Contains memberValidation (to be utilized more)
├── create.go            # Existing wizard (to be enhanced)
├── confirm.go           # DONE: Confirmation prompt implementation
├── actions.go           # NEW: ChooseAction, AskRecovery functions
└── templates.go         # NEW: chooseByName template selection

cmd/
├── holotree.go          # DONE: Confirmation prompts for delete/remove
├── holotreeVariables.go # DONE: Spinner integration for environment creation
├── configuration.go     # DONE: Confirmation prompts for cleanup
├── run.go               # MODIFY: Add Robot Run Dashboard (Layout F)
└── [other commands]     # Add dashboard displays where appropriate

htfs/
└── commands.go          # DONE: Spinner integration for NewEnvironment/RecordEnvironment

cloud/
└── client.go            # DONE: Progress bar integration for Download
```

**Structure Decision**: Modifications to existing Go packages following RCC's established patterns. New files added within existing package directories. No new top-level directories needed.

## Complexity Tracking

> No violations to justify - all gates pass.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| N/A | N/A | N/A |

---

## Phase 2 Technical Design

### Cursor Control Module (`pretty/cursor.go`)

Provides low-level cursor manipulation using CSI escape sequences.

```go
// Core cursor functions
func SaveCursor()                        // CSI s or ESC 7
func RestoreCursor()                     // CSI u or ESC 8
func MoveTo(row, col int)                // CSI {row};{col}H
func MoveUp(n int)                       // CSI {n}A
func MoveDown(n int)                     // CSI {n}B
func MoveRight(n int)                    // CSI {n}C
func MoveLeft(n int)                     // CSI {n}D

// Scroll region management
func SetScrollRegion(top, bottom int)    // CSI {top};{bottom}r (DECSTBM)
func ClearScrollRegion()                 // CSI r (reset to full screen)
func ScrollUp(n int)                     // CSI {n}S
func ScrollDown(n int)                   // CSI {n}T

// Line manipulation
func ClearLine()                         // CSI 2K
func ClearToEnd()                        // CSI 0K
func ClearToStart()                      // CSI 1K
func ClearScreen()                       // CSI 2J

// Cursor visibility
func HideCursor()                        // CSI ?25l
func ShowCursor()                        // CSI ?25h
```

### Advanced Colors Module (`pretty/colors.go`)

Extends existing color support with 256-color palette and TrueColor.

```go
// Color mode detection
type ColorMode int
const (
    ColorModeNone ColorMode = iota       // NO_COLOR set or dumb terminal
    ColorModeBasic                       // 16 basic colors
    ColorMode256                         // 256-color palette
    ColorModeTrueColor                   // 24-bit RGB
)

func DetectColorMode() ColorMode         // Check COLORTERM, TERM, NO_COLOR

// Semantic color functions
func SeverityColor(level string) string  // trace→dim, debug→gray, info→white, warning→yellow, error→red, critical→bright red
func StatusColor(status string) string   // pending→gray, running→cyan, complete→green, failed→red, skipped→dim

// Extended color functions
func Color256(n int) string              // CSI 38;5;{n}m
func BGColor256(n int) string            // CSI 48;5;{n}m
func RGB(r, g, b int) string             // CSI 38;2;{r};{g};{b}m
func BGRGB(r, g, b int) string           // CSI 48;2;{r};{g};{b}m
```

### Box Drawing Module (`pretty/box.go`)

Provides box-drawing characters and rendering functions.

```go
// Box styles
type BoxStyle struct {
    TopLeft, TopRight, BottomLeft, BottomRight string
    Horizontal, Vertical string
    LeftT, RightT, TopT, BottomT, Cross string
}

var (
    BoxSingle  BoxStyle  // ┌─┐│└─┘├┤┬┴┼
    BoxDouble  BoxStyle  // ╔═╗║╚═╝╠╣╦╩╬
    BoxRounded BoxStyle  // ╭─╮│╰─╯├┤┬┴┼
    BoxASCII   BoxStyle  // +-+|+-++++  (fallback)
)

func ActiveBoxStyle() BoxStyle           // Returns Unicode or ASCII based on terminal support

// Drawing functions
func DrawBox(x, y, width, height int, style BoxStyle)
func DrawHLine(x, y, width int, char string)
func DrawVLine(x, y, height int, char string)
func DrawBoxWithTitle(x, y, width, height int, title string, style BoxStyle)
```

### Dashboard Framework (`pretty/dashboard.go`)

Core dashboard system supporting multiple layout types.

```go
// Dashboard interface
type Dashboard interface {
    Start()
    Stop(success bool)
    Update(state DashboardState)
    SetStep(index int, status StepStatus, message string)
    AddOutput(line string)
}

// Step status
type StepStatus int
const (
    StepPending StepStatus = iota
    StepRunning
    StepComplete
    StepFailed
    StepSkipped
)

// Dashboard detection
func ShouldUseDashboard() bool           // Check terminal dimensions, Interactive flag

// Factory functions for each layout
func NewEnvironmentDashboard(steps []string) Dashboard    // Layout A: 15-step env build
func NewDiagnosticsDashboard(checks []string) Dashboard   // Layout B: Checklist style
func NewDownloadDashboard(filename string, total int64) Dashboard  // Layout C: Single download
func NewMultiTaskDashboard(tasks []string) Dashboard      // Layout D: Parallel operations
func NewCompactProgress(message string) Dashboard         // Layout E: Inline fallback
func NewRobotRunDashboard(robotName string) Dashboard     // Layout F: Robot execution
```

### Dashboard Layout A: Environment Build

```
┌─────────────────────────────────────────────────────────────────┐
│ RCC Environment Build                                    3/15   │
├─────────────────────────────────────────────────────────────────┤
│ ✓ 1. Context verification                                       │
│ ✓ 2. Holotree lock acquired                                     │
│ ⠋ 3. Composing blueprint...                                     │
│   4. Validate blueprint                                         │
│   5. Check remote catalog                                       │
│   6. Prepare holotree stage                                     │
│   ...                                                           │
├─────────────────────────────────────────────────────────────────┤
│ [████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 20%  ETA: 2m30s       │
└─────────────────────────────────────────────────────────────────┘
```

### Dashboard Layout F: Robot Run

```
╭─────────────────────────────────────────────────────────────────╮
│ 🤖 Robot: my-automation | Task: Main Task                       │
│ Status: Running | Duration: 1m23s                               │
├─────────────────────────────────────────────────────────────────┤
│ [Scrolling output region - last 10 lines]                       │
│ > Processing item 42 of 100...                                  │
│ > API response: 200 OK                                          │
│ > Writing to database...                                        │
├─────────────────────────────────────────────────────────────────┤
│ Tasks: 3/5 | Pass: 2 | Fail: 1 | Skip: 0                        │
╰─────────────────────────────────────────────────────────────────╯
```

### Wizard Enhancements (`wizard/actions.go`)

```go
// Action represents a selectable option
type Action struct {
    Key         string           // Unique identifier
    Name        string           // Display name
    Description string           // Help text
    Handler     func() error     // Optional callback
}

// ChooseAction presents numbered options and returns selection
func ChooseAction(prompt string, actions []Action) (*Action, error)

// AskRecovery presents recovery options for an error
func AskRecovery(err error, options []Action) (*Action, error)

// ConfirmDangerous requires typing confirmation text for critical operations
func ConfirmDangerous(prompt string, confirmText string, force bool) (bool, error)
```

### Integration Points

1. **Environment Build** (`htfs/commands.go`):
   - Replace spinner with EnvironmentDashboard in NewEnvironment()
   - Call dashboard.SetStep() at each of the 15 stages

2. **Robot Run** (`cmd/run.go`):
   - Wrap robot execution with RobotRunDashboard
   - Pipe stdout/stderr to dashboard.AddOutput()

3. **Downloads** (`cloud/client.go`):
   - For single large files, use DownloadDashboard
   - For multiple parallel downloads, use MultiTaskDashboard

4. **Diagnostics** (`operations/diagnostics.go`):
   - Wrap diagnostic checks with DiagnosticsDashboard
