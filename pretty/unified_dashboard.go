package pretty

// UnifiedDashboard provides a seamless Bubble Tea dashboard that handles both
// environment build and robot execution phases in a single terminal session.
// This eliminates the output gap between separate dashboards.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	teaprogress "github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
)

// DashboardPhase represents the current phase of the unified dashboard
type DashboardPhase int

const (
	PhaseEnvironment DashboardPhase = iota
	PhaseRobot
	PhaseComplete
)

// UnifiedDashboard is a single Bubble Tea dashboard that handles all phases
type UnifiedDashboard struct {
	mu        sync.Mutex
	program   *tea.Program
	model     *unifiedModel
	running   bool
	startTime time.Time
}

// unifiedModel is the Bubble Tea model for the unified dashboard
type unifiedModel struct {
	// General state
	width      int
	height     int
	startTime  time.Time
	quitting   bool
	success    bool
	phase      DashboardPhase
	spinner    spinner.Model
	progress   teaprogress.Model
	updateChan chan unifiedUpdate

	// Environment build phase
	envSteps       []envStepInfo
	envCurrentStep int

	// Robot run phase
	robotName     string
	taskName      string
	robotStatus   string
	robotOutput   []string
	maxOutputLines int

	// Shared state
	warnings []string
	notices  []string

	// Result information (shown at the end)
	resultContext string
	resultSuccess bool
	resultReason  string
}

type envStepInfo struct {
	name    string
	status  StepStatus
	message string
}

type unifiedUpdate struct {
	// Environment updates
	stepIndex   int
	stepStatus  StepStatus
	stepMessage string

	// Robot updates
	robotStatus string
	taskName    string
	outputLine  string

	// Shared updates
	warning      string
	notice       string
	phaseChange  *DashboardPhase
	resultInfo   *resultUpdate
}

type resultUpdate struct {
	context string
	success bool
	reason  string
}

// Messages for Bubble Tea
type unifiedTickMsg time.Time
type unifiedUpdateMsg unifiedUpdate
type unifiedQuitMsg struct{ success bool }

// Unified styles using lipgloss - consistent theme throughout
var (
	unifiedTitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		MarginBottom(0)

	unifiedBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2)

	unifiedHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255"))

	unifiedSubHeaderStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	unifiedStepPendingStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	unifiedStepRunningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("39"))

	unifiedStepCompleteStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	unifiedStepFailedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196"))

	unifiedProgressStyle = lipgloss.NewStyle().
		MarginTop(1)

	unifiedWarningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	unifiedNoticeStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Italic(true)

	unifiedOutputStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("250"))

	unifiedDurationStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Italic(true)

	unifiedSuccessStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	unifiedFailureStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true)
)

// Global unified dashboard instance for the current operation
var globalUnifiedDashboard *UnifiedDashboard
var globalUnifiedMu sync.Mutex

// GetUnifiedDashboard returns the global unified dashboard, creating one if needed
func GetUnifiedDashboard() *UnifiedDashboard {
	globalUnifiedMu.Lock()
	defer globalUnifiedMu.Unlock()
	return globalUnifiedDashboard
}

// StopUnifiedDashboard stops the global unified dashboard if one is running
// This should be called when an operation completes without robot execution
func StopUnifiedDashboard(success bool) {
	globalUnifiedMu.Lock()
	dashboard := globalUnifiedDashboard
	globalUnifiedMu.Unlock()

	if dashboard != nil {
		dashboard.Stop(success)
	}
}

