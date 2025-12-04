# UI Enhancement Specifications

This document outlines potential enhancements to RCC's terminal UI and interactive wizard using currently unused utility functions.

## Overview

Two utility functions exist in the codebase that are well-designed but currently unused:

| Function | Location | Purpose |
|----------|----------|---------|
| `csif` | `pretty/internal.go:9` | Dynamic CSI escape sequence formatting |
| `memberValidation` | `wizard/common.go:27` | Validator factory for enum-like choices |

---

## 1. Dynamic Terminal UI with `csif`

### Background

CSI (Control Sequence Introducer) escape codes control terminal behavior - cursor position, colors, clearing screen, etc. The format is `ESC[<parameters>` where parameters can be numbers.

**Current usage** - all static/hardcoded:
```go
Red = csi("91m")      // Static: always red
Home = csi("1;1H")    // Static: always row 1, col 1
```

**What `csif` enables** - dynamic values:
```go
// Move cursor to ANY position (row, column)
csif("%d;%dH", row, col)

// 256-color palette (0-255)
csif("38;5;%dm", 208)  // Orange foreground

// True RGB colors
csif("38;2;%d;%d;%dm", 255, 128, 0)  // RGB orange

// Scroll region (lines top to bottom)
csif("%d;%dr", 5, 20)  // Scroll only lines 5-20
```

### Feature 1.1: Progress Bar

Display a dynamic progress bar during long operations (environment creation, downloads).

**File:** `pretty/progress.go` (new)

```go
package pretty

import (
    "fmt"
    "strings"
)

// ProgressBar displays a dynamic progress bar
// Returns a string that moves cursor to column 1 and prints the bar
func ProgressBar(current, total int, width int) string {
    if total <= 0 {
        total = 1
    }
    percent := float64(current) / float64(total)
    filled := int(percent * float64(width))
    if filled > width {
        filled = width
    }
    
    bar := fmt.Sprintf("[%s%s] %3.0f%%",
        strings.Repeat("█", filled),
        strings.Repeat("░", width-filled),
        percent*100)
    
    // CSI <n>G = move cursor to column n
    return csif("%dG", 1) + bar
}

// ProgressLine prints a progress bar that overwrites itself
func ProgressLine(current, total int, label string) string {
    bar := ProgressBar(current, total, 30)
    return fmt.Sprintf("%s %s", bar, label)
}
```

**Usage:**
```go
for i := 0; i <= 100; i++ {
    fmt.Print(pretty.ProgressBar(i, 100, 30))
    time.Sleep(50 * time.Millisecond)
}
fmt.Println() // Move to next line when done

// Output (updates in place):
// [███████████████░░░░░░░░░░░░░░░]  50%
```

### Feature 1.2: Severity-Based Coloring

Dynamic coloring based on status/severity using 256-color palette.

**File:** `pretty/severity.go` (new)

```go
package pretty

// SeverityColor returns an ANSI color code based on severity level (0-100)
// 0-33: Green, 34-66: Yellow, 67-100: Red
func SeverityColor(level int) string {
    switch {
    case level < 33:
        return csif("38;5;%dm", 82)   // Green
    case level < 66:
        return csif("38;5;%dm", 226)  // Yellow
    default:
        return csif("38;5;%dm", 196)  // Red
    }
}

// StatusColor returns color based on status string
func StatusColor(status string) string {
    switch status {
    case "ok", "success", "pass", "ready":
        return csif("38;5;%dm", 82)   // Green
    case "warning", "pending", "building":
        return csif("38;5;%dm", 226)  // Yellow
    case "error", "fail", "failed":
        return csif("38;5;%dm", 196)  // Red
    default:
        return csif("38;5;%dm", 245)  // Grey
    }
}
```

**Usage:**
```go
fmt.Printf("%s[%s]%s Environment status\n", 
    pretty.StatusColor("ok"), "OK", pretty.Reset)
```

### Feature 1.3: Status Dashboard

Real-time status updates without disrupting scrolling output. Different layouts serve different use cases.

**File:** `pretty/dashboard.go` (new)

```go
package pretty

import (
    "fmt"
    "strings"
    "time"
)

// === Core Cursor Control Functions ===

// SaveCursor saves the current cursor position
func SaveCursor() string {
    return csi("s")
}

// RestoreCursor restores the previously saved cursor position
func RestoreCursor() string {
    return csi("u")
}

// MoveTo moves cursor to specific row and column (1-indexed)
func MoveTo(row, col int) string {
    return csif("%d;%dH", row, col)
}

// MoveToColumn moves cursor to specific column on current line
func MoveToColumn(col int) string {
    return csif("%dG", col)
}

// ClearLine clears the current line
func ClearLine() string {
    return csi("2K")
}

// ClearToEnd clears from cursor to end of line
func ClearToEnd() string {
    return csi("0K")
}

// SetScrollRegion sets the scrollable region (rows are 1-indexed)
func SetScrollRegion(top, bottom int) string {
    return csif("%d;%dr", top, bottom)
}

// ResetScrollRegion resets scroll region to full screen
func ResetScrollRegion() string {
    return csi("r")
}

// HideCursor hides the cursor
func HideCursor() string {
    return csi("?25l")
}

// ShowCursor shows the cursor
func ShowCursor() string {
    return csi("?25h")
}

// UpdateStatusLine updates a specific line without disrupting output
func UpdateStatusLine(row int, message string) {
    fmt.Print(SaveCursor())
    fmt.Print(MoveTo(row, 1))
    fmt.Print(ClearLine())
    fmt.Print(message)
    fmt.Print(RestoreCursor())
}
```

