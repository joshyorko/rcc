package pretty

import (
	"fmt"
	"strings"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/dashcore"
)

// RobotRunDashboard displays real-time robot execution status (Layout F)
// Shows robot name, task name, status, duration, scrolling output, and task counts
//
// Layout:
// ╭─────────────────────────────────────────────────────────────────╮
// │ 🤖 Robot: my-automation | Task: Main Task                       │
// │ Status: Running | Duration: 1m23s                               │
// ├─────────────────────────────────────────────────────────────────┤
// │ [Scrolling output region - last 10 lines]                       │
// │ > Processing item 42 of 100...                                  │
// │ > API response: 200 OK                                          │
// │ > Writing to database...                                        │
// ├─────────────────────────────────────────────────────────────────┤
// │ Tasks: 3/5 | Pass: 2 | Fail: 1 | Skip: 0                        │
// ╰─────────────────────────────────────────────────────────────────┘
type RobotRunDashboard struct {
	baseDashboard
	robotName    string
	taskName     string
	status       string // "Starting", "Running", "Complete", "Failed"
	outputLines  []string
	maxLines     int // Maximum output lines to display (default: 10)
	totalTasks   int
	passedTasks  int
	failedTasks  int
	skippedTasks int
	width        int
	height       int
}

// Start begins the robot run dashboard display
func (r *RobotRunDashboard) Start() {
	r.Mu.Lock()
	if r.Running {
		r.Mu.Unlock()
		return
	}
	r.Running = true
	r.StartTime = time.Now()
	r.Mu.Unlock()

	// Skip if not interactive
	if !Interactive {
		common.Trace("RobotRunDashboard skipped (non-interactive mode)")
		return
	}

	// Mark dashboard as active to suppress verbose output
	dashcore.SetDashboardActive(true)

	common.Trace("RobotRunDashboard starting for robot: %s, task: %s", r.robotName, r.taskName)

	// Setup signal handlers for cleanup
	dashcore.SetupDashboardSignals(func() {
		r.cleanup()
	})

	// Hide cursor
	HideCursor()

	// Calculate dashboard height (header + output lines + footer + borders)
	dashboardLines := 3 + r.maxLines + 3 // header(2) + sep(1) + output + sep(1) + footer(1) + bottom(1)
	termHeight := TerminalHeight()

	// Set scroll region: dashboard stays at top, robot output scrolls below
	SetScrollRegion(dashboardLines+1, termHeight)

	// Move cursor to the scroll region for robot output
	MoveTo(dashboardLines+1, 1)

	// Draw initial frame
	r.render()

	// Start render loop in background
	go r.baseDashboard.StartRenderLoop(r.render)
}

// Stop terminates the dashboard and shows final status
func (r *RobotRunDashboard) Stop(success bool) {
	r.Mu.Lock()
	if !r.Running {
		r.Mu.Unlock()
		return
	}
	r.Running = false

	// Update final status
	if success {
		r.status = "Complete"
	} else {
		r.status = "Failed"
	}
	r.Mu.Unlock()

	// Mark dashboard as inactive
	dashcore.SetDashboardActive(false)

	if !Interactive {
		return
	}

	// Signal render loop to stop
	close(r.StopChan)
	<-r.DoneChan

	// Render final state
	r.render()

	// Clean up terminal state
	r.cleanup()

	// Log completion
	duration := time.Since(r.StartTime)
	if success {
		icon := "+"
		if Iconic {
			icon = "✓"
		}
		common.Stdout("\n%s%s%s Robot execution completed in %v%s\n",
			Green, icon, Reset, duration, Reset)
		common.Trace("RobotRunDashboard completed successfully in %v", duration)
	} else {
		icon := "x"
		if Iconic {
			icon = "✗"
		}
		common.Stdout("\n%s%s%s Robot execution failed after %v%s\n",
			Red, icon, Reset, duration, Reset)
		common.Trace("RobotRunDashboard stopped with failures in %v", duration)
	}
}

// Update updates the dashboard state (for compatibility with Dashboard interface)
func (r *RobotRunDashboard) Update(state DashboardState) {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	r.State = state

	// Update status from message if provided
	if state.Message != "" {
		r.status = state.Message
	}
}

// SetStep updates task counts based on step status
func (r *RobotRunDashboard) SetStep(index int, status StepStatus, message string) {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// Update task counts based on status changes
	switch status {
	case StepComplete:
		r.passedTasks++
	case StepFailed:
		r.failedTasks++
	case StepSkipped:
		r.skippedTasks++
	}

	if message != "" {
		r.status = message
	}

	common.Trace("RobotRunDashboard task updated: Pass=%d, Fail=%d, Skip=%d",
		r.passedTasks, r.failedTasks, r.skippedTasks)
}

// AddOutput adds a line to the scrolling output region
// Maintains a circular buffer of the last N lines
func (r *RobotRunDashboard) AddOutput(line string) {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// Trim whitespace and skip empty lines
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	// Add line to buffer
	r.outputLines = append(r.outputLines, line)

	// Keep only the last maxLines
	if len(r.outputLines) > r.maxLines {
		r.outputLines = r.outputLines[len(r.outputLines)-r.maxLines:]
	}
}

