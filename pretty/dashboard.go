package pretty

// Dashboard framework for RCC's UI enhancements (Phase 11: T079-T084)
//
// This package provides the core dashboard system with interface and common functionality
// for displaying interactive, real-time progress information in the terminal.
//
// Architecture:
// - Dashboard interface: Common contract for all dashboard implementations
// - StepStatus: Enumeration for step states with visual representations
// - DashboardState: Shared state structure across dashboard types
// - baseDashboard: Common fields and functionality for dashboard implementations
// - Factory functions: Create type-specific dashboards (stubs for now, implemented in later phases)
//
// Dashboard Detection:
// Dashboards are enabled when:
// - Interactive mode is active (stdin/stdout/stderr are TTY)
// - Terminal height >= 20 lines (minimum for useful dashboard display)
// - RCC_NO_DASHBOARD environment variable is NOT set
//
// Signal Handling:
// All dashboards register SIGINT/SIGTERM handlers for graceful cleanup:
// - Restore scroll region to full screen
// - Show cursor (unhide)
// - Clean up any rendering state
//
// Render Loop:
// Dashboards use a 50ms update cycle (20 fps) for smooth animations,
// checking stopChan for termination signals between frames.

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joshyorko/rcc/common"
)

// activeDashboard tracks if a dashboard is currently active
// This is used to suppress redundant output from pretty.Progress() etc.
var activeDashboard atomic.Int32

// keepDashboardAlive tracks if the dashboard should remain active after Stop()
var keepDashboardAlive atomic.Bool

// SetKeepDashboardAlive sets whether the dashboard should remain active after Stop()
func SetKeepDashboardAlive(keep bool) {
	keepDashboardAlive.Store(keep)
}

// IsKeepDashboardAlive returns true if the dashboard should remain active
func IsKeepDashboardAlive() bool {
	return keepDashboardAlive.Load()
}

// IsDashboardActive returns true if any dashboard is currently rendering
func IsDashboardActive() bool {
	return activeDashboard.Load() > 0
}

// setDashboardActive increments or decrements the active dashboard counter
func setDashboardActive(active bool) {
	if active {
		activeDashboard.Add(1)
	} else {
		activeDashboard.Add(-1)
	}
}

// Dashboard interface defines the contract for interactive dashboard displays
type Dashboard interface {
	Start()
	Stop(success bool)
	Update(state DashboardState)
	SetStep(index int, status StepStatus, message string)
	AddOutput(line string)
}

// StepStatus represents the current state of a dashboard step
type StepStatus int

const (
	StepPending StepStatus = iota
	StepRunning
	StepComplete
	StepFailed
	StepSkipped
)

// String returns the visual representation of a step status
func (s StepStatus) String() string {
	if Iconic {
		switch s {
		case StepPending:
			return "○"
		case StepRunning:
			return "⠋" // Spinner frame (will be animated in implementations)
		case StepComplete:
			return "✓"
		case StepFailed:
			return "✗"
		case StepSkipped:
			return "⊘"
		default:
			return "○"
		}
	}

	// ASCII fallback
	switch s {
	case StepPending:
		return "o"
	case StepRunning:
		return "-"
	case StepComplete:
		return "+"
	case StepFailed:
		return "x"
	case StepSkipped:
		return "/"
	default:
		return "o"
	}
}

// StepState represents the state of a single step in a dashboard
type StepState struct {
	Index   int
	Status  StepStatus
	Message string
}

// DashboardState holds the common state shared across dashboard implementations
type DashboardState struct {
	Steps     []StepState
	Progress  float64 // 0.0 to 1.0
	Message   string
	StartTime time.Time
	Output    []string // Recent output lines
}

// baseDashboard provides common functionality for dashboard implementations
type baseDashboard struct {
	running   bool
	mu        sync.Mutex
	stopChan  chan struct{}
	doneChan  chan struct{}
	startTime time.Time
	state     DashboardState
}