---

## Dashboard Layout Options

RCC has several distinct operation types that benefit from different dashboard layouts:

### Layout A: Environment Build Dashboard (15-step process)

**Use case:** `rcc holotree variables`, `rcc run` (first time), environment creation

The current progress system shows 15 steps (0-15). A dashboard could show:

```
┌─────────────────────────────────────────────────────────────────────┐
│  RCC Environment Build                                    v18.0.0  │
├─────────────────────────────────────────────────────────────────────┤
│  Blueprint: abc123def456                                           │
│  [████████████░░░░░░░░░░░░░░░░░░] 40% (Step 6/15)                  │
│                                                                     │
│  ✓ Context initialized                           0.12s             │
│  ✓ Holotree mode: shared                         0.01s             │
│  ✓ Blueprint computed                            0.34s             │
│  ✓ Remote origin checked                         1.20s             │
│  ✓ Stage cleanup                                 0.05s             │
│  ● Building environment...                       12.5s             │
│  ○ Restore partial environment                                     │
│  ○ Micromamba phase                                                │
│  ○ Pip/UV install phase                                            │
│  ○ Post-install scripts                                            │
│  ○ Record to hololib                                               │
│  ○ Restore space                                                   │
│  ○ Finalize                                                        │
├─────────────────────────────────────────────────────────────────────┤
│  Elapsed: 14.22s │ Est. remaining: ~2m 30s │ Workers: 8/8 CPUs    │
└─────────────────────────────────────────────────────────────────────┘

[scrolling log output below...]
```

**Implementation:**

```go
package pretty

// BuildStep represents a single build step
type BuildStep struct {
    Number      int
    Name        string
    Status      string  // "pending", "running", "done", "failed"
    Duration    time.Duration
}

// BuildDashboard manages the environment build display
type BuildDashboard struct {
    Blueprint    string
    Steps        []BuildStep
    CurrentStep  int
    StartTime    time.Time
    HeaderRows   int  // rows reserved for header
}

// NewBuildDashboard creates a dashboard for environment builds
func NewBuildDashboard(blueprint string) *BuildDashboard {
    return &BuildDashboard{
        Blueprint: blueprint,
        Steps: []BuildStep{
            {0, "Context initialization", "pending", 0},
            {1, "Holotree mode setup", "pending", 0},
            {2, "Blueprint computation", "pending", 0},
            {3, "Remote origin check", "pending", 0},
            {4, "Stage cleanup", "pending", 0},
            {5, "Environment build", "pending", 0},
            {6, "Partial restore", "pending", 0},
            {7, "Micromamba phase", "pending", 0},
            {8, "Pip/UV install", "pending", 0},
            {9, "Post-install scripts", "pending", 0},
            {10, "Layer finalization", "pending", 0},
            {11, "Reserved", "pending", 0},
            {12, "Reserved", "pending", 0},
            {13, "Record to hololib", "pending", 0},
            {14, "Restore space", "pending", 0},
            {15, "Finalize", "pending", 0},
        },
        StartTime:  time.Now(),
        HeaderRows: 20,
    }
}

func (d *BuildDashboard) Start() {
    if !Interactive {
        return
    }
    // Reserve top area, set scroll region below
    fmt.Print(HideCursor())
    fmt.Print(SetScrollRegion(d.HeaderRows+1, 50)) // Assume 50-row terminal
    fmt.Print(MoveTo(d.HeaderRows+1, 1))
    d.Render()
}

func (d *BuildDashboard) Render() {
    if !Interactive {
        return
    }
    fmt.Print(SaveCursor())
    fmt.Print(MoveTo(1, 1))
    
    // Header
    fmt.Printf("%s┌─────────────────────────────────────────────────────────────────────┐%s\n", Cyan, Reset)
    fmt.Printf("%s│  RCC Environment Build %44s  │%s\n", Cyan, common.Version, Reset)
    fmt.Printf("%s├─────────────────────────────────────────────────────────────────────┤%s\n", Cyan, Reset)
    fmt.Printf("%s│  Blueprint: %-55s │%s\n", Grey, d.Blueprint[:12]+"...", Reset)
    
    // Progress bar
    progress := float64(d.CurrentStep) / float64(len(d.Steps))
    barWidth := 40
    filled := int(progress * float64(barWidth))
    bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
    fmt.Printf("%s│  [%s] %3.0f%% (Step %d/%d) %20s│%s\n", 
        Cyan, bar, progress*100, d.CurrentStep, len(d.Steps), "", Reset)
    
    // Steps (show last 8)
    start := 0
    if d.CurrentStep > 5 {
        start = d.CurrentStep - 5
    }
    for i := start; i < min(start+8, len(d.Steps)); i++ {
        step := d.Steps[i]
        icon := "○"
        color := Grey
        switch step.Status {
        case "done":
            icon = "✓"
            color = Green
        case "running":
            icon = "●"
            color = Yellow
        case "failed":
            icon = "✗"
            color = Red
        }
        dur := ""
        if step.Duration > 0 {
            dur = fmt.Sprintf("%6.2fs", step.Duration.Seconds())
        }
        fmt.Printf("%s│  %s %-50s %7s │%s\n", Cyan, color+icon+Reset, step.Name, dur, Reset)
    }
    
    // Footer
    elapsed := time.Since(d.StartTime)
    fmt.Printf("%s├─────────────────────────────────────────────────────────────────────┤%s\n", Cyan, Reset)
    fmt.Printf("%s│  Elapsed: %6.1fs │ Workers: %d CPUs %26s│%s\n", 
        Grey, elapsed.Seconds(), runtime.NumCPU(), "", Reset)
    fmt.Printf("%s└─────────────────────────────────────────────────────────────────────┘%s\n", Cyan, Reset)
    
    fmt.Print(RestoreCursor())
}

func (d *BuildDashboard) UpdateStep(step int, status string) {
    if step >= 0 && step < len(d.Steps) {
        if d.Steps[step].Status == "running" && status == "done" {
            d.Steps[step].Duration = time.Since(d.StartTime) // Simplified
        }
        d.Steps[step].Status = status
        d.CurrentStep = step
    }
    d.Render()
}

func (d *BuildDashboard) Stop() {
    if !Interactive {
        return
    }
    fmt.Print(ResetScrollRegion())
    fmt.Print(ShowCursor())
}
```

