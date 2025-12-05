package pretty

import (
	"fmt"
	"strings"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/dashcore"
)

// diagnosticsCheck represents a single diagnostic check with its status
type diagnosticsCheck struct {
	Name     string
	Category string     // Category name (e.g., "System Checks", "Network Checks")
	Status   StepStatus
	Message  string // Additional details for warnings/errors
}

// diagnosticsDashboard implements Layout B - checklist-style dashboard for diagnostic checks
type diagnosticsDashboard struct {
	baseDashboard
	checks         []diagnosticsCheck
	spinnerFrame   int
	version        string // RCC version to display in header
	dashboardLines int    // Number of lines the dashboard occupies
}

// Start begins the diagnostics dashboard rendering
func (d *diagnosticsDashboard) Start() {
	d.Mu.Lock()
	if d.Running {
		d.Mu.Unlock()
		return
	}
	d.Running = true
	d.StartTime = time.Now()
	d.Mu.Unlock()

	// Skip if not interactive
	if !Interactive {
		common.Trace("DiagnosticsDashboard skipped (non-interactive mode)")
		return
	}

	common.Trace("Starting diagnostics dashboard with %d checks", len(d.checks))

	// Setup signal handlers for graceful cleanup
	dashcore.SetupDashboardSignals(func() {
		d.cleanup()
	})

	// Calculate dashboard height based on number of checks and categories
	// Count unique categories for section headers
	categories := d.getCategories()
	// Header: 2 lines (top border with title + separator)
	// Content: category headers + checks + blank lines between categories
	// Footer: 2 lines (separator + summary)
	// Borders: included in above
	contentLines := len(d.checks) + len(categories) + (len(categories) - 1) // checks + headers + spacing
	d.dashboardLines = 2 + contentLines + 2

	// Hide cursor
	HideCursor()

	// Set scroll region: dashboard stays at top, output scrolls below
	termHeight := TerminalHeight()
	if d.dashboardLines < termHeight {
		SetScrollRegion(d.dashboardLines+1, termHeight)
		// Move cursor to the scroll region for any output that comes later
		MoveTo(d.dashboardLines+1, 1)
	}

	// Draw initial frame
	d.render()

	// Start render loop
	go d.StartRenderLoop(d.render)
}

// Stop ends the diagnostics dashboard and shows final state
func (d *diagnosticsDashboard) Stop(success bool) {
	d.Mu.Lock()
	if !d.Running {
		d.Mu.Unlock()
		return
	}
	d.Running = false
	d.Mu.Unlock()

	if !Interactive {
		return
	}

	common.Trace("Stopping diagnostics dashboard, success=%v", success)

	// Signal render loop to stop
	close(d.StopChan)

	// Wait for render loop to finish
	<-d.DoneChan

	// Render final state
	d.render()

	// Cleanup
	d.cleanup()

	// Log completion
	duration := time.Since(d.StartTime)
	statusMsg := ""
	if success {
		statusMsg = fmt.Sprintf("%s✓%s Diagnostics completed in %v", Green, Reset, duration)
		common.Trace("DiagnosticsDashboard completed successfully in %v", duration)
	} else {
		statusMsg = fmt.Sprintf("%s✗%s Diagnostics completed with errors in %v", Red, Reset, duration)
		common.Trace("DiagnosticsDashboard stopped with failures in %v", duration)
	}

	// Print final status message in the output area (below dashboard)
	common.Stdout("\n%s\n", statusMsg)
}

// Update updates the dashboard state (for generic state updates)
func (d *diagnosticsDashboard) Update(state DashboardState) {
	d.Mu.Lock()
	defer d.Mu.Unlock()

	// Update checks from state steps
	for i, step := range state.Steps {
		if i < len(d.checks) {
			d.checks[i].Status = step.Status
			d.checks[i].Message = step.Message
		}
	}
}

// SetStep updates a specific check's status and message
func (d *diagnosticsDashboard) SetStep(index int, status StepStatus, message string) {
	d.Mu.Lock()
	defer d.Mu.Unlock()

	if index >= 0 && index < len(d.checks) {
		d.checks[index].Status = status
		d.checks[index].Message = message
		common.Trace("Check %d (%s) updated: status=%d, message=%s", index, d.checks[index].Name, status, message)
	}
}

