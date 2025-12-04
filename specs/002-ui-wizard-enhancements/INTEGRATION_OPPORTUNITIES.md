# Dashboard Integration Opportunities

This document identifies opportunities to integrate the newly implemented dashboard system into RCC commands.

**Status**: Documentation only - No code modifications have been made to command files.

**Generated**: 2025-12-04 (Phase 20, T125-T132)

---

## Overview

All six dashboard layouts (A-F) have been implemented in the `pretty` package:

1. **Layout A**: Environment Dashboard (`EnvironmentDashboard`)
2. **Layout B**: Diagnostics Dashboard (`diagnosticsDashboard`)
3. **Layout C**: Multi-Task Dashboard (`MultiTaskDashboard`)
4. **Layout D**: Compact Progress (`CompactProgress`)
5. **Layout E**: Download Progress Bar (integrated into `Download()`)
6. **Layout F**: Robot Run Dashboard (`RobotRunDashboard`)

Each dashboard automatically detects terminal capabilities and falls back gracefully when not in interactive mode.

---

## T125-T126: Environment Dashboard Integration

### Current State in `htfs/commands.go`

The `NewEnvironment()` function currently uses a `DelayedSpinner` (lines 23-38):

```go
// environmentSpinner tracks spinner state for environment operations
var environmentSpinner *pretty.DelayedSpinner

func NewEnvironment(condafile, holozip string, restore, force bool, puller CatalogPuller) (label string, scorecard common.Scorecard, err error) {
    defer fail.Around(&err)

    // Start delayed spinner for environment creation
    environmentSpinner = pretty.NewDelayedSpinner("Creating environment...")
    environmentSpinner.Start()
    defer func() {
        if environmentSpinner != nil {
            environmentSpinner.Stop(err == nil)
        }
    }()
    // ... rest of function
}
```

The spinner is updated at various points:
- Line 94: "Waiting for holotree lock..."
- Line 103: "Composing blueprint..."
- Line 137: "Restoring environment from library..."
- Line 184: "Checking remote catalog..."
- Line 200: "Preparing holotree stage..."
- Line 212: "Building environment..."
- Line 236: "Recording to hololib..."

### Integration Opportunity

**Replace** `DelayedSpinner` **with** `EnvironmentDashboard` to provide a richer, step-by-step visualization.

#### Proposed Steps Mapping

```go
steps := []string{
    "Acquiring holotree lock",
    "Composing blueprint",
    "Checking remote catalog",
    "Preparing holotree stage",
    "Building environment",
    "Recording to hololib",
    "Restoring environment",
}

dashboard := pretty.NewEnvironmentDashboard(steps)
dashboard.Start()
defer dashboard.Stop(err == nil)
```

#### Benefits

1. **Visual Progress**: Users see all steps at once with their status
2. **Parallel Awareness**: Multiple workers visible during build (lines 66, 68, 109, 139, 238)
3. **Time Tracking**: Dashboard shows elapsed time automatically
4. **Better Context**: Users understand which phase is taking time
5. **Graceful Fallback**: Automatically falls back to simple progress in non-interactive mode

#### Integration Points

Replace spinner updates with dashboard step updates:

| Current Line | Current Code | Proposed Replacement |
|--------------|--------------|----------------------|
| 94 | `environmentSpinner.Update(0, "Waiting...")` | `dashboard.SetStep(0, pretty.StepRunning, "Acquiring lock")` |
| 103 | `environmentSpinner.Update(0, "Composing...")` | `dashboard.SetStep(1, pretty.StepRunning, "Composing blueprint")` |
| 184 | `environmentSpinner.Update(0, "Checking...")` | `dashboard.SetStep(2, pretty.StepRunning, "Checking catalog")` |
| 200 | `environmentSpinner.Update(0, "Preparing...")` | `dashboard.SetStep(3, pretty.StepRunning, "Preparing stage")` |
| 212 | `environmentSpinner.Update(0, "Building...")` | `dashboard.SetStep(4, pretty.StepRunning, "Building environment")` |
| 236 | `environmentSpinner.Update(0, "Recording...")` | `dashboard.SetStep(5, pretty.StepRunning, "Recording to hololib")` |
| 137 | `environmentSpinner.Update(0, "Restoring...")` | `dashboard.SetStep(6, pretty.StepRunning, "Restoring environment")` |

#### Implementation Notes

- The dashboard will detect terminal capabilities automatically via `pretty.ShouldUseDashboard()`
- In non-TTY environments, it returns a `noopDashboard` that silently ignores all operations
- No need to check `pretty.Interactive` manually - the dashboard handles it
- The existing `pretty.Progress()` calls can remain for logging purposes