// newBaseDashboard creates a new base dashboard with initialized channels
func newBaseDashboard() baseDashboard {
	return baseDashboard{
		running:   false,
		stopChan:  make(chan struct{}),
		doneChan:  make(chan struct{}),
		startTime: time.Now(),
		state: DashboardState{
			StartTime: time.Now(),
			Steps:     []StepState{},
			Output:    []string{},
		},
	}
}

// setupDashboardSignals registers signal handlers for graceful shutdown
// This ensures the dashboard cleans up properly on Ctrl+C or termination
func setupDashboardSignals(cleanup func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		common.Trace("Interrupt signal received, cleaning up dashboard")
		cleanup()
		os.Exit(1)
	}()
}

// cleanupDashboard performs common cleanup operations
// Restores scroll region and cursor visibility
func cleanupDashboard() {
	ClearScrollRegion()
	ShowCursor()
}

// startRenderLoop begins the dashboard render loop at 20fps (50ms cycle)
func (b *baseDashboard) startRenderLoop(renderFunc func()) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopChan:
			close(b.doneChan)
			return
		case <-ticker.C:
			renderFunc()
		}
	}
}

// ShouldUseDashboard determines if dashboards should be enabled
// Returns true only if:
// - Interactive mode is enabled (stdin/stdout/stderr are TTY)
// - Terminal height is >= 20 lines
// - RCC_NO_DASHBOARD environment variable is NOT set
// - Not running with a non-user controller (e.g., citests, cloud)
func ShouldUseDashboard() bool {
	if !Interactive {
		common.Trace("Dashboard disabled: not in interactive mode")
		return false
	}

	if os.Getenv("RCC_NO_DASHBOARD") != "" {
		common.Trace("Dashboard disabled: RCC_NO_DASHBOARD is set")
		return false
	}

	// Disable dashboards when running with a controller that's not "user"
	// This prevents Bubble Tea alt-screen mode from interfering with CI test output capture
	controller := common.ControllerType
	if controller != "" && controller != "user" {
		common.Trace("Dashboard disabled: running with controller %q", controller)
		return false
	}

	height := TerminalHeight()
	if height < 20 {
		common.Trace("Dashboard disabled: terminal height %d < 20", height)
		return false
	}

	common.Trace("Dashboard enabled: interactive mode, height=%d", height)
	return true
}

// noopDashboard is a no-op implementation for when dashboards are disabled
type noopDashboard struct{}

func (n *noopDashboard) Start()                                               {}
func (n *noopDashboard) Stop(success bool)                                    {}
func (n *noopDashboard) Update(state DashboardState)                          {}
func (n *noopDashboard) SetStep(index int, status StepStatus, message string) {}
func (n *noopDashboard) AddOutput(line string)                                {}

// NewNoopDashboard returns a no-op dashboard implementation that does nothing.
// Use this when you need a Dashboard interface but don't want any visual output.
func NewNoopDashboard() Dashboard {
	return &noopDashboard{}
}

// Factory functions for dashboard layouts
// These will be implemented in subsequent phases

// NewEnvironmentDashboard creates a dashboard for environment setup operations (Layout A)
// Shows sequential steps for environment creation and dependency installation
// Uses Bubble Tea for a clean, modern UI that completely owns the terminal
func NewEnvironmentDashboard(steps []string) Dashboard {
	common.Trace("NewEnvironmentDashboard called with %d steps", len(steps))

	if !ShouldUseDashboard() {
		common.Trace("Dashboard disabled, returning noop")
		return &noopDashboard{}
	}

	// Use Unified Dashboard for seamless experience
	unifiedDashboard := NewUnifiedDashboard(steps)
	if unifiedDashboard != nil {
		common.Trace("Using Unified dashboard with %d steps", len(steps))
		return unifiedDashboard
	}

	// Fallback to Bubble Tea dashboard if Unified fails (shouldn't happen if NewUnifiedDashboard works)
	teaDashboard := NewTeaEnvironmentDashboard(steps)
	if teaDashboard != nil {
		common.Trace("Using Bubble Tea dashboard with %d steps", len(steps))
		return teaDashboard
	}

	// Fallback to noop if Bubble Tea fails to initialize
	common.Trace("Bubble Tea init failed, returning noop")
	return &noopDashboard{}
}