// NewUnifiedDashboard creates a new unified dashboard for the entire operation
func NewUnifiedDashboard(envSteps []string) *UnifiedDashboard {
	if !ShouldUseDashboard() {
		return nil
	}

	stepInfos := make([]envStepInfo, len(envSteps))
	for i, name := range envSteps {
		stepInfos[i] = envStepInfo{name: name, status: StepPending}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	p := teaprogress.New(
		teaprogress.WithDefaultGradient(),
		teaprogress.WithWidth(60),
		teaprogress.WithoutPercentage(),
	)

	model := &unifiedModel{
		startTime:      time.Now(),
		phase:          PhaseEnvironment,
		spinner:        s,
		progress:       p,
		envSteps:       stepInfos,
		robotOutput:    make([]string, 0, 20),
		maxOutputLines: 15,
		warnings:       make([]string, 0, 5),
		notices:        make([]string, 0, 5),
		updateChan:     make(chan unifiedUpdate, 100),
	}

	dashboard := &UnifiedDashboard{
		model:     model,
		startTime: time.Now(),
	}

	// Store globally so robot phase can access it
	globalUnifiedMu.Lock()
	globalUnifiedDashboard = dashboard
	globalUnifiedMu.Unlock()

	return dashboard
}

// Start begins the unified dashboard
func (d *UnifiedDashboard) Start() {
	if d == nil {
		return
	}

	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.startTime = time.Now()
	d.mu.Unlock()

	// Mark dashboard as active to suppress log output
	setDashboardActive(true)

	// Intercept all log output and route appropriately
	common.SetLogInterceptor(func(message string) bool {
		// Capture warnings and notices
		if strings.Contains(message, "Warning:") {
			d.AddWarning(strings.TrimPrefix(message, "Warning: "))
		} else if strings.Contains(message, "Note:") {
			d.AddNotice(strings.TrimPrefix(message, "Note: "))
		}
		return true // Suppress all terminal output
	})

	// Start Bubble Tea program with alt screen
	d.program = tea.NewProgram(d.model, tea.WithAltScreen())

	go func() {
		if _, err := d.program.Run(); err != nil {
			common.Error("unified dashboard", err)
		}
	}()

	// Start update listener
	go d.listenForUpdates()
}

func (d *UnifiedDashboard) listenForUpdates() {
	for update := range d.model.updateChan {
		if d.program != nil {
			d.program.Send(unifiedUpdateMsg(update))
		}
	}
}

// Stop terminates the dashboard
func (d *UnifiedDashboard) Stop(success bool) {
	if d == nil {
		return
	}

	// If we should keep the dashboard alive (e.g. for robot phase), don't stop yet
	if IsKeepDashboardAlive() {
		return
	}

	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	d.mu.Unlock()

	// Clear log interceptor
	common.ClearLogInterceptor()
	setDashboardActive(false)

	// Clear global reference
	globalUnifiedMu.Lock()
	globalUnifiedDashboard = nil
	globalUnifiedMu.Unlock()

	// Send quit message
	if d.program != nil {
		d.program.Send(unifiedQuitMsg{success: success})
		// Give it time to render final state
		time.Sleep(150 * time.Millisecond)
		d.program.Quit()
	}

	close(d.model.updateChan)
}

// Update implements Dashboard interface
func (d *UnifiedDashboard) Update(state DashboardState) {
	if d == nil || d.model == nil {
		return
	}

	if state.Message != "" {
		select {
		case d.model.updateChan <- unifiedUpdate{robotStatus: state.Message}:
		default:
		}
	}
}

// SetStep updates a step's status (environment phase)
func (d *UnifiedDashboard) SetStep(index int, status StepStatus, message string) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.model.updateChan <- unifiedUpdate{
		stepIndex:   index,
		stepStatus:  status,
		stepMessage: message,
	}:
	default:
	}
}