---

## T127: Robot Run Dashboard Integration

### Current State in `cmd/run.go` and `operations/running.go`

The `runCmd` executes robot tasks through:

```go
// cmd/run.go:32-36
simple, config, todo, label := operations.LoadTaskWithEnvironment(robotFile, runTask, forceFlag)
commandline := todo.Commandline()
commandline = append(commandline, args...)
operations.SelectExecutionModel(captureRunFlags(false), simple, commandline, config, todo, label, interactiveFlag, nil)
```

The `SelectExecutionModel` function (operations/running.go:203-217) branches to either:
- `ExecuteSimpleTask()` for simple runs
- `ExecuteTask()` for full task execution

### Integration Opportunity

**Add** `RobotRunDashboard` **to track robot task execution in real-time**.

#### Proposed Integration in `SelectExecutionModel`

```go
func SelectExecutionModel(runFlags *RunFlags, simple bool, template []string,
                          config robot.Robot, todo robot.Task, label string,
                          interactive bool, extraEnv map[string]string) {

    common.TimelineBegin("robot execution (simple=%v).", simple)
    common.RunJournal("start", "robot", "started")
    defer common.RunJournal("stop", "robot", "done")
    defer common.TimelineEnd()

    // NEW: Create robot run dashboard
    dashboard := pretty.NewRobotRunDashboard(config.Name())
    dashboard.Start()
    defer dashboard.Stop(err == nil)

    pathlib.EnsureDirectoryExists(config.ArtifactDirectory())
    if simple {
        common.RunJournal("select", "robot", "simple run")
        pathlib.NoteDirectoryContent("[Before run] Artifact dir", config.ArtifactDirectory(), true)
        ExecuteSimpleTask(runFlags, template, config, todo, interactive, extraEnv)
    } else {
        common.RunJournal("run", "robot", "task run")
        ExecuteTask(runFlags, template, config, todo, label, interactive, extraEnv)
    }
}
```

#### Dashboard Features

The `RobotRunDashboard` (Layout F) provides:

1. **Header Section**: Robot name, task name, elapsed time
2. **Scrolling Output**: Last N lines of robot stdout/stderr (configurable)
3. **Statistics Footer**: Task counts (pass/fail/skip)
4. **Real-time Updates**: Updates during task execution

#### Integration Points

The dashboard needs to receive updates from the robot execution:

| Event | Method Call | Purpose |
|-------|-------------|---------|
| Task starts | `dashboard.SetTaskName(taskName)` | Show current task |
| Output line | `dashboard.AddOutput(line)` | Add to scrolling buffer |
| Task completes | `dashboard.SetStats(stats)` | Update pass/fail counts |
| Error occurs | `dashboard.Stop(false)` | Show failure state |

#### Benefits

1. **Live Feedback**: Users see robot output as it happens
2. **Progress Awareness**: Clear indication of which task is running
3. **Statistics Tracking**: See cumulative results during long runs
4. **Output History**: Scroll back through recent output
5. **Graceful Degradation**: Falls back to normal output in non-TTY mode

#### Implementation Notes

- The dashboard is designed to coexist with `common.RunJournal()` calls
- Existing journal entries remain for debugging and logging
- In interactive mode, output goes to dashboard; in non-interactive, to stdout
- The `interactiveFlag` parameter should be checked alongside `pretty.Interactive`

---

## T128: Diagnostics Dashboard Integration

### Current State in `operations/diagnostics.go`

The `runDiagnostics()` function (lines 60-198) performs multiple checks:

1. **System information gathering** (lines 65-118)
2. **Shared directory checks** (lines 130-134) if `common.SharedHolotree`
3. **Environment variable checks** (lines 146-158)
4. **Lock file checks** (lines 167-168)
5. **Slow checks** (lines 173-196) - DNS, TLS, downloads

Results are displayed via:
- `jsonDiagnostics()` for JSON output (lines 557-563)
- `humaneDiagnostics()` for human-readable output (lines 565-591)

### Integration Opportunity

**Add** `DiagnosticsDashboard` **to show live progress during diagnostic checks**.

#### Proposed Integration in `runDiagnostics`