// NewDiagnosticsDashboard creates a dashboard for diagnostics operations (Layout B)
// Shows parallel checks with real-time status updates
func NewDiagnosticsDashboard(checks []string) Dashboard {
	common.Trace("NewDiagnosticsDashboard called with %d checks", len(checks))

	if !ShouldUseDashboard() {
		common.Trace("Dashboard conditions not met, returning noop dashboard")
		return &noopDashboard{}
	}

	// Initialize dashboard with checks
	dashboard := &diagnosticsDashboard{
		baseDashboard: newBaseDashboard(),
		checks:        make([]diagnosticsCheck, len(checks)),
		version:       "v18.0.0", // TODO: Get actual RCC version
	}

	// Initialize all checks as pending
	// Parse check names to extract category (if format is "Category:Check Name")
	for i, name := range checks {
		category := ""
		checkName := name

		// Check if name contains category separator
		if idx := strings.Index(name, ":"); idx != -1 {
			category = name[:idx]
			checkName = name[idx+1:]
		}

		dashboard.checks[i] = diagnosticsCheck{
			Name:     checkName,
			Category: category,
			Status:   StepPending,
		}
	}

	common.Trace("DiagnosticsDashboard created with %d checks", len(checks))
	return dashboard
}

// NewDownloadDashboard creates a dashboard for download operations (Layout C)
// Shows progress bar with transfer rate and estimated time
func NewDownloadDashboard(filename string, total int64) Dashboard {
	common.Trace("NewDownloadDashboard called for %s, %d bytes", filename, total)

	if !ShouldUseDashboard() {
		common.Trace("Dashboard conditions not met, returning noop dashboard")
		return &noopDashboard{}
	}

	dashboard := &DownloadDashboard{
		baseDashboard: newBaseDashboard(),
		filename:      filename,
		total:         total,
		current:       0,
		speed:         0,
		lastUpdate:    time.Now(),
		lastBytes:     0,
		speedSamples:  make([]float64, 0, 5),
	}

	common.Trace("DownloadDashboard created for %s (%d bytes)", filename, total)
	return dashboard
}

// NewMultiTaskDashboard creates a dashboard for parallel task execution (Layout D)
// Shows multiple concurrent operations with individual progress
func NewMultiTaskDashboard(tasks []string) Dashboard {
	common.Trace("NewMultiTaskDashboard called with %d tasks (stub implementation)", len(tasks))
	// TODO: Implement in Phase 15 (T103-T108)
	return &noopDashboard{}
}


// NewCompactProgress creates a minimal progress indicator (Layout E)
// Single-line display for simple operations
func NewCompactProgress(message string) Dashboard {
	common.Trace("NewCompactProgress called with message: %s", message)
	return &CompactProgress{
		baseDashboard: newBaseDashboard(),
		message:       message,
		currentStep:   0,
		totalSteps:    0,
		progress:      0.0,
		status:        StepRunning,
		spinnerIdx:    0,
	}
}

// NewRobotRunDashboard creates a dashboard for robot execution (Layout F)
// Shows test suite progress with pass/fail/skip counts
// Uses Bubble Tea for a clean, modern UI that completely owns the terminal
func NewRobotRunDashboard(robotName string) Dashboard {
	common.Trace("NewRobotRunDashboard called for %s", robotName)

	if !ShouldUseDashboard() {
		common.Trace("Dashboard conditions not met, returning noop dashboard")
		return &noopDashboard{}
	}

	// Use Bubble Tea dashboard for modern UI with alt-screen mode
	teaDashboard := NewTeaRobotDashboard(robotName)
	if teaDashboard != nil {
		common.Trace("Using Bubble Tea robot dashboard for robot: %s", robotName)
		return teaDashboard
	}

	// Fallback to noop if Bubble Tea fails to initialize
	common.Trace("Bubble Tea init failed, returning noop")
	return &noopDashboard{}
}