// AddOutput adds output to the dashboard (not used in diagnostics layout)
func (d *diagnosticsDashboard) AddOutput(line string) {
	// Diagnostics dashboard doesn't display output lines
}

// render draws the diagnostics dashboard
func (d *diagnosticsDashboard) render() {
	d.Mu.Lock()
	if !Interactive || !d.Running {
		d.Mu.Unlock()
		return
	}

	checks := make([]diagnosticsCheck, len(d.checks))
	copy(checks, d.checks)
	spinnerFrame := d.spinnerFrame
	d.spinnerFrame = (d.spinnerFrame + 1) % 10 // Cycle through 10 spinner frames
	version := d.version
	d.Mu.Unlock()

	// Save cursor position (which should be in the scroll region)
	SaveCursor()

	// Calculate dimensions
	termWidth := TerminalWidth()
	boxWidth := termWidth
	if boxWidth > 73 {
		boxWidth = 73 // Match spec width
	}
	if boxWidth < 40 {
		boxWidth = 40
	}

	// Position box at top of terminal
	boxX := 1
	boxY := 1

	// Get active box style
	style := ActiveBoxStyle()

	// Draw top border with title and version
	MoveTo(boxY, boxX)
	title := "RCC Diagnostics"
	versionText := version
	if versionText == "" {
		versionText = "v18.0.0" // fallback version
	}

	// Calculate spacing for right-aligned version
	titleLen := len(title)
	versionLen := len(versionText)
	spacing := boxWidth - titleLen - versionLen - 6 // 6 = borders + padding
	if spacing < 1 {
		spacing = 1
	}

	common.Stdout("%s  %s%s%s  %s",
		style.TopLeft,
		title,
		strings.Repeat(" ", spacing),
		versionText,
		style.TopRight)

	// Draw separator line
	MoveTo(boxY+1, boxX)
	common.Stdout("%s%s%s",
		style.LeftT,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.RightT)

	// Group checks by category and render
	currentRow := boxY + 2
	categories := d.getCategories()

	for catIdx, category := range categories {
		// Draw category header
		MoveTo(currentRow, boxX+1)
		common.Stdout("%s  %s%s%s%s",
			style.Vertical,
			Bold,
			category,
			Reset,
			strings.Repeat(" ", boxWidth-len(category)-4))
		currentRow++

		// Draw checks in this category
		for _, check := range checks {
			if check.Category == category {
				MoveTo(currentRow, boxX+1)

				// Get status icon and color
				icon, color := d.getCheckIconAndColor(check.Status, spinnerFrame)

				// Format check line with proper padding
				checkName := check.Name
				detail := check.Message

				// Build the line: "    {icon} {name}         {detail}"
				// Calculate available width for detail
				// Length without color codes
				displayLen := 4 + 2 + len(checkName) // "    " + icon + " " + name

				detailPadding := boxWidth - displayLen - len(detail) - 4 // account for borders and right padding
				if detailPadding < 2 {
					detailPadding = 2
				}

				// Truncate detail if too long
				maxDetailWidth := boxWidth - displayLen - 6
				if len(detail) > maxDetailWidth && maxDetailWidth > 3 {
					detail = detail[:maxDetailWidth-3] + "..."
				}

				line := fmt.Sprintf("%s    %s%s%s %s%s%s",
					style.Vertical,
					color, icon, Reset,
					checkName,
					strings.Repeat(" ", detailPadding),
					detail)

				// Ensure line ends with border
				common.Stdout("%s%s", line, strings.Repeat(" ", boxWidth-2-displayLen-len(detail)))
				MoveTo(currentRow, boxX+boxWidth-1)
				common.Stdout("%s", style.Vertical)

				currentRow++
			}
		}

		// Add blank line between categories (except after last category)
		if catIdx < len(categories)-1 {
			MoveTo(currentRow, boxX)
			common.Stdout("%s%s%s",
				style.Vertical,
				strings.Repeat(" ", boxWidth-2),
				style.Vertical)
			currentRow++
		}
	}

	// Draw separator before footer
	MoveTo(currentRow, boxX)
	common.Stdout("%s%s%s",
		style.LeftT,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.RightT)
	currentRow++

	// Draw footer with summary (matching spec format)
	MoveTo(currentRow, boxX+1)
	summary := d.getSummaryLine(checks)
	// Pad the summary line
	summaryDisplay := summary
	if len(summaryDisplay) < boxWidth-3 {
		summaryDisplay += strings.Repeat(" ", boxWidth-3-len(summaryDisplay))
	}
	common.Stdout("%s  %s %s", style.Vertical, summaryDisplay, style.Vertical)
	currentRow++

	// Draw bottom border
	MoveTo(currentRow, boxX)
	common.Stdout("%s%s%s",
		style.BottomLeft,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.BottomRight)

	// Restore cursor position (back to scroll region)
	RestoreCursor()
}