---

### Layout B: Diagnostics Dashboard (checklist style)

**Use case:** `rcc configure diagnostics`, network tests, system checks

```
┌─────────────────────────────────────────────────────────────────────┐
│  RCC Diagnostics                                          v18.0.0  │
├─────────────────────────────────────────────────────────────────────┤
│  System Checks                                                      │
│    ✓ Operating system         Linux 6.1.0                          │
│    ✓ User permissions         kdlocpanda (uid:1000)                │
│    ✓ Temp directory           /tmp (writable)                      │
│    ✓ Home directory           /home/kdlocpanda                     │
│                                                                     │
│  Network Checks                                                     │
│    ✓ DNS resolution           OK (8.8.8.8)                         │
│    ● HTTPS connectivity       Testing api.robocorp.com...          │
│    ○ PyPI access              Pending                              │
│    ○ Conda access             Pending                              │
│                                                                     │
│  Environment Checks                                                 │
│    ○ Micromamba version       Pending                              │
│    ○ Holotree status          Pending                              │
│    ○ Disk space               Pending                              │
├─────────────────────────────────────────────────────────────────────┤
│  Progress: 5/12 checks │ Passed: 5 │ Failed: 0 │ Warnings: 0       │
└─────────────────────────────────────────────────────────────────────┘
```

**Implementation:**

```go
// DiagnosticsDashboard for system checks
type DiagnosticsDashboard struct {
    Categories []DiagnosticCategory
    StartTime  time.Time
}

type DiagnosticCategory struct {
    Name   string
    Checks []DiagnosticCheck
}

type DiagnosticCheck struct {
    Name    string
    Status  string  // "pending", "running", "pass", "warn", "fail"
    Detail  string
}
```

---

### Layout C: Download Progress (single operation)

**Use case:** `rcc pull`, downloading catalogs, fetching remote environments

```
Downloading catalog abc123def456...

  Source: https://downloads.robocorp.com/catalogs/abc123def456.zip
  
  [████████████████████░░░░░░░░░░░░░░░░░░░░] 52%
  
  Downloaded: 156.2 MB / 300.0 MB
  Speed: 12.4 MB/s
  ETA: 11s
```

**Implementation:**

```go
// DownloadProgress for file downloads
type DownloadProgress struct {
    URL         string
    TotalBytes  int64
    Downloaded  int64
    StartTime   time.Time
}

func (p *DownloadProgress) Render() string {
    if !Interactive {
        return ""
    }
    percent := float64(p.Downloaded) / float64(p.TotalBytes)
    elapsed := time.Since(p.StartTime).Seconds()
    speed := float64(p.Downloaded) / elapsed / 1024 / 1024 // MB/s
    remaining := float64(p.TotalBytes-p.Downloaded) / (float64(p.Downloaded) / elapsed)
    
    bar := ProgressBar(int(p.Downloaded), int(p.TotalBytes), 40)
    
    return fmt.Sprintf(
        "%s\n\n  Source: %s\n\n  %s\n\n  Downloaded: %.1f MB / %.1f MB\n  Speed: %.1f MB/s\n  ETA: %.0fs\n",
        ClearLine(),
        p.URL,
        bar,
        float64(p.Downloaded)/1024/1024,
        float64(p.TotalBytes)/1024/1024,
        speed,
        remaining,
    )
}
```