// DownloadDashboard implementation (Layout C)
// Shows enhanced single-file download visualization with progress bar, speed, and ETA

// DownloadDashboard displays download progress with transfer rate and estimated time
type DownloadDashboard struct {
	baseDashboard
	filename     string
	total        int64
	current      int64
	speed        float64 // bytes per second (smoothed)
	lastUpdate   time.Time
	lastBytes    int64
	speedSamples []float64 // Rolling average for smooth speed display
}

// Start begins the download dashboard display
func (d *DownloadDashboard) Start() {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.startTime = time.Now()
	d.lastUpdate = time.Now()
	d.mu.Unlock()

	common.Trace("Starting DownloadDashboard for %s", d.filename)

	// Setup signal handlers for cleanup
	setupDashboardSignals(func() {
		d.cleanup()
	})

	// Hide cursor and start render loop
	HideCursor()

	go d.startRenderLoop(d.render)
}

// Stop stops the download dashboard and shows final status
func (d *DownloadDashboard) Stop(success bool) {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	d.mu.Unlock()

	common.Trace("Stopping DownloadDashboard with success=%v", success)

	// Stop render loop
	close(d.stopChan)
	<-d.doneChan

	// Cleanup and show final message
	d.cleanup()

	// Show completion message
	if success {
		if Iconic {
			common.Stdout("%s✓%s Download complete: %s (%s)\n", Green, Reset, d.filename, formatBytes(d.total))
		} else {
			common.Stdout("%s[OK]%s Download complete: %s (%s)\n", Green, Reset, d.filename, formatBytes(d.total))
		}
	} else {
		if Iconic {
			common.Stdout("%s✗%s Download failed: %s\n", Red, Reset, d.filename)
		} else {
			common.Stdout("%s[FAIL]%s Download failed: %s\n", Red, Reset, d.filename)
		}
	}
}

// Update updates the download progress
func (d *DownloadDashboard) Update(state DashboardState) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Use Progress field to calculate current bytes (0.0 to 1.0)
	if state.Progress >= 0 && state.Progress <= 1.0 {
		d.current = int64(float64(d.total) * state.Progress)
	}

	// Calculate speed with rolling average
	now := time.Now()
	elapsed := now.Sub(d.lastUpdate).Seconds()

	if elapsed > 0.1 { // Update speed every 100ms minimum
		bytesDelta := d.current - d.lastBytes
		instantSpeed := float64(bytesDelta) / elapsed

		// Add to rolling average
		d.speedSamples = append(d.speedSamples, instantSpeed)
		if len(d.speedSamples) > 5 {
			d.speedSamples = d.speedSamples[1:]
		}

		// Calculate smoothed speed
		sum := 0.0
		for _, s := range d.speedSamples {
			sum += s
		}
		d.speed = sum / float64(len(d.speedSamples))

		d.lastUpdate = now
		d.lastBytes = d.current
	}

	common.Trace("DownloadDashboard updated: %d/%d bytes, %.2f bytes/sec", d.current, d.total, d.speed)
}

// SetStep is a no-op for download dashboard (not applicable)
func (d *DownloadDashboard) SetStep(index int, status StepStatus, message string) {
	// Not used for download dashboard
}

// AddOutput is a no-op for download dashboard (not applicable)
func (d *DownloadDashboard) AddOutput(line string) {
	// Not used for download dashboard
}