// render draws the complete dashboard frame
func (r *RobotRunDashboard) render() {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	if !Interactive || !r.Running {
		return
	}

	// Save cursor position (which should be in the scroll region)
	SaveCursor()

	// Calculate layout dimensions
	boxWidth := r.width
	if boxWidth > 67 {
		boxWidth = 67 // Standard width for readability
	}

	style := BoxRounded // Use rounded corners as specified

	// Calculate duration
	duration := time.Since(r.StartTime)
	durationStr := formatRobotDuration(duration)

	// === Draw Header (2 lines) ===

	// Line 1: Top border with robot and task name
	MoveTo(1, 1)
	robotIcon := "Robot"
	if Iconic {
		robotIcon = "🤖 Robot"
	}

	headerLine1 := fmt.Sprintf("%s: %s | Task: %s", robotIcon, r.robotName, r.taskName)
	// Truncate if too long
	maxHeaderWidth := boxWidth - 4
	if len(headerLine1) > maxHeaderWidth {
		headerLine1 = headerLine1[:maxHeaderWidth-3] + "..."
	}

	padding1 := boxWidth - len(headerLine1) - 2
	if padding1 < 0 {
		padding1 = 0
	}

	common.Stdout("%s %s%s %s",
		style.TopLeft,
		headerLine1,
		strings.Repeat(" ", padding1),
		style.TopRight)

	// Line 2: Status and duration
	MoveTo(2, 1)
	statusColor := Grey
	switch r.status {
	case "Running":
		statusColor = Cyan
	case "Complete":
		statusColor = Green
	case "Failed":
		statusColor = Red
	default:
		statusColor = Yellow
	}

	headerLine2 := fmt.Sprintf("Status: %s%s%s | Duration: %s",
		statusColor, r.status, Reset, durationStr)

	// Calculate visible length (without ANSI codes)
	visibleLen2 := len(fmt.Sprintf("Status: %s | Duration: %s", r.status, durationStr))
	padding2 := boxWidth - visibleLen2 - 2
	if padding2 < 0 {
		padding2 = 0
	}

	common.Stdout("%s %s%s %s",
		style.Vertical,
		headerLine2,
		strings.Repeat(" ", padding2),
		style.Vertical)

	// === Draw separator ===
	MoveTo(3, 1)
	common.Stdout("%s%s%s",
		style.LeftT,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.RightT)

	// === Draw output region (maxLines) ===
	outputStartLine := 4
	for i := 0; i < r.maxLines; i++ {
		MoveTo(outputStartLine+i, 1)

		var lineContent string
		if i < len(r.outputLines) {
			line := r.outputLines[i]
			// Truncate line if too long
			maxLineWidth := boxWidth - 6 // Account for borders and "> "
			if len(line) > maxLineWidth {
				line = line[:maxLineWidth-3] + "..."
			}
			lineContent = fmt.Sprintf("> %s", line)
		} else {
			lineContent = ""
		}

		// Pad to full width
		displayLen := len(lineContent)
		padding := boxWidth - displayLen - 2
		if padding < 0 {
			padding = 0
		}

		common.Stdout("%s %s%s %s",
			style.Vertical,
			lineContent,
			strings.Repeat(" ", padding),
			style.Vertical)
	}

	// === Draw separator before footer ===
	footerLine := outputStartLine + r.maxLines
	MoveTo(footerLine, 1)
	common.Stdout("%s%s%s",
		style.LeftT,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.RightT)

	// === Draw footer with task counts ===
	MoveTo(footerLine+1, 1)
	footerContent := fmt.Sprintf("Tasks: %d/%d | %sPass: %d%s | %sFail: %d%s | %sSkip: %d%s",
		r.passedTasks+r.failedTasks+r.skippedTasks,
		r.totalTasks,
		Green, r.passedTasks, Reset,
		Red, r.failedTasks, Reset,
		Grey, r.skippedTasks, Reset)

	// Calculate visible length
	visibleFooterLen := len(fmt.Sprintf("Tasks: %d/%d | Pass: %d | Fail: %d | Skip: %d",
		r.passedTasks+r.failedTasks+r.skippedTasks,
		r.totalTasks,
		r.passedTasks,
		r.failedTasks,
		r.skippedTasks))

	footerPadding := boxWidth - visibleFooterLen - 2
	if footerPadding < 0 {
		footerPadding = 0
	}

	common.Stdout("%s %s%s %s",
		style.Vertical,
		footerContent,
		strings.Repeat(" ", footerPadding),
		style.Vertical)

	// === Draw bottom border ===
	MoveTo(footerLine+2, 1)
	common.Stdout("%s%s%s",
		style.BottomLeft,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.BottomRight)

	// Restore cursor position (back to scroll region for robot output)
	RestoreCursor()
}

// cleanup restores terminal state
func (r *RobotRunDashboard) cleanup() {
	if !Interactive {
		return
	}
	ClearScrollRegion()
	ShowCursor()
}

// formatRobotDuration formats a duration as human-readable string
func formatRobotDuration(d time.Duration) string {
	seconds := int(d.Seconds())

	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}

	minutes := seconds / 60
	remainingSeconds := seconds % 60

	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, remainingSeconds)
	}

	hours := minutes / 60
	remainingMinutes := minutes % 60
	return fmt.Sprintf("%dh%dm", hours, remainingMinutes)
}