---

### Layout D: Multi-Task Progress (batch operations)

**Use case:** `rcc holotree prebuild` (building multiple environments), batch imports

```
┌─────────────────────────────────────────────────────────────────────┐
│  Prebuild Environments (3/10)                             v18.0.0  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Overall: [██████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░] 30%           │
│                                                                     │
│  1. python310-base        ✓ Complete              2m 15s           │
│  2. python311-selenium    ✓ Complete              3m 42s           │
│  3. python310-playwright  ● Building... (step 8/15)                │
│  4. python39-legacy       ○ Queued                                 │
│  5. python312-minimal     ○ Queued                                 │
│  ...                                                                │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│  Completed: 2 │ In Progress: 1 │ Queued: 7 │ Failed: 0             │
│  Elapsed: 5m 57s │ Est. total: ~20m                                │
└─────────────────────────────────────────────────────────────────────┘
```

---

### Layout E: Compact Inline Progress (non-blocking)

**Use case:** Quick operations, when full dashboard is overkill

```
● Creating environment... [████████░░░░░░░░░░░░] 40% (step 6/15) 12.3s
```

**Implementation:**

```go
// InlineProgress for simple single-line updates
func InlineProgress(label string, current, total int, elapsed time.Duration) string {
    bar := ProgressBar(current, total, 20)
    return fmt.Sprintf("%s%s● %s %s (step %d/%d) %.1fs%s",
        MoveToColumn(1),
        ClearToEnd(),
        label,
        bar,
        current,
        total,
        elapsed.Seconds(),
        Reset,
    )
}
```

---

### Layout F: Robot Run Dashboard

**Use case:** `rcc run`, `rcc task run` - showing robot execution status

```
┌─────────────────────────────────────────────────────────────────────┐
│  Robot: my-automation-bot                                 Running  │
├─────────────────────────────────────────────────────────────────────┤
│  Task: Process Invoices                                            │
│  Environment: abc123def456 (cached)                                │
│                                                                     │
│  Status: Executing step 3/5                                        │
│  [████████████████████░░░░░░░░░░░░░░░░░░░░] 60%                    │
│                                                                     │
│  Runtime: 45.2s                                                    │
│  Memory: 256 MB                                                    │
│  CPU: 23%                                                          │
├─────────────────────────────────────────────────────────────────────┤
│  Output: Processing invoice INV-2024-0042...                       │
└─────────────────────────────────────────────────────────────────────┘

[robot stdout/stderr scrolls below...]
```

---

## Layout Selection Matrix

| Operation | Recommended Layout | Fallback (no TTY) |
|-----------|-------------------|-------------------|
| `rcc holotree variables` | A (Build Dashboard) | Line-by-line progress |
| `rcc run` (first time) | A (Build Dashboard) | Line-by-line progress |
| `rcc run` (cached) | F (Robot Run) | Simple status messages |
| `rcc configure diagnostics` | B (Diagnostics) | Checklist output |
| `rcc pull` | C (Download) | Percentage updates |
| `rcc holotree prebuild` | D (Multi-Task) | Per-item status |
| `rcc holotree import` | C (Download) | Percentage updates |
| Quick operations | E (Inline) | Simple log messages |

---

## Graceful Degradation

All dashboard layouts must degrade gracefully when:

1. **Non-interactive terminal** (`!pretty.Interactive`): Fall back to `common.Log()` style output
2. **Piped output** (`stdout` not a TTY): Disable cursor movement, use line-based output
3. **CI/CD environments**: Detect `CI=true` and use simplified output
4. **Narrow terminals**: Adapt layout width or switch to compact mode

```go
// ShouldUseDashboard checks if rich dashboard is appropriate
func ShouldUseDashboard() bool {
    if !Interactive {
        return false
    }
    if os.Getenv("CI") == "true" {
        return false
    }
    if os.Getenv("RCC_NO_DASHBOARD") == "true" {
        return false
    }
    return true
}
```
```

### Feature 1.4: RGB/TrueColor Support

Support for 24-bit RGB colors (modern terminals).

**File:** Add to `pretty/colors.go` (new)

```go
package pretty

// RGB returns an ANSI escape code for 24-bit foreground color
func RGB(r, g, b int) string {
    return csif("38;2;%d;%d;%dm", r, g, b)
}

// BGColor returns an ANSI escape code for 24-bit background color
func BGRGB(r, g, b int) string {
    return csif("48;2;%d;%d;%dm", r, g, b)
}