// render draws the download dashboard
func (d *DownloadDashboard) render() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Get terminal dimensions
	width := getTerminalWidth()
	if width < 40 {
		width = 40 // Minimum width
	}
	if width > 80 {
		width = 80 // Maximum width for readability
	}

	style := ActiveBoxStyle()

	// Calculate box dimensions
	boxWidth := width - 2
	boxHeight := 6

	// Clear area and draw box with title
	ClearLine()
	DrawBoxWithTitle(1, 1, boxWidth, boxHeight, "Downloading: "+d.filename, style)

	// Calculate progress percentage
	percentage := 0.0
	if d.total > 0 {
		percentage = float64(d.current) / float64(d.total) * 100.0
		if percentage > 100.0 {
			percentage = 100.0
		}
	}

	// Draw progress bar (line 2)
	progressBarWidth := boxWidth - 4 // Account for borders and padding
	filledWidth := int(float64(progressBarWidth) * percentage / 100.0)

	progressBar := ""
	if Iconic {
		// Unicode progress bar
		for i := 0; i < progressBarWidth; i++ {
			if i < filledWidth {
				progressBar += "█"
			} else {
				progressBar += "░"
			}
		}
	} else {
		// ASCII progress bar
		for i := 0; i < progressBarWidth; i++ {
			if i < filledWidth {
				progressBar += "="
			} else {
				progressBar += " "
			}
		}
	}

	MoveTo(3, 3)
	common.Stdout("[%s] %3.0f%%", progressBar, percentage)

	// Draw size information (line 3)
	MoveTo(4, 3)
	common.Stdout("%s / %s", formatBytes(d.current), formatBytes(d.total))

	// Draw speed and ETA (line 4)
	MoveTo(5, 3)
	speedStr := formatSpeed(d.speed)
	etaStr := formatETA(d.calculateETA())
	common.Stdout("Speed: %s   ETA: %s", speedStr, etaStr)
}

// cleanup restores terminal state
func (d *DownloadDashboard) cleanup() {
	ClearLine()
	ShowCursor()
}

// calculateETA calculates estimated time remaining in seconds
func (d *DownloadDashboard) calculateETA() int {
	if d.speed <= 0 || d.current >= d.total {
		return 0
	}

	remaining := d.total - d.current
	seconds := float64(remaining) / d.speed

	return int(seconds)
}

// formatBytes formats byte count as human-readable string (KB, MB, GB)
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	if bytes >= GB {
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	} else if bytes >= MB {
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	} else if bytes >= KB {
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	}
	return fmt.Sprintf("%d B", bytes)
}

// formatSpeed formats bytes per second as human-readable string
func formatSpeed(bytesPerSec float64) string {
	const (
		KB = 1024.0
		MB = 1024.0 * KB
		GB = 1024.0 * MB
	)

	if bytesPerSec >= GB {
		return fmt.Sprintf("%.1f GB/s", bytesPerSec/GB)
	} else if bytesPerSec >= MB {
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/MB)
	} else if bytesPerSec >= KB {
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/KB)
	}
	return fmt.Sprintf("%.0f B/s", bytesPerSec)
}

// formatETA formats seconds as human-readable time estimate
func formatETA(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, secs)
	}
	return fmt.Sprintf("%ds", secs)
}

// CompactProgress implements a minimal single-line progress indicator (Layout E)
// This is used as a fallback for small terminals or simple operations
// Format: ⠋ Building environment... (Step 3/15) [42%]
type CompactProgress struct {
	baseDashboard
	message     string
	currentStep int
	totalSteps  int
	progress    float64
	status      StepStatus
	spinnerIdx  int
}

// spinnerFrames returns the spinner animation frames
func (c *CompactProgress) spinnerFrames() []string {
	if Iconic {
		return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	// ASCII fallback
	return []string{"|", "/", "-", "\\"}
}

// Start begins the compact progress indicator
func (c *CompactProgress) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	// Skip if not interactive
	if !Interactive {
		common.Trace("CompactProgress skipped (non-interactive mode): %s", c.message)
		common.Stdout("%s\n", c.message)
		return
	}

	common.Trace("Starting CompactProgress: %s", c.message)

	// Setup signal handler for cleanup
	setupDashboardSignals(func() {
		c.cleanup()
	})

	// Hide cursor for cleaner animation
	HideCursor()

	// Start render loop
	go c.startRenderLoop(c.render)
}

// Stop stops the compact progress and shows final status
func (c *CompactProgress) Stop(success bool) {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	common.Trace("Stopping CompactProgress with success=%v: %s", success, c.message)

	if !Interactive {
		return
	}

	// Signal render loop to stop
	close(c.stopChan)
	<-c.doneChan

	// Show final status
	c.renderFinal(success)
	c.cleanup()
}