```go
func runDiagnostics(quick bool) *common.DiagnosticStatus {
    result := &common.DiagnosticStatus{
        Details: make(map[string]string),
        Checks:  []*common.DiagnosticCheck{},
    }

    // Define check names
    checkNames := []string{
        "System Information",
        "Shared Directory Verification",
        "Product Home Check",
        "Environment Variables",
        "Lock Files",
    }

    if !quick {
        checkNames = append(checkNames,
            "DNS Lookups",
            "TLS Verification",
            "Canary Download",
            "PyPI Check",
            "Conda Check",
        )
    }

    // NEW: Create diagnostics dashboard
    dashboard := pretty.NewDiagnosticsDashboard(checkNames)
    dashboard.Start()
    defer dashboard.Stop(true)

    // Update dashboard as checks complete
    dashboard.SetStep(0, pretty.StepRunning, "Gathering system info")
    // ... gather system info (lines 65-118)
    dashboard.SetStep(0, pretty.StepComplete, "System info collected")

    // ... continue for each check group

    return result
}
```

#### Dashboard Features

The `diagnosticsDashboard` (Layout B) provides:

1. **Checklist View**: All checks listed with status icons
2. **Real-time Status**: See which check is running, passed, or failed
3. **Progress Tracking**: Visual indication of overall progress
4. **Failure Details**: Warnings and errors shown inline

#### Integration Points

| Check Group | Current Lines | Dashboard Step |
|-------------|---------------|----------------|
| System info | 65-118 | Step 0 |
| Shared dirs | 130-134 | Step 1 |
| Product home | 136-139 | Step 2 |
| Env vars | 146-158 | Step 3 |
| Lock files | 167-168 | Step 4 |
| DNS lookups | 175-180 | Step 5 (if !quick) |
| TLS checks | 181-186 | Step 6 (if !quick) |
| Canary download | 194 | Step 7 (if !quick) |
| PyPI check | 195 | Step 8 (if !quick) |
| Conda check | 196 | Step 9 (if !quick) |

#### Benefits

1. **Live Feedback**: Users see progress during slow network checks
2. **Failure Identification**: Failed checks immediately visible
3. **Time Awareness**: Elapsed time shown for long-running diagnostics
4. **Better UX**: No more waiting with no feedback
5. **Debug Friendly**: Existing logging and journal entries preserved

#### Implementation Notes

- The dashboard should be created before checks start
- Each check group updates its step status upon completion
- Failures should set step to `pretty.StepFailed` with error message
- The existing result structure and JSON output remain unchanged
- Dashboard only affects interactive display, not data collection

---

## T129: Multi-Task Dashboard Integration

### Current State in `cloud/client.go`

The `Download()` function (lines 236-305) currently uses a progress bar:

```go
// Create progress bar if content length is known and interactive
var progressBar pretty.ProgressIndicator
contentLength := response.ContentLength
if contentLength > 0 && pretty.Interactive {
    progressBar = pretty.NewProgressBar(fmt.Sprintf("Downloading %s", filename), contentLength)
    progressBar.Start()
    defer progressBar.Stop(true)
}
```

This works well for **single downloads** but doesn't support **parallel operations**.

### Integration Opportunity

**Add** `MultiTaskDashboard` **for parallel download operations** (e.g., holotree catalog pulls).

#### Current Download Scenario

The `Download()` function is called from various places:
- Micromamba downloads (conda package)
- Holotree catalog pulls (htfs package)
- Remote resource downloads

Most of these are **sequential**, but catalog pulls could benefit from **parallel downloads** with a unified dashboard.

#### Proposed Integration for Parallel Downloads

For operations that perform multiple downloads (like pulling multiple catalog items):

```go
// In a hypothetical parallel downloader
func DownloadMultiple(urls []string, destDir string) error {
    // Create task list
    tasks := make([]string, len(urls))
    for i, url := range urls {
        tasks[i] = filepath.Base(url)
    }

    // NEW: Create multi-task dashboard
    dashboard := pretty.NewMultiTaskDashboard(tasks)
    dashboard.Start()
    defer dashboard.Stop(true)

    // Download in parallel with dashboard updates
    for i, url := range urls {
        go func(idx int, u string) {
            dashboard.SetStep(idx, pretty.StepRunning, "Downloading")
            err := Download(u, filepath.Join(destDir, tasks[idx]))
            if err != nil {
                dashboard.SetStep(idx, pretty.StepFailed, err.Error())
            } else {
                dashboard.SetStep(idx, pretty.StepComplete, "Complete")
            }
        }(i, url)
    }

    return nil
}
```

#### Dashboard Features

The `MultiTaskDashboard` (Layout C) provides:

1. **Parallel Task View**: All tasks visible simultaneously
2. **Per-Task Progress**: Individual progress bars for each download
3. **Overall Progress**: Summary of completed/remaining tasks
4. **Failure Tracking**: Failed tasks clearly marked

#### Benefits