// Color256 returns an ANSI escape code for 256-color palette
func Color256(code int) string {
    return csif("38;5;%dm", code)
}

// BGColor256 returns an ANSI escape code for 256-color background
func BGColor256(code int) string {
    return csif("48;5;%dm", code)
}
```

---

## 2. Interactive Prompts with `memberValidation`

### Background

`memberValidation` creates a validator that checks if user input matches one of a predefined list of valid options. Currently, the wizard uses `regexpValidation` for numeric selection, but `memberValidation` enables cleaner named-option validation.

### Feature 2.1: Yes/No Confirmation

Safe confirmation for destructive operations.

**File:** `wizard/confirm.go` (new)

```go
package wizard

import "strings"

// Confirm asks for yes/no confirmation
// Returns true if user confirms, false otherwise
func Confirm(question string, defaultYes bool) (bool, error) {
    defaultVal := "no"
    hint := "[y/N]"
    if defaultYes {
        defaultVal = "yes"
        hint = "[Y/n]"
    }
    
    validator := memberValidation(
        []string{"yes", "no", "y", "n", "Y", "N", "YES", "NO"},
        "Please enter 'yes' or 'no'",
    )
    
    fullQuestion := question + " " + hint
    reply, err := ask(fullQuestion, defaultVal, validator)
    if err != nil {
        return false, err
    }
    
    lower := strings.ToLower(reply)
    return lower == "yes" || lower == "y", nil
}

// MustConfirm is like Confirm but exits on error
func MustConfirm(question string, defaultYes bool) bool {
    result, err := Confirm(question, defaultYes)
    if err != nil {
        return false
    }
    return result
}
```

**Usage:**
```go
if wizard.MustConfirm("Delete all cached environments?", false) {
    // proceed with deletion
}

// Output:
// ? Delete all cached environments? [y/N]: yes
```

### Feature 2.2: Action Selection

Present named actions instead of numbers for clearer UX.

**File:** `wizard/actions.go` (new)

```go
package wizard

import (
    "fmt"
    "strings"

    "github.com/joshyorko/rcc/common"
    "github.com/joshyorko/rcc/pretty"
)

// ChooseAction presents named options and returns the selected one
func ChooseAction(question string, options []string, defaultOption string) (string, error) {
    // Display options
    common.Stdout("%sOptions:%s ", pretty.Grey, pretty.Reset)
    for i, opt := range options {
        if i > 0 {
            common.Stdout(", ")
        }
        if opt == defaultOption {
            common.Stdout("%s%s%s", pretty.Cyan, opt, pretty.Reset)
        } else {
            common.Stdout("%s", opt)
        }
    }
    common.Stdout("\n\n")
    
    // Build case-insensitive validator
    lowerOptions := make([]string, 0, len(options)*2)
    for _, opt := range options {
        lowerOptions = append(lowerOptions, opt, strings.ToLower(opt))
    }
    
    validator := memberValidation(
        lowerOptions,
        fmt.Sprintf("Please choose one of: %s", strings.Join(options, ", ")),
    )
    
    reply, err := ask(question, defaultOption, validator)
    if err != nil {
        return "", err
    }
    
    // Normalize to original case
    lower := strings.ToLower(reply)
    for _, opt := range options {
        if strings.ToLower(opt) == lower {
            return opt, nil
        }
    }
    return reply, nil
}
```

**Usage:**
```go
action, err := wizard.ChooseAction(
    "Environment already exists. What to do?",
    []string{"overwrite", "rename", "cancel"},
    "cancel",
)

// Output:
// Options: overwrite, rename, cancel (default highlighted)
//
// ? Environment already exists. What to do? [cancel]: overwrite
```

### Feature 2.3: Named Template Selection

Alternative to numeric selection for templates.

**File:** Update `wizard/create.go`

```go
// chooseByName presents options by name instead of number
func chooseByName(question, label string, candidates []string) (string, error) {
    common.Stdout("%s%s:%s\n", pretty.Grey, label, pretty.Reset)
    for _, candidate := range candidates {
        common.Stdout("  %s• %s%s%s\n", pretty.Grey, pretty.White, candidate, pretty.Reset)
    }
    common.Stdout("\n")
    
    validator := memberValidation(
        candidates,
        fmt.Sprintf("Unknown option. Choose from: %s", strings.Join(candidates, ", ")),
    )
    
    return ask(question, candidates[0], validator)
}
```

**Usage:**
```go
template, err := chooseByName("Choose a template", "Available templates", 
    []string{"python", "standard", "extended"})

// Output:
// Available templates:
//   • python
//   • standard
//   • extended
//
// ? Choose a template [python]: standard
```

### Feature 2.4: Error Recovery Prompts

Handle errors gracefully with retry/skip/abort options.

**File:** `wizard/recovery.go` (new)

```go
package wizard

// RecoveryAction represents possible error recovery actions
type RecoveryAction string

const (
    ActionRetry  RecoveryAction = "retry"
    ActionSkip   RecoveryAction = "skip"
    ActionAbort  RecoveryAction = "abort"
)