// Update updates the progress state
func (c *CompactProgress) Update(state DashboardState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.progress = state.Progress
	if state.Message != "" {
		c.message = state.Message
	}
	if len(state.Steps) > 0 {
		c.totalSteps = len(state.Steps)
		// Count completed steps
		completed := 0
		for _, step := range state.Steps {
			if step.Status == StepComplete {
				completed++
			}
		}
		c.currentStep = completed
	}
}

// SetStep updates the current step information
func (c *CompactProgress) SetStep(index int, status StepStatus, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentStep = index + 1
	c.status = status
	if message != "" {
		c.message = message
	}

	common.Trace("CompactProgress step updated: %d, status=%v, message=%s", c.currentStep, status, message)
}

// AddOutput is a no-op for compact progress (no output display)
func (c *CompactProgress) AddOutput(line string) {
	// Compact progress doesn't display output lines
}

// render draws the current progress state
func (c *CompactProgress) render() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get spinner frame
	frames := c.spinnerFrames()
	spinner := frames[c.spinnerIdx%len(frames)]
	c.spinnerIdx++

	// Build status line
	var statusLine string

	// Format: {spinner} {message}... (Step {current}/{total}) [{percent}%]
	statusLine = spinner + " " + c.message + "..."

	// Add step counter if we have steps
	if c.totalSteps > 0 {
		statusLine += fmt.Sprintf(" (Step %d/%d)", c.currentStep, c.totalSteps)
	}

	// Add percentage if we have progress
	if c.progress > 0 {
		percentage := int(c.progress * 100)
		if percentage > 100 {
			percentage = 100
		}
		statusLine += fmt.Sprintf(" [%d%%]", percentage)
	}

	// Clear line and print status
	common.Stdout("\r%s%s", csif("0K"), statusLine)
}

// renderFinal shows the final status line
func (c *CompactProgress) renderFinal(success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var icon string
	var color string

	if success {
		c.status = StepComplete
		if Iconic {
			icon = "✓"
		} else {
			icon = "+"
		}
		color = Green
	} else {
		c.status = StepFailed
		if Iconic {
			icon = "✗"
		} else {
			icon = "x"
		}
		color = Red
	}

	// Build final message
	var finalMsg string
	if c.totalSteps > 0 {
		if success {
			finalMsg = fmt.Sprintf("%s%s %s (%d/%d)%s", color, icon, c.message, c.totalSteps, c.totalSteps, Reset)
		} else {
			finalMsg = fmt.Sprintf("%s%s %s at step %d%s", color, icon, c.message, c.currentStep, Reset)
		}
	} else {
		finalMsg = fmt.Sprintf("%s%s %s%s", color, icon, c.message, Reset)
	}

	// Clear line and print final status with newline
	common.Stdout("\r%s%s\n", csif("0K"), finalMsg)
}

// cleanup restores terminal state
func (c *CompactProgress) cleanup() {
	ShowCursor()
}

// MultiTaskDashboard implementation (Layout D)
// Shows up to 5 parallel operations with individual progress bars

// TaskProgress represents the progress of a single task in the multi-task dashboard
type TaskProgress struct {
	Name     string
	Status   StepStatus
	Progress float64 // 0.0 to 1.0
	Message  string  // e.g., "(queued)", "error msg"
}

// MultiTaskDashboard shows up to 5 parallel operations with individual progress
type MultiTaskDashboard struct {
	baseDashboard
	tasks        []TaskProgress
	maxShow      int
	spinnerFrame int
}

// Start begins the multi-task dashboard display
func (m *MultiTaskDashboard) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	common.Trace("Starting MultiTaskDashboard with %d tasks", len(m.tasks))

	// Setup signal handler for graceful cleanup
	setupDashboardSignals(func() {
		m.cleanup()
	})

	// Hide cursor
	HideCursor()

	// Start render loop
	go m.startRenderLoop(m.render)
}