// AddOutput adds robot output to the scrolling region
func (d *UnifiedDashboard) AddOutput(line string) {
	if d == nil || d.model == nil {
		return
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	select {
	case d.model.updateChan <- unifiedUpdate{outputLine: line}:
	default:
	}
}

// AddWarning adds a warning to display
func (d *UnifiedDashboard) AddWarning(warning string) {
	if d == nil || d.model == nil {
		return
	}

	warning = strings.TrimSpace(warning)
	if warning == "" {
		return
	}

	select {
	case d.model.updateChan <- unifiedUpdate{warning: warning}:
	default:
	}
}

// AddNotice adds a notice to display
func (d *UnifiedDashboard) AddNotice(notice string) {
	if d == nil || d.model == nil {
		return
	}

	notice = strings.TrimSpace(notice)
	if notice == "" {
		return
	}

	select {
	case d.model.updateChan <- unifiedUpdate{notice: notice}:
	default:
	}
}

// TransitionToRobotPhase switches the dashboard to robot execution mode
func (d *UnifiedDashboard) TransitionToRobotPhase(robotName, taskName string) {
	if d == nil || d.model == nil {
		return
	}

	phase := PhaseRobot
	select {
	case d.model.updateChan <- unifiedUpdate{
		phaseChange: &phase,
		taskName:    taskName,
	}:
	default:
	}

	// Update robot name directly
	d.mu.Lock()
	d.model.robotName = robotName
	d.mu.Unlock()
}

// SetRobotStatus updates the robot execution status
func (d *UnifiedDashboard) SetRobotStatus(status string) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.model.updateChan <- unifiedUpdate{robotStatus: status}:
	default:
	}
}

// SetTaskName updates the current task name
func (d *UnifiedDashboard) SetTaskName(taskName string) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.model.updateChan <- unifiedUpdate{taskName: taskName}:
	default:
	}
}

// SetResult sets the final result information to display
func (d *UnifiedDashboard) SetResult(context string, success bool, reason string) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.model.updateChan <- unifiedUpdate{
		resultInfo: &resultUpdate{
			context: context,
			success: success,
			reason:  reason,
		},
	}:
	default:
	}
}

// Bubble Tea methods for the model

func (m *unifiedModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		unifiedTickCmd(),
	)
}

func unifiedTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return unifiedTickMsg(t)
	})
}