// AskRecovery prompts user for error recovery action
func AskRecovery(errorMessage string) (RecoveryAction, error) {
    note("Error: %s", errorMessage)
    
    action, err := ChooseAction(
        "How to proceed?",
        []string{"retry", "skip", "abort"},
        "retry",
    )
    if err != nil {
        return ActionAbort, err
    }
    
    return RecoveryAction(action), nil
}
```

**Usage:**
```go
for {
    err := downloadFile(url)
    if err != nil {
        action, _ := wizard.AskRecovery(err.Error())
        switch action {
        case wizard.ActionRetry:
            continue
        case wizard.ActionSkip:
            return nil
        case wizard.ActionAbort:
            return err
        }
    }
    break
}
```

---

## Implementation Priority

### Phase 1: Core Infrastructure (Foundation)
| Priority | Feature | Effort | Impact |
|----------|---------|--------|--------|
| 1.1 | Core cursor control functions | Low | Required for all dashboards |
| 1.2 | `ShouldUseDashboard()` detection | Low | Graceful degradation |
| 1.3 | `ProgressBar()` function | Low | Reused by all layouts |

### Phase 2: Interactive Prompts
| Priority | Feature | Effort | Impact |
|----------|---------|--------|--------|
| 2.1 | Yes/No Confirmation (`Confirm`) | Low | High - safer destructive ops |
| 2.2 | Action Selection (`ChooseAction`) | Low | Medium - cleaner UX |
| 2.3 | Error Recovery (`AskRecovery`) | Low | Medium - better error handling |

### Phase 3: Dashboard Layouts
| Priority | Feature | Effort | Impact |
|----------|---------|--------|--------|
| 3.1 | Layout E: Inline Progress | Low | Quick wins, low risk |
| 3.2 | Layout C: Download Progress | Low | Common operation |
| 3.3 | Layout A: Build Dashboard | Medium | High - most visible operation |
| 3.4 | Layout B: Diagnostics Dashboard | Medium | Helpful for troubleshooting |
| 3.5 | Layout D: Multi-Task Progress | Medium | Prebuild operations |
| 3.6 | Layout F: Robot Run Dashboard | High | Complex state tracking |

### Phase 4: Polish
| Priority | Feature | Effort | Impact |
|----------|---------|--------|--------|
| 4.1 | Severity Colors | Low | Visual polish |
| 4.2 | RGB/TrueColor support | Low | Modern terminal support |
| 4.3 | Terminal width adaptation | Medium | Better UX on narrow terminals |

---

## Testing Considerations

1. **Terminal Detection**: Features should gracefully degrade when not in interactive terminal
2. **Color Support**: Check `TERM` and `COLORTERM` environment variables
3. **Width Detection**: Progress bars should adapt to terminal width
4. **CI/CD**: Ensure non-interactive mode works in pipelines

---

## Robot Framework Test Specifications

All UI enhancements must have comprehensive Robot Framework tests following the existing patterns in `robot_tests/`. Tests ensure both interactive and non-interactive modes work correctly.

### Test File Structure

Create new test file: `robot_tests/ui_enhancements.robot`

```robot
*** Settings ***
Library         OperatingSystem
Library         supporting.py
Resource        resources.robot
Suite Setup     UI Test Setup

*** Keywords ***
UI Test Setup
    Fire And Forget    build/rcc ht delete 4e67cd8
    Comment    Ensure clean state for UI tests
```

### Test Categories

#### Category 1: Progress Bar Tests

```robot
*** Test Cases ***

Goal: Progress output appears during environment build
    [Documentation]    Verify that progress messages appear during holotree operations
    Step        build/rcc holotree variables --space uitest --controller citests robot_tests/conda.yaml
    Use STDERR
    Must Have   Progress: 01/15
    Must Have   Progress: 15/15
    Must Have   OK.

Goal: Progress shows correct step sequence
    [Documentation]    Verify progress steps appear in order
    Step        build/rcc holotree variables --space uitest2 --controller citests robot_tests/conda.yaml --timeline
    Use STDERR
    Must Have   Context:
    Must Have   Fresh
    Must Have   holotree done

Goal: Progress works with force flag
    [Documentation]    Verify progress works when forcing rebuild
    Step        build/rcc holotree variables --space uitest --controller citests robot_tests/conda.yaml --force
    Use STDERR
    Must Have   Progress: 01/15
    Must Have   Progress: 15/15
    Must Have   OK.

Goal: Progress output is suppressed with silent flag
    [Documentation]    Verify silent mode suppresses progress
    Step        build/rcc holotree hash --silent --controller citests robot_tests/conda.yaml
    Use STDERR
    Wont Have   Progress:

Goal: Timeline shows step durations
    [Documentation]    Verify timeline flag shows timing information
    Step        build/rcc holotree variables --space uitest --controller citests robot_tests/conda.yaml --timeline
    Use STDERR
    Must Have   Progress:
    Must Have   s