// Stop stops the dashboard and shows final status
func (m *MultiTaskDashboard) Stop(success bool) {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.mu.Unlock()

	common.Trace("Stopping MultiTaskDashboard with success=%v", success)

	// Stop render loop
	close(m.stopChan)
	<-m.doneChan

	// Cleanup
	m.cleanup()

	// Show final summary
	m.renderFinalSummary(success)
}

// Update updates the dashboard state (not used for this dashboard type)
func (m *MultiTaskDashboard) Update(state DashboardState) {
	// MultiTaskDashboard uses SetStep for updates
}

// SetStep updates a specific task's status and message
func (m *MultiTaskDashboard) SetStep(index int, status StepStatus, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.tasks) {
		common.Trace("SetStep: index %d out of range [0, %d)", index, len(m.tasks))
		return
	}

	m.tasks[index].Status = status
	m.tasks[index].Message = message

	// Set progress based on status
	switch status {
	case StepComplete:
		m.tasks[index].Progress = 1.0
	case StepFailed:
		m.tasks[index].Progress = 0.0
	case StepRunning:
		// Keep current progress
	case StepPending:
		m.tasks[index].Progress = 0.0
	}

	common.Trace("SetStep: task %d (%s) set to status %v, message=%s", index, m.tasks[index].Name, status, message)
}

// SetTaskProgress updates a specific task's progress (0.0 to 1.0)
func (m *MultiTaskDashboard) SetTaskProgress(index int, progress float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.tasks) {
		common.Trace("SetTaskProgress: index %d out of range [0, %d)", index, len(m.tasks))
		return
	}

	// Clamp progress to [0.0, 1.0]
	if progress < 0.0 {
		progress = 0.0
	}
	if progress > 1.0 {
		progress = 1.0
	}

	m.tasks[index].Progress = progress
	common.Trace("SetTaskProgress: task %d (%s) progress set to %.2f", index, m.tasks[index].Name, progress)
}

// AddOutput is not used for multi-task dashboard
func (m *MultiTaskDashboard) AddOutput(line string) {
	// Not implemented for this dashboard type
}

// render draws the dashboard frame
func (m *MultiTaskDashboard) render() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update spinner frame
	m.spinnerFrame = (m.spinnerFrame + 1) % 8

	// Calculate status counts
	complete := 0
	running := 0
	queued := 0
	failed := 0
	for _, task := range m.tasks {
		switch task.Status {
		case StepComplete:
			complete++
		case StepRunning:
			running++
		case StepPending:
			queued++
		case StepFailed:
			failed++
		}
	}

	// Clear screen and move to top
	MoveTo(1, 1)

	// Get box style
	style := ActiveBoxStyle()

	// Determine how many tasks to show
	tasksToShow := len(m.tasks)
	if tasksToShow > m.maxShow {
		tasksToShow = m.maxShow
	}

	// Calculate box dimensions
	boxWidth := 67

	// Draw top border with title
	title := fmt.Sprintf("Building Environments (%d of %d)", complete, len(m.tasks))
	common.Stdout("%s%s %s %s%s\n",
		style.TopLeft,
		strings.Repeat(style.Horizontal, 1),
		title,
		strings.Repeat(style.Horizontal, boxWidth-len(title)-4),
		style.TopRight)

	// Draw separator
	common.Stdout("%s%s%s\n",
		style.LeftT,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.RightT)

	// Draw tasks
	for i := 0; i < tasksToShow; i++ {
		task := m.tasks[i]
		common.Stdout("%s %s\n", style.Vertical, m.formatTaskLine(task, boxWidth-4))
	}

	// Show "... and N more" if there are more tasks
	if len(m.tasks) > m.maxShow {
		remaining := len(m.tasks) - m.maxShow
		line := fmt.Sprintf("   ... and %d more", remaining)
		common.Stdout("%s %s%s%s\n",
			style.Vertical,
			line,
			strings.Repeat(" ", boxWidth-len(line)-4),
			style.Vertical)
	}

	// Draw separator
	common.Stdout("%s%s%s\n",
		style.LeftT,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.RightT)

	// Draw footer with counts
	footer := m.formatFooter(complete, running, queued, failed)
	common.Stdout("%s %s%s%s\n",
		style.Vertical,
		footer,
		strings.Repeat(" ", boxWidth-len(footer)-4),
		style.Vertical)

	// Draw bottom border
	common.Stdout("%s%s%s\n",
		style.BottomLeft,
		strings.Repeat(style.Horizontal, boxWidth-2),
		style.BottomRight)
}