// getCheckIconAndColor returns the appropriate icon and color for a check status
func (d *diagnosticsDashboard) getCheckIconAndColor(status StepStatus, spinnerFrame int) (string, string) {
	// Spinner frames for running status
	spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	asciiSpinnerFrames := []string{"|", "/", "-", "\\"}

	switch status {
	case StepComplete:
		if dashcore.Iconic {
			return "✓", Green
		}
		return "+", Green
	case StepFailed:
		if dashcore.Iconic {
			return "✗", Red
		}
		return "x", Red
	case StepSkipped:
		// Use warning symbol for skipped (yellow)
		if dashcore.Iconic {
			return "⚠", Yellow
		}
		return "!", Yellow
	case StepRunning:
		// Animated spinner
		if dashcore.Iconic {
			frame := spinnerFrame % len(spinnerFrames)
			return spinnerFrames[frame], Cyan
		}
		frame := spinnerFrame % len(asciiSpinnerFrames)
		return asciiSpinnerFrames[frame], Cyan
	case StepPending:
		fallthrough
	default:
		if dashcore.Iconic {
			return "○", Grey
		}
		return "o", Grey
	}
}

// getSummary generates the footer summary line with counts (old format)
func (d *diagnosticsDashboard) getSummary(checks []diagnosticsCheck) string {
	var pass, warn, fail, pending int

	for _, check := range checks {
		switch check.Status {
		case StepComplete:
			pass++
		case StepFailed:
			fail++
		case StepSkipped:
			warn++
		case StepPending:
			pending++
		case StepRunning:
			pending++
		}
	}

	// Build colored summary
	parts := []string{}

	if pass > 0 {
		parts = append(parts, fmt.Sprintf("%sPass: %d%s", Green, pass, Reset))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%sWarn: %d%s", Yellow, warn, Reset))
	}
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%sFail: %d%s", Red, fail, Reset))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%sPending: %d%s", Grey, pending, Reset))
	}

	return strings.Join(parts, "  ")
}

// getSummaryLine generates the footer summary matching the spec format:
// "Progress: 5/12 checks │ Passed: 5 │ Failed: 0 │ Warnings: 0"
func (d *diagnosticsDashboard) getSummaryLine(checks []diagnosticsCheck) string {
	var completed, passed, failed, warnings int
	total := len(checks)

	for _, check := range checks {
		switch check.Status {
		case StepComplete:
			passed++
			completed++
		case StepFailed:
			failed++
			completed++
		case StepSkipped:
			warnings++
			completed++
		case StepRunning:
			// Running counts as in-progress, not completed
		case StepPending:
			// Pending doesn't count as completed
		}
	}

	// Use Unicode vertical bar if iconic, otherwise pipe character
	separator := " | "
	if dashcore.Iconic {
		separator = " │ "
	}

	return fmt.Sprintf("Progress: %d/%d checks%sPassed: %d%sFailed: %d%sWarnings: %d",
		completed, total, separator, passed, separator, failed, separator, warnings)
}

// getCategories returns the unique list of categories in the order they appear
func (d *diagnosticsDashboard) getCategories() []string {
	seen := make(map[string]bool)
	var categories []string

	for _, check := range d.checks {
		if check.Category != "" && !seen[check.Category] {
			categories = append(categories, check.Category)
			seen[check.Category] = true
		}
	}

	return categories
}

// cleanup restores terminal state
func (d *diagnosticsDashboard) cleanup() {
	if !Interactive {
		return
	}
	ClearScrollRegion()
	ShowCursor()
}