```

#### Category 2: Color and Formatting Tests

```robot
*** Test Cases ***

Goal: Colors appear in interactive mode simulation
    [Documentation]    Verify ANSI codes present when colors enabled
    [Tags]    interactive
    Step        build/rcc version --controller citests
    # Note: Actual ANSI detection requires terminal, this validates no crashes
    Use STDOUT
    Must Have   v

Goal: Output works without colors in CI mode
    [Documentation]    Verify output works when CI=true
    [Tags]    ci
    ${code}    ${output}    ${error}=    Run and return code output error    CI=true build/rcc version --controller citests
    Should Be Equal As Strings    0    ${code}
    Should Contain    ${output}    v

Goal: Diagnostics show status indicators
    [Documentation]    Verify diagnostic output has status markers
    Step        build/rcc configure diagnostics --controller citests
    Use STDOUT
    Must Have   rcc
    Must Have   ROBOCORP_HOME
```

#### Category 3: Wizard and Prompt Tests (Non-Interactive Validation)

```robot
*** Test Cases ***

Goal: Robot init works with all flags provided
    [Documentation]    Verify wizard can be bypassed with command line flags
    Step        build/rcc robot init --controller citests -t standard -d tmp/wizardtest -f
    Use STDERR
    Must Have   OK.
    Must Exist  tmp/wizardtest/robot.yaml
    Must Exist  tmp/wizardtest/conda.yaml

Goal: Template list shows available templates
    [Documentation]    Verify templates can be listed
    Step        build/rcc robot init --controller citests --list
    Use STDOUT
    Must Have   standard
    Must Have   python
    Must Have   extended

Goal: Invalid template name gives helpful error
    [Documentation]    Verify error message for invalid template
    Step        build/rcc robot init --controller citests -t nonexistent -d tmp/badtemplate -f    expected=1
    Use STDERR
    Must Have   template

Goal: Force flag prevents interactive prompts
    [Documentation]    Verify force flag allows non-interactive execution
    Step        build/rcc robot init --controller citests -t standard -d tmp/forcetest -f
    Use STDERR
    Must Have   OK.
```

#### Category 4: Dashboard Graceful Degradation Tests

```robot
*** Test Cases ***

Goal: Build works in non-interactive environment
    [Documentation]    Verify builds complete without TTY
    Step        build/rcc holotree variables --space nointeractive --controller citests robot_tests/conda.yaml
    Use STDERR
    Must Have   Progress:
    Must Have   OK.

Goal: Long operations show progress without dashboard
    [Documentation]    Verify progress appears in log format
    Step        build/rcc holotree variables --space logtest --controller citests robot_tests/conda.yaml --timeline
    Use STDERR
    Must Have   Progress: 01/15
    Must Have   Progress: 15/15

Goal: Environment variable disables rich output
    [Documentation]    Verify RCC_NO_DASHBOARD=true works
    ${code}    ${output}    ${error}=    Run and return code output error    RCC_NO_DASHBOARD=true build/rcc holotree list --controller citests
    Should Be Equal As Strings    0    ${code}
```

#### Category 5: Download and Pull Progress Tests

```robot
*** Test Cases ***

Goal: Pull command shows progress information
    [Documentation]    Verify download operations show status
    [Tags]    network
    Fire And Forget    build/rcc holotree delete --controller citests
    # Note: This test requires network access and valid remote
    # Skip in offline environments
    Step        build/rcc community pull --controller citests --debug
    Use STDERR
    # Should show download attempt even if it fails
    Must Have   Downloading

Goal: Import shows file processing status
    [Documentation]    Verify import operations report progress
    [Tags]    requires-export
    # First create an export to import
    Step        build/rcc holotree export --controller citests -o tmp/test_export.zip
    Use STDERR
    Must Have   OK.
    # Then import it
    Step        build/rcc holotree import --controller citests tmp/test_export.zip
    Use STDERR
    Must Have   OK.
```

#### Category 6: Multi-Task Progress Tests (Prebuild)

```robot
*** Test Cases ***

Goal: Prebuild shows per-environment progress
    [Documentation]    Verify prebuild reports status for each environment
    [Tags]    long-running
    # Create a metafile with multiple conda.yaml references
    Create File    tmp/prebuild_meta.txt    robot_tests/conda.yaml\n
    Step        build/rcc holotree prebuild --controller citests -m tmp/prebuild_meta.txt
    Use STDERR
    Must Have   1/1
    Must Have   OK.

Goal: Prebuild continues on individual failures
    [Documentation]    Verify prebuild doesn't stop on first error
    [Tags]    long-running    error-handling
    # Create metafile with one good and one bad entry
    Create File    tmp/prebuild_mixed.txt    robot_tests/conda.yaml\nrobot_tests/nonexistent.yaml\n
    Step        build/rcc holotree prebuild --controller citests -m tmp/prebuild_mixed.txt    expected=0
    Use STDERR
    # Should complete the valid one
    Must Have   1/2