// formatTaskLine formats a single task line with status, name, progress bar, and percentage
func (m *MultiTaskDashboard) formatTaskLine(task TaskProgress, width int) string {
	// Get status icon (with spinner animation for running tasks)
	statusIcon := m.getStatusIcon(task.Status)

	// Progress bar width (20 chars)
	barWidth := 20
	filled := int(task.Progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	// Build progress bar
	var bar string
	if Iconic {
		bar = strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	} else {
		bar = strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
	}

	// Format percentage
	percentage := int(task.Progress * 100)

	// Build line
	line := statusIcon + " " + padRight(task.Name, 16) + " [" + bar + "] " + fmt.Sprintf("%3d%%", percentage)

	// Add message if present
	if task.Message != "" {
		line += "   " + task.Message
	}

	// Pad to width
	if len(line) < width {
		line += strings.Repeat(" ", width-len(line))
	} else if len(line) > width {
		line = line[:width]
	}

	return line + ActiveBoxStyle().Vertical
}

// getStatusIcon returns the icon for a status, with spinner animation for running
func (m *MultiTaskDashboard) getStatusIcon(status StepStatus) string {
	if status == StepRunning {
		// Animate spinner
		if Iconic {
			frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"}
			return Cyan + frames[m.spinnerFrame] + Reset
		}
		frames := []string{"|", "/", "-", "\\"}
		return Cyan + frames[m.spinnerFrame%4] + Reset
	}

	// Get color for status
	color := getStatusColor(status)
	icon := status.String()

	return color + icon + Reset
}

// formatFooter formats the footer line with status counts
func (m *MultiTaskDashboard) formatFooter(complete, running, queued, failed int) string {
	parts := []string{}

	if complete > 0 {
		parts = append(parts, Green+"Complete: "+fmt.Sprintf("%d", complete)+Reset)
	}
	if running > 0 {
		parts = append(parts, Cyan+"Running: "+fmt.Sprintf("%d", running)+Reset)
	}
	if queued > 0 {
		parts = append(parts, Grey+"Queued: "+fmt.Sprintf("%d", queued)+Reset)
	}
	if failed > 0 {
		parts = append(parts, Red+"Failed: "+fmt.Sprintf("%d", failed)+Reset)
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "   "
		}
		result += part
	}

	return result
}

// cleanup restores terminal state
func (m *MultiTaskDashboard) cleanup() {
	ShowCursor()
	ClearScreen()
	MoveTo(1, 1)
}

// renderFinalSummary shows the final summary after stopping
func (m *MultiTaskDashboard) renderFinalSummary(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Count final statuses
	complete := 0
	failed := 0
	for _, task := range m.tasks {
		if task.Status == StepComplete {
			complete++
		} else if task.Status == StepFailed {
			failed++
		}
	}

	// Show summary
	if success {
		common.Stdout("%sAll tasks completed successfully (%d/%d)%s\n", Green, complete, len(m.tasks), Reset)
	} else {
		common.Stdout("%sTasks completed with errors (%d completed, %d failed)%s\n", Red, complete, failed, Reset)
	}
}

// Helper functions for MultiTaskDashboard

func getStatusColor(status StepStatus) string {
	switch status {
	case StepComplete:
		return Green
	case StepRunning:
		return Cyan
	case StepFailed:
		return Red
	case StepPending:
		return Grey
	case StepSkipped:
		return Faint
	default:
		return Reset
	}
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}