1. **Parallel Visibility**: See all concurrent downloads
2. **Progress Awareness**: Know which downloads are complete
3. **Failure Identification**: Spot problematic downloads immediately
4. **Better UX**: No more wondering what's happening in parallel operations

#### Implementation Notes

- Currently, RCC mostly performs **sequential downloads**
- The multi-task dashboard is implemented but not yet integrated
- Best candidates for integration:
  - Holotree catalog synchronization (if made parallel in future)
  - Bulk asset downloads (if feature is added)
  - Parallel environment preparations
- The dashboard will show a fallback for sequential operations

---

## Summary of Integration Status

| Dashboard Type | Implementation | Integration | Status |
|----------------|----------------|-------------|--------|
| Environment Dashboard | ✅ Complete | 📋 Documented | Ready for integration in `htfs/commands.go` |
| Diagnostics Dashboard | ✅ Complete | 📋 Documented | Ready for integration in `operations/diagnostics.go` |
| Robot Run Dashboard | ✅ Complete | 📋 Documented | Ready for integration in `operations/running.go` |
| Multi-Task Dashboard | ✅ Complete | 📋 Documented | Awaiting parallel operation use cases |
| Compact Progress | ✅ Complete | ✅ Available | Use via `pretty.NewCompactProgress()` |
| Download Progress | ✅ Complete | ✅ Integrated | Already in `cloud.Download()` (line 274-279) |

---

## Build and Test Status

### Build Verification (T131)

**Command**: `GOARCH=amd64 go build -o build/ ./cmd/...`

**Result**: ✅ **Success** - Binary builds without errors

**Artifacts**:
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/build/rcc` (24 MB)
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/build/rccremote` (13 MB)

### Test Verification (T132)

**Command**: `GOARCH=amd64 go test ./pretty/... ./wizard/...`

**Result**: ✅ **Success** - All tests pass

**Details**:
- `pretty` package: All tests pass
- `wizard` package: All tests pass

**Fixed Issue**: Test file `wizard/actions_test.go` had incorrect function calls:
- Changed `ConfirmDangerous(prompt, confirmText, force)` (3 args)
- To `ConfirmDangerousWithText(prompt, confirmText, force)` (3 args)
- The 2-arg `ConfirmDangerous(question, force)` is a separate function

---

## Recommendations

### Immediate Next Steps

1. **Environment Dashboard** (High Priority)
   - Most visible improvement for users
   - Replace simple spinner with rich dashboard in `htfs/commands.go`
   - Minimal risk - graceful fallback already implemented

2. **Diagnostics Dashboard** (Medium Priority)
   - Improves troubleshooting experience
   - Easy integration in `operations/diagnostics.go`
   - Helps users understand what's being checked

3. **Robot Run Dashboard** (Medium Priority)
   - Valuable for interactive development
   - Requires more integration work in `operations/running.go`
   - Should respect `interactiveFlag` parameter

4. **Multi-Task Dashboard** (Low Priority - Future)
   - Awaits parallel operation scenarios
   - Currently RCC is mostly sequential
   - Good candidate for future optimization work

### Integration Guidelines

For each integration:

1. **Preserve Existing Behavior**
   - Keep all existing logging and journal calls
   - Don't break non-interactive mode
   - Maintain backward compatibility

2. **Use Dashboard Detection**
   - Let `ShouldUseDashboard()` decide whether to use dashboard
   - Don't manually check `pretty.Interactive` unless needed
   - Trust the `noopDashboard` fallback

3. **Maintain Testability**
   - Dashboards should not break unit tests
   - Use `noopDashboard` in test environments
   - Keep test coverage at current levels

4. **Follow Existing Patterns**
   - Use `defer dashboard.Stop()` for cleanup
   - Update steps as operations progress
   - Handle errors gracefully

---

## File Locations

**Dashboard Implementations**:
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/pretty/dashboard.go` - Core framework
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/pretty/dashboard_robot.go` - Robot dashboard (Layout F)
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/pretty/diagnostics_dashboard.go` - Diagnostics dashboard (Layout B)

**Integration Targets**:
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/htfs/commands.go` - Environment operations
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/operations/diagnostics.go` - Diagnostic checks
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/operations/running.go` - Robot execution
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/cloud/client.go` - Downloads

**Tests**:
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/pretty/dashboard_test.go`
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/pretty/diagnostics_dashboard_test.go`
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/wizard/actions_test.go` (fixed)
- `/var/home/kdlocpanda/second_brain/Projects/yorko-io/refactors/rcc/wizard/confirm_test.go`

---

**End of Document**