```

#### Category 7: Confirmation Prompt Tests (Future Feature)

```robot
*** Test Cases ***

Goal: Destructive operations require confirmation flag
    [Documentation]    Verify dangerous operations need explicit flag
    [Tags]    future    confirmation
    # When confirmation feature is implemented:
    # Step    build/rcc holotree delete --all --controller citests    expected=1
    # Use STDERR
    # Must Have   confirmation
    # Must Have   --yes
    Pass Execution    Future feature - confirmation prompts not yet implemented

Goal: Yes flag bypasses confirmation
    [Documentation]    Verify --yes flag works for scripting
    [Tags]    future    confirmation
    # When implemented:
    # Step    build/rcc holotree delete --all --yes --controller citests
    # Use STDERR
    # Must Have   OK
    Pass Execution    Future feature - confirmation prompts not yet implemented
```

### Supporting Python Functions

Add to `robot_tests/supporting.py`:

```python
def run_with_terminal_simulation(command: str) -> tuple[int, str, str]:
    """
    Run command simulating an interactive terminal.
    Sets TERM and forces pseudo-TTY if possible.
    """
    import os
    import pty
    import subprocess
    
    env = os.environ.copy()
    env["TERM"] = "xterm-256color"
    env["COLORTERM"] = "truecolor"
    
    # Note: pty only works on Unix
    if sys.platform != "win32":
        master, slave = pty.openpty()
        try:
            proc = subprocess.Popen(
                command,
                shell=True,
                stdin=slave,
                stdout=slave,
                stderr=slave,
                env=env,
                cwd=get_cwd(),
            )
            proc.wait()
            os.close(slave)
            output = os.read(master, 65536).decode()
            os.close(master)
            return proc.returncode, output, ""
        except Exception as e:
            return 1, "", str(e)
    else:
        return run_and_return_code_output_error(command, env=env)


def strip_ansi_codes(text: str) -> str:
    """Remove ANSI escape codes from text for comparison."""
    import re
    ansi_escape = re.compile(r'\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])')
    return ansi_escape.sub('', text)


def output_contains_progress_bar(output: str) -> bool:
    """Check if output contains progress bar characters."""
    progress_chars = ['█', '░', '▓', '▒', '[', ']', '%']
    return any(char in output for char in progress_chars)


def verify_ansi_codes_present(output: str) -> bool:
    """Verify that ANSI escape codes are present in output."""
    return '\x1b[' in output or '\033[' in output
```

### Test Tags and Execution

```robot
*** Settings ***
# Add these force tags for categorization
Force Tags    ui    enhancements

# Test execution examples:
# Run all UI tests:           robot -i ui robot_tests/ui_enhancements.robot
# Run only interactive tests: robot -i interactive robot_tests/ui_enhancements.robot
# Skip network tests:         robot -e network robot_tests/ui_enhancements.robot
# Skip long-running tests:    robot -e long-running robot_tests/ui_enhancements.robot
# Run future feature tests:   robot -i future robot_tests/ui_enhancements.robot
```

### Expected Test Outputs

Create directory `robot_tests/expected/` with baseline outputs:

```
robot_tests/expected/
├── progress_build.txt          # Expected progress output format
├── diagnostics_output.txt      # Expected diagnostics format
├── template_list.txt           # Expected template listing
└── version_output.txt          # Expected version format
```

### Integration with CI/CD

Add to `.github/workflows/rcc.yaml`:

```yaml
  ui-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.20'
      - name: Set up Python
        uses: actions/setup-python@v4
        with:
          python-version: '3.10'
      - name: Install dependencies
        run: |
          python -m pip install --upgrade pip
          pip install -r robot_requirements.txt
      - name: Build RCC
        run: inv local
      - name: Run UI Tests
        run: |
          python -m robot -L DEBUG -d tmp/output \
            --exclude network \
            --exclude long-running \
            --exclude future \
            robot_tests/ui_enhancements.robot
      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v3
        with:
          name: ui-test-results
          path: tmp/output/
```

### Test Coverage Requirements

Before merging UI enhancements, ensure:

| Feature | Required Tests | Coverage |
|---------|---------------|----------|
| Progress Bar | 5+ tests | All step counts, timing, silent mode |
| Colors | 3+ tests | Enable/disable, CI mode, ANSI presence |
| Wizard Prompts | 4+ tests | All flags, error cases, templates |
| Dashboard Degradation | 3+ tests | No TTY, CI env, disable flag |
| Download Progress | 2+ tests | Status messages, offline handling |
| Multi-Task Progress | 2+ tests | Per-item status, error continuation |
| Confirmation Prompts | 3+ tests | Require flag, bypass with --yes |

---

## Related Files

- `pretty/internal.go` - Contains `csif` and `csi` functions
- `pretty/variables.go` - Current color definitions
- `wizard/common.go` - Contains `memberValidation` and `ask` functions
- `wizard/create.go` - Current wizard implementation