func (m *unifiedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.Width = min(msg.Width-20, 60)
		// Adaptive output lines based on terminal height
		if msg.Height > 40 {
			m.maxOutputLines = 18
		} else if msg.Height > 30 {
			m.maxOutputLines = 12
		} else {
			m.maxOutputLines = 8
		}

	case unifiedTickMsg:
		return m, unifiedTickCmd()

	case unifiedUpdateMsg:
		// Handle step updates (environment phase)
		if msg.stepStatus != 0 || msg.stepMessage != "" {
			if msg.stepIndex >= 0 && msg.stepIndex < len(m.envSteps) {
				if msg.stepStatus != 0 {
					m.envSteps[msg.stepIndex].status = msg.stepStatus
				}
				if msg.stepMessage != "" {
					m.envSteps[msg.stepIndex].message = msg.stepMessage
				}
				if msg.stepStatus == StepRunning {
					m.envCurrentStep = msg.stepIndex
				}
			}
		}

		// Handle robot status updates
		if msg.robotStatus != "" {
			m.robotStatus = msg.robotStatus
		}
		if msg.taskName != "" {
			m.taskName = msg.taskName
		}

		// Handle output lines
		if msg.outputLine != "" {
			m.robotOutput = append(m.robotOutput, msg.outputLine)
			if len(m.robotOutput) > m.maxOutputLines {
				m.robotOutput = m.robotOutput[len(m.robotOutput)-m.maxOutputLines:]
			}
		}

		// Handle warnings
		if msg.warning != "" {
			m.warnings = append(m.warnings, msg.warning)
			if len(m.warnings) > 5 {
				m.warnings = m.warnings[len(m.warnings)-5:]
			}
		}

		// Handle notices
		if msg.notice != "" {
			m.notices = append(m.notices, msg.notice)
			if len(m.notices) > 3 {
				m.notices = m.notices[len(m.notices)-3:]
			}
		}

		// Handle phase change
		if msg.phaseChange != nil {
			m.phase = *msg.phaseChange
		}

		// Handle result info
		if msg.resultInfo != nil {
			m.resultContext = msg.resultInfo.context
			m.resultSuccess = msg.resultInfo.success
			m.resultReason = msg.resultInfo.reason
		}

	case unifiedQuitMsg:
		m.success = msg.success
		m.quitting = true
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *unifiedModel) View() string {
	if m.quitting {
		return m.renderFinal()
	}

	var b strings.Builder

	// Calculate elapsed time
	elapsed := time.Since(m.startTime)
	durationStr := formatDuration(elapsed)

	// Title bar based on phase
	var title string
	switch m.phase {
	case PhaseEnvironment:
		completed := 0
		for _, step := range m.envSteps {
			if step.status == StepComplete {
				completed++
			}
		}
		title = fmt.Sprintf("RCC Environment Build  [%d/%d]  %s", completed, len(m.envSteps), durationStr)
	case PhaseRobot:
		title = fmt.Sprintf("RCC Robot Run  %s  %s", m.robotName, durationStr)
	default:
		title = fmt.Sprintf("RCC  %s", durationStr)
	}
	b.WriteString(unifiedTitleStyle.Render(title))
	b.WriteString("\n\n")

	// Main content based on phase
	switch m.phase {
	case PhaseEnvironment:
		b.WriteString(m.renderEnvironmentPhase())
	case PhaseRobot:
		b.WriteString(m.renderRobotPhase())
	}

	// Notices section
	if len(m.notices) > 0 {
		b.WriteString("\n")
		b.WriteString(unifiedNoticeStyle.Render("Notices:"))
		b.WriteString("\n")
		for _, notice := range m.notices {
			if len(notice) > 70 {
				notice = notice[:67] + "..."
			}
			b.WriteString(unifiedNoticeStyle.Render("  " + notice))
			b.WriteString("\n")
		}
	}

	// Warnings section
	if len(m.warnings) > 0 {
		b.WriteString("\n")
		b.WriteString(unifiedWarningStyle.Render("Warnings:"))
		b.WriteString("\n")
		for _, warning := range m.warnings {
			if len(warning) > 70 {
				warning = warning[:67] + "..."
			}
			b.WriteString(unifiedWarningStyle.Render("  ! " + warning))
			b.WriteString("\n")
		}
	}

	// Progress bar
	b.WriteString("\n")
	progress := m.calculateProgress()
	b.WriteString(unifiedProgressStyle.Render(m.progress.ViewAs(progress)))
	b.WriteString("\n")

	// Wrap in box
	return unifiedBoxStyle.Render(b.String())
}

