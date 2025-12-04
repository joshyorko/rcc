# Unified Dashboard Architecture for RCC

**Date**: 2025-12-04
**Status**: Design Proposal
**Inspired By**: [terminal.shop](https://github.com/terminaldotshop/terminal), [opencode](https://github.com/sst/opencode), k9s

## Executive Summary

The current RCC dashboard implementation feels disconnected - more like a loosely integrated add-on than a core feature. Logs are scattered, progress bars behave inconsistently (moving backwards), key data is missing, and the overall experience lacks cohesion.

This document proposes a unified dashboard architecture that:
1. Provides a **single, cohesive dashboard** that integrates across ALL RCC commands
2. Centralizes **log aggregation and status display**
3. Implements **consistent, forward-only progress indicators**
4. Delivers a **polished, modern CLI experience** comparable to k9s

---

## Research Findings

### Terminal Shop Architecture (Go/Bubble Tea)

**Key Patterns:**
```
packages/go/
├── pkg/tui/
│   ├── root.go          # Central model managing all state
│   ├── theme/           # Centralized theming
│   │   ├── theme.go     # Theme struct with semantic colors
│   │   └── huh.go       # Form theming
│   ├── styles.go        # Reusable style helpers
│   ├── header.go        # Fixed header component
│   ├── footer.go        # Fixed footer with commands
│   ├── breadcrumbs.go   # Navigation context
│   └── [pages].go       # Individual page components
```

**Strengths:**
- **Single Root Model**: One `model` struct owns ALL application state
- **Centralized Theme**: `Theme` struct with semantic methods (`TextBody()`, `TextAccent()`, `TextHighlight()`)
- **Responsive Sizing**: Four breakpoints (undersized, small, medium, large)
- **Clean Layout**: Header + Breadcrumbs + Content + Footer pattern
- **Page-Based Navigation**: `SwitchPage()` method with page constants

**Dependencies:**
```go
github.com/charmbracelet/bubbles     // UI components
github.com/charmbracelet/bubbletea   // Application framework
github.com/charmbracelet/lipgloss    // Styling
github.com/charmbracelet/huh         // Forms
```

### Current RCC Dashboard Issues

| Issue | Root Cause | Impact |
|-------|-----------|--------|
| Disconnected feel | Multiple independent dashboard types | No unified experience |
| Scattered logs | Log interceptor only suppresses, doesn't aggregate | Users miss important info |
| Progress moves backwards | Manual progress calculation in each command | Confusing UX |
| Missing data | Dashboard not integrated with command internals | Incomplete status |
| No navigation | Each dashboard is single-purpose | Can't see history/context |

---

## Proposed Architecture

### 1. Unified Theme System

Create a centralized theme that ALL dashboard components use:

```go
// pretty/theme.go
package pretty

import "github.com/charmbracelet/lipgloss"

type Theme struct {
    renderer *lipgloss.Renderer

    // Semantic colors
    background lipgloss.TerminalColor
    foreground lipgloss.TerminalColor
    border     lipgloss.TerminalColor
    accent     lipgloss.TerminalColor  // Primary brand color
    success    lipgloss.TerminalColor  // Green
    warning    lipgloss.TerminalColor  // Yellow
    error      lipgloss.TerminalColor  // Red
    muted      lipgloss.TerminalColor  // Dim text

    // Base styles
    base lipgloss.Style
}

func NewTheme(renderer *lipgloss.Renderer) Theme {
    t := Theme{renderer: renderer}

    t.background = lipgloss.AdaptiveColor{Dark: "#1a1b26", Light: "#f5f5f5"}
    t.foreground = lipgloss.AdaptiveColor{Dark: "#c0caf5", Light: "#343b58"}
    t.border     = lipgloss.AdaptiveColor{Dark: "#3b4261", Light: "#9aa5ce"}
    t.accent     = lipgloss.Color("#7aa2f7")  // RCC brand blue
    t.success    = lipgloss.Color("#9ece6a")
    t.warning    = lipgloss.Color("#e0af68")
    t.error      = lipgloss.Color("#f7768e")
    t.muted      = lipgloss.AdaptiveColor{Dark: "#565f89", Light: "#9aa5ce"}

    t.base = renderer.NewStyle().Foreground(t.foreground)
    return t
}

// Semantic style methods
func (t Theme) Base() lipgloss.Style           { return t.base.Copy() }
func (t Theme) TextMuted() lipgloss.Style      { return t.Base().Foreground(t.muted) }
func (t Theme) TextAccent() lipgloss.Style     { return t.Base().Foreground(t.accent) }
func (t Theme) TextSuccess() lipgloss.Style    { return t.Base().Foreground(t.success) }
func (t Theme) TextWarning() lipgloss.Style    { return t.Base().Foreground(t.warning) }
func (t Theme) TextError() lipgloss.Style      { return t.Base().Foreground(t.error) }

func (t Theme) BorderColor() lipgloss.TerminalColor { return t.border }
func (t Theme) AccentColor() lipgloss.TerminalColor { return t.accent }
```

### 2. Unified Dashboard Model

A single root model that can display ANY RCC operation:

```go
// pretty/unified_model.go
package pretty

type DashboardMode int

const (
    ModeEnvironment DashboardMode = iota  // Building environments
    ModeDiagnostics                        // Running diagnostics
    ModeDownload                           // File downloads
    ModeRobotRun                           // Robot execution
    ModeMultiTask                          // Parallel operations
    ModeInteractive                        // Wizard/prompts
)

type UnifiedModel struct {
    // Core state
    mode          DashboardMode
    theme         Theme
    width         int
    height        int

    // Layout components
    header        HeaderModel
    footer        FooterModel
    statusBar     StatusBarModel

    // Operation state
    title         string
    steps         []Step
    currentStep   int
    logs          *LogBuffer      // Circular buffer for logs
    progress      float64         // 0.0 to 1.0 (ONLY increases)
    startTime     time.Time

    // Sub-models for different modes
    envModel      *EnvironmentModel
    diagModel     *DiagnosticsModel
    robotModel    *RobotModel
    downloadModel *DownloadModel

    // Bubble Tea components
    spinner       spinner.Model
    progressBar   progress.Model
    viewport      viewport.Model  // For scrollable content
}
```

### 3. Command Integration Pattern

Each RCC command integrates with the unified dashboard through a simple API:

```go
// operations/dashboard_integration.go
package operations

import "github.com/joshyorko/rcc/pretty"

// DashboardContext provides commands with dashboard access
type DashboardContext struct {
    dashboard pretty.Dashboard
    steps     []string
}

// StartOperation begins a tracked operation
func (dc *DashboardContext) StartOperation(name string, steps []string) {
    dc.steps = steps
    dc.dashboard = pretty.NewUnifiedDashboard(name, steps)
    dc.dashboard.Start()
}

// SetStep updates current step (progress only moves forward)
func (dc *DashboardContext) SetStep(index int, message string) {
    if index >= dc.currentStep {  // NEVER go backwards
        dc.currentStep = index
        dc.dashboard.SetStep(index, pretty.StepRunning, message)
    }
}

// CompleteStep marks a step as done
func (dc *DashboardContext) CompleteStep(index int) {
    dc.dashboard.SetStep(index, pretty.StepComplete, "")
}

// Log adds a log line (captured by dashboard)
func (dc *DashboardContext) Log(level, message string) {
    dc.dashboard.AddLog(level, message)
}

// Finish completes the operation
func (dc *DashboardContext) Finish(success bool) {
    dc.dashboard.Stop(success)
}
```

### 4. Log Aggregation System

Instead of suppressing logs, aggregate them into a scrollable viewport:

```go
// pretty/log_buffer.go
package pretty

type LogLevel int

const (
    LogTrace LogLevel = iota
    LogDebug
    LogInfo
    LogWarn
    LogError
)

type LogEntry struct {
    Time    time.Time
    Level   LogLevel
    Source  string   // Command or component name
    Message string
}

type LogBuffer struct {
    entries []LogEntry
    maxSize int
    mu      sync.RWMutex
}

func NewLogBuffer(maxSize int) *LogBuffer {
    return &LogBuffer{
        entries: make([]LogEntry, 0, maxSize),
        maxSize: maxSize,
    }
}

func (lb *LogBuffer) Add(level LogLevel, source, message string) {
    lb.mu.Lock()
    defer lb.mu.Unlock()

    entry := LogEntry{
        Time:    time.Now(),
        Level:   level,
        Source:  source,
        Message: message,
    }

    lb.entries = append(lb.entries, entry)
    if len(lb.entries) > lb.maxSize {
        lb.entries = lb.entries[1:]  // Circular buffer
    }
}

func (lb *LogBuffer) Recent(n int) []LogEntry {
    lb.mu.RLock()
    defer lb.mu.RUnlock()

    if n > len(lb.entries) {
        n = len(lb.entries)
    }
    return lb.entries[len(lb.entries)-n:]
}
```

### 5. Progress Tracking (Forward-Only)

Ensure progress NEVER moves backwards:

```go
// pretty/progress_tracker.go
package pretty

type ProgressTracker struct {
    steps       []StepState
    minProgress float64  // Progress can never go below this
    mu          sync.Mutex
}

func NewProgressTracker(stepCount int) *ProgressTracker {
    steps := make([]StepState, stepCount)
    for i := range steps {
        steps[i] = StepState{Status: StepPending}
    }
    return &ProgressTracker{steps: steps}
}

// SetStep updates a step, ensuring progress only increases
func (pt *ProgressTracker) SetStep(index int, status StepStatus, message string) {
    pt.mu.Lock()
    defer pt.mu.Unlock()

    if index < 0 || index >= len(pt.steps) {
        return
    }

    // Only allow forward progress
    currentStatus := pt.steps[index].Status
    if !canTransition(currentStatus, status) {
        return  // Ignore invalid transitions
    }

    pt.steps[index].Status = status
    pt.steps[index].Message = message

    // Update minimum progress
    newProgress := pt.calculateProgress()
    if newProgress > pt.minProgress {
        pt.minProgress = newProgress
    }
}

func canTransition(from, to StepStatus) bool {
    // Valid transitions:
    // Pending -> Running, Skipped
    // Running -> Complete, Failed
    // Complete, Failed, Skipped -> (no transitions)
    switch from {
    case StepPending:
        return to == StepRunning || to == StepSkipped
    case StepRunning:
        return to == StepComplete || to == StepFailed
    default:
        return false
    }
}

func (pt *ProgressTracker) Progress() float64 {
    pt.mu.Lock()
    defer pt.mu.Unlock()
    return pt.minProgress
}

func (pt *ProgressTracker) calculateProgress() float64 {
    completed := 0
    for _, step := range pt.steps {
        if step.Status == StepComplete || step.Status == StepSkipped {
            completed++
        }
    }
    return float64(completed) / float64(len(pt.steps))
}
```

### 6. Dashboard Layout Structure

```
┌─────────────────────────────────────────────────────────────────────┐
│  RCC v18.0.0                              Environment: python-3.11  │ <- Header
├─────────────────────────────────────────────────────────────────────┤
│  Building Environment ─────────────────────────────────  4/15 steps │ <- Title Bar
├─────────────────────────────────────────────────────────────────────┤
│  ● 1. Checking holotree                                      0.2s   │
│  ● 2. Resolving dependencies                                 1.4s   │
│  ● 3. Downloading packages                                   3.2s   │
│  ⠋ 4. Installing pip packages...                                    │ <- Steps
│  ○ 5. Setting up paths                                              │
│  ○ 6. Verifying environment                                         │
│    ...                                                              │
├─────────────────────────────────────────────────────────────────────┤
│  [████████████░░░░░░░░░░░░░░░░░░░░░░░░░░]  27%  ETA: 45s            │ <- Progress
├─────────────────────────────────────────────────────────────────────┤
│  12:34:56 [INFO]  Installing numpy==1.24.0                          │
│  12:34:57 [INFO]  Installing pandas==2.0.0                          │ <- Log View
│  12:34:58 [DEBUG] Resolving dependency tree...                      │   (scrollable)
├─────────────────────────────────────────────────────────────────────┤
│  [q] Quit   [l] Toggle Logs   [↑↓] Scroll                           │ <- Footer
└─────────────────────────────────────────────────────────────────────┘
```

### 7. Command Coverage Matrix

| Command | Dashboard Mode | Steps Shown | Logs Captured |
|---------|---------------|-------------|---------------|
| `rcc run` | RobotRun | Task steps | Robot output |
| `rcc holotree vars` | Environment | 15+ env steps | Conda/pip logs |
| `rcc holotree pull` | Download | Download phases | Transfer logs |
| `rcc holotree import` | Environment | Import steps | File operations |
| `rcc holotree export` | MultiTask | Export tasks | Archive logs |
| `rcc diagnostics` | Diagnostics | Check items | Check results |
| `rcc download` | Download | Single file | Transfer logs |
| `rcc create` | Interactive | Wizard steps | (none) |
| `rcc robot bundle` | MultiTask | Bundle steps | Archive logs |
| `rcc cloud new` | Environment | Cloud setup | API logs |
| `rcc cloud prepare` | Environment | Prep steps | Build logs |

---

## Implementation Plan

### Phase 1: Foundation
1. Create `pretty/theme.go` with unified theme system
2. Create `pretty/log_buffer.go` for log aggregation
3. Create `pretty/progress_tracker.go` with forward-only logic
4. Update existing dashboards to use new theme

### Phase 2: Unified Model
1. Create `pretty/unified_model.go` as root dashboard model
2. Implement Header, Footer, StatusBar components
3. Add viewport-based log display
4. Wire up keyboard navigation (q, l, arrows)

### Phase 3: Command Integration
1. Create `operations/dashboard_context.go`
2. Update `operations/running.go` to use dashboard context
3. Integrate with holotree commands
4. Integrate with download operations

### Phase 4: Polish
1. Add responsive sizing (like terminal shop)
2. Implement smooth animations
3. Add ETA calculations based on step history
4. Test across terminal types

---

## Key Design Principles

1. **Single Source of Truth**: One model owns all state
2. **Forward-Only Progress**: Progress NEVER moves backwards
3. **Captured Logs**: All output goes through the dashboard
4. **Responsive Layout**: Adapts to terminal size
5. **Semantic Theming**: Colors have meaning, not just aesthetics
6. **Graceful Degradation**: Falls back cleanly for non-interactive terminals

---

## Dependencies

Already available in RCC:
```go
github.com/charmbracelet/bubbles
github.com/charmbracelet/bubbletea
github.com/charmbracelet/lipgloss
```

**Note**: The current spec says "bubbletea out of scope" but it's already being used in `tea_dashboard.go`. This design embraces that existing dependency.

---

## Success Metrics

1. **Cohesion**: Dashboard feels like a core RCC feature, not an add-on
2. **No Backwards Progress**: Progress bar ONLY moves forward
3. **Log Visibility**: Users can see all relevant logs in one place
4. **Navigation**: Easy to switch between views and scroll history
5. **Polish**: Comparable to k9s/lazygit in visual quality