func (m *unifiedModel) renderEnvironmentPhase() string {
	var b strings.Builder

	// Show steps in a compact way
	for i, step := range m.envSteps {
		line := m.renderEnvStep(i, step)
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (m *unifiedModel) renderEnvStep(index int, step envStepInfo) string {
	var icon string
	var style lipgloss.Style

	switch step.status {
	case StepPending:
		icon = "o"
		style = unifiedStepPendingStyle
	case StepRunning:
		icon = m.spinner.View()
		style = unifiedStepRunningStyle
	case StepComplete:
		icon = "+"
		style = unifiedStepCompleteStyle
	case StepFailed:
		icon = "x"
		style = unifiedStepFailedStyle
	case StepSkipped:
		icon = "-"
		style = unifiedStepPendingStyle
	}

	text := fmt.Sprintf("%s %2d. %s", icon, index+1, step.name)

	return style.Render(text)
}

func (m *unifiedModel) renderRobotPhase() string {
	var b strings.Builder

	// Show a compact summary of completed environment steps
	completedSteps := 0
	for _, step := range m.envSteps {
		if step.status == StepComplete {
			completedSteps++
		}
	}
	envSummary := fmt.Sprintf("Environment: %d/%d steps complete", completedSteps, len(m.envSteps))
	b.WriteString(unifiedStepCompleteStyle.Render("+ " + envSummary))
	b.WriteString("\n\n")

	// Robot execution header with visual separator
	b.WriteString(unifiedHeaderStyle.Render("--- Robot Execution ---"))
	b.WriteString("\n\n")

	// Robot and Task info in a structured way
	robotLine := fmt.Sprintf("  Robot:  %s", m.robotName)
	b.WriteString(unifiedSubHeaderStyle.Render(robotLine))
	b.WriteString("\n")

	taskLine := fmt.Sprintf("  Task:   %s", m.taskName)
	b.WriteString(unifiedStepRunningStyle.Render(taskLine))
	b.WriteString("\n")

	// Status with spinner
	statusIcon := m.spinner.View()
	if m.robotStatus == "Completed" || m.robotStatus == "Complete" {
		statusIcon = "+"
	} else if m.robotStatus == "Failed" {
		statusIcon = "x"
	}
	statusLine := fmt.Sprintf("  Status: %s %s", statusIcon, m.robotStatus)
	var statusStyle lipgloss.Style
	switch m.robotStatus {
	case "Completed", "Complete":
		statusStyle = unifiedStepCompleteStyle
	case "Failed":
		statusStyle = unifiedStepFailedStyle
	default:
		statusStyle = unifiedStepRunningStyle
	}
	b.WriteString(statusStyle.Render(statusLine))
	b.WriteString("\n\n")

	// Output section with bordered area
	b.WriteString(unifiedSubHeaderStyle.Render("  Live Output:"))
	b.WriteString("\n")

	// Calculate available width for output
	maxWidth := 72
	if m.width > 80 {
		maxWidth = m.width - 12
	}

	if len(m.robotOutput) > 0 {
		// Show output with indentation and subtle styling
		for _, line := range m.robotOutput {
			// Truncate long lines
			displayLine := line
			if len(displayLine) > maxWidth {
				displayLine = displayLine[:maxWidth-3] + "..."
			}
			b.WriteString(unifiedOutputStyle.Render("    " + displayLine))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(unifiedStepPendingStyle.Render("    Waiting for output..."))
		b.WriteString("\n")
	}

	// Pad to fill space for consistent layout
	outputLines := len(m.robotOutput)
	if outputLines == 0 {
		outputLines = 1
	}
	for i := outputLines; i < m.maxOutputLines; i++ {
		b.WriteString("\n")
	}

	return b.String()
}

func (m *unifiedModel) calculateProgress() float64 {
	switch m.phase {
	case PhaseEnvironment:
		completed := 0
		for _, step := range m.envSteps {
			if step.status == StepComplete {
				completed++
			}
		}
		return float64(completed) / float64(len(m.envSteps))
	case PhaseRobot:
		// Pulsing animation for robot phase
		elapsed := time.Since(m.startTime)
		pulse := (float64(elapsed.Milliseconds()%3000) / 3000.0)
		if pulse > 0.5 {
			pulse = 1.0 - pulse
		}
		return pulse * 2
	default:
		return 1.0
	}
}

func (m *unifiedModel) renderFinal() string {
	var b strings.Builder

	elapsed := time.Since(m.startTime)

	if m.success {
		b.WriteString(unifiedSuccessStyle.Render(fmt.Sprintf("+ Operation completed successfully in %s", formatDuration(elapsed))))
	} else {
		msg := fmt.Sprintf("x Operation failed after %s", formatDuration(elapsed))
		if m.resultReason != "" {
			msg += fmt.Sprintf(": %s", m.resultReason)
		}
		b.WriteString(unifiedFailureStyle.Render(msg))
	}

	// Show any final warnings
	if len(m.warnings) > 0 {
		b.WriteString("\n\n")
		b.WriteString(unifiedWarningStyle.Render("Warnings:"))
		for _, warning := range m.warnings {
			b.WriteString("\n")
			b.WriteString(unifiedWarningStyle.Render("  ! " + warning))
		}
	}

	return b.String()
}
