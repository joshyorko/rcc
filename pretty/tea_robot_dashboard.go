package pretty

// Bubble Tea based dashboard for robot execution
// Provides a clean, modern UI that completely owns the terminal during operation
// Shows comprehensive information: task progress, environment info, output, and notices

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

// TeaRobotDashboard is a Bubble Tea-based dashboard for robot execution
type TeaRobotDashboard struct {
	mu        sync.Mutex
	program   *tea.Program
	model     *robotDashboardModel
	running   bool
	startTime time.Time
}

// robotDashboardModel is the Bubble Tea model for the robot dashboard
type robotDashboardModel struct {
	// Basic info
	robotName string
	taskName  string
	status    string

	// Context info (computer name, platform, workers, CPUs)
	contextName string
	platform    string
	workers     int
	cpus        int

	// Environment info
	envHash    string
	workingDir string
	envPath    string

	// UI components
	spinner  spinner.Model
	progress teaprogress.Model

	// Terminal dimensions
	width  int
	height int

	// Timing
	startTime time.Time

	// State
	quitting bool
	success  bool

	// Output and messages
	outputLines []string
	maxLines    int
	notices     []string
	warnings    []string
	updateChan  chan robotUpdate
}

type robotUpdate struct {
	status      string
	outputLine  string
	notice      string
	warning     string
	taskName    string
	envHash     string
	workingDir  string
	envPath     string
	contextName string
	platform    string
	workers     int
	cpus        int
}

// Messages for Bubble Tea
type robotTickMsg time.Time
type robotUpdateMsg robotUpdate
type robotQuitMsg struct{ success bool }

// Styles for the robot dashboard - matched to build dashboard for consistency
var (
	// Title style matches build dashboard titleStyle
	robotTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

	// Box style EXACTLY matches build dashboard boxStyle for visual consistency
	robotBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	robotLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	robotValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	robotStatusRunning = lipgloss.NewStyle().
				Foreground(lipgloss.Color("39"))

	robotStatusComplete = lipgloss.NewStyle().
				Foreground(lipgloss.Color("42"))

	robotStatusFailed = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))

	robotOutputStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("250"))

	robotWarningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214"))

	robotDurationStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Italic(true)
)

// NewTeaRobotDashboard creates a new Bubble Tea-based robot dashboard
func NewTeaRobotDashboard(robotName string) *TeaRobotDashboard {
	if !ShouldUseDashboard() {
		return nil
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	p := teaprogress.New(
		teaprogress.WithDefaultGradient(),
		teaprogress.WithWidth(60),
		teaprogress.WithoutPercentage(),
	)

	model := &robotDashboardModel{
		robotName:   robotName,
		taskName:    "Initializing",
		status:      "Starting",
		spinner:     s,
		progress:    p,
		startTime:   time.Now(),
		outputLines: make([]string, 0, 5), // Only need recent lines
		maxLines:    1,                    // Single line display
		notices:     make([]string, 0, 10),
		warnings:    make([]string, 0, 10),
		updateChan:  make(chan robotUpdate, 200),
	}

	return &TeaRobotDashboard{
		model:     model,
		startTime: time.Now(),
	}
}

// Start begins the dashboard
func (d *TeaRobotDashboard) Start() {
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

	// Intercept log output to capture notices and warnings
	common.SetLogInterceptor(func(message string) bool {
		// Capture different types of messages
		if strings.Contains(message, "Warning:") {
			d.AddWarning(strings.TrimPrefix(message, "Warning: "))
		} else if strings.Contains(message, "Note:") || strings.Contains(message, "Note!") {
			d.AddNotice(strings.TrimPrefix(strings.TrimPrefix(message, "Note: "), "Note! "))
		} else if strings.HasPrefix(message, "####  Progress:") {
			// Extract progress info
			d.AddNotice(message)
		}
		return true // Suppress all from going to terminal
	})

	// Start Bubble Tea program with alt screen
	d.program = tea.NewProgram(d.model, tea.WithAltScreen())

	go func() {
		if _, err := d.program.Run(); err != nil {
			common.Error("robot dashboard", err)
		}
	}()

	// Start update listener
	go d.listenForUpdates()
}

func (d *TeaRobotDashboard) listenForUpdates() {
	for update := range d.model.updateChan {
		if d.program != nil {
			d.program.Send(robotUpdateMsg(update))
		}
	}
}

// Stop terminates the dashboard
func (d *TeaRobotDashboard) Stop(success bool) {
	if d == nil {
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

	// Send quit message
	if d.program != nil {
		d.program.Send(robotQuitMsg{success: success})
		// Give it time to render final state
		time.Sleep(100 * time.Millisecond)
		d.program.Quit()
	}

	close(d.model.updateChan)
}

// Update implements Dashboard interface
func (d *TeaRobotDashboard) Update(state DashboardState) {
	if d == nil || d.model == nil {
		return
	}

	if state.Message != "" {
		select {
		case d.model.updateChan <- robotUpdate{status: state.Message}:
		default:
		}
	}
}

// SetStep updates the task name/status
func (d *TeaRobotDashboard) SetStep(index int, status StepStatus, message string) {
	if d == nil || d.model == nil {
		return
	}

	statusStr := ""
	switch status {
	case StepRunning:
		statusStr = "Running"
	case StepComplete:
		statusStr = "Complete"
	case StepFailed:
		statusStr = "Failed"
	}

	if statusStr != "" || message != "" {
		select {
		case d.model.updateChan <- robotUpdate{status: statusStr, taskName: message}:
		default:
		}
	}
}

// AddOutput adds a line to the scrolling output region
func (d *TeaRobotDashboard) AddOutput(line string) {
	if d == nil || d.model == nil {
		return
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	select {
	case d.model.updateChan <- robotUpdate{outputLine: line}:
	default:
	}
}

// AddWarning adds a warning to be displayed in the dashboard
func (d *TeaRobotDashboard) AddWarning(warning string) {
	if d == nil || d.model == nil {
		return
	}

	warning = strings.TrimSpace(warning)
	if warning == "" {
		return
	}

	select {
	case d.model.updateChan <- robotUpdate{warning: warning}:
	default:
	}
}

// AddNotice adds a notice/note to be displayed in the dashboard
func (d *TeaRobotDashboard) AddNotice(notice string) {
	if d == nil || d.model == nil {
		return
	}

	notice = strings.TrimSpace(notice)
	if notice == "" {
		return
	}

	select {
	case d.model.updateChan <- robotUpdate{notice: notice}:
	default:
	}
}

// SetEnvironmentInfo sets environment-related info to display
func (d *TeaRobotDashboard) SetEnvironmentInfo(envHash, workingDir, envPath string) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.model.updateChan <- robotUpdate{envHash: envHash, workingDir: workingDir, envPath: envPath}:
	default:
	}
}

// SetContextInfo sets context info (computer name, platform, workers, CPUs) like RCC point of view
func (d *TeaRobotDashboard) SetContextInfo(contextName, platform string, workers, cpus int) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.model.updateChan <- robotUpdate{contextName: contextName, platform: platform, workers: workers, cpus: cpus}:
	default:
	}
}

// Bubble Tea methods for the model

func (m *robotDashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		robotTickCmd(),
	)
}

func robotTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return robotTickMsg(t)
	})
}

func (m *robotDashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		// Single line output - no multi-line adjustment needed

	case robotTickMsg:
		return m, robotTickCmd()

	case robotUpdateMsg:
		if msg.status != "" {
			m.status = msg.status
		}
		if msg.taskName != "" {
			m.taskName = msg.taskName
		}
		if msg.outputLine != "" {
			// Keep only last 3 lines for single-line streaming display
			m.outputLines = append(m.outputLines, msg.outputLine)
			if len(m.outputLines) > 3 {
				m.outputLines = m.outputLines[len(m.outputLines)-3:]
			}
		}
		if msg.notice != "" {
			m.notices = append(m.notices, msg.notice)
			if len(m.notices) > 5 {
				m.notices = m.notices[len(m.notices)-5:]
			}
		}
		if msg.warning != "" {
			m.warnings = append(m.warnings, msg.warning)
			if len(m.warnings) > 5 {
				m.warnings = m.warnings[len(m.warnings)-5:]
			}
		}
		if msg.envHash != "" {
			m.envHash = msg.envHash
		}
		if msg.workingDir != "" {
			m.workingDir = msg.workingDir
		}
		if msg.envPath != "" {
			m.envPath = msg.envPath
		}
		if msg.contextName != "" {
			m.contextName = msg.contextName
		}
		if msg.platform != "" {
			m.platform = msg.platform
		}
		if msg.workers > 0 {
			m.workers = msg.workers
		}
		if msg.cpus > 0 {
			m.cpus = msg.cpus
		}

	case robotQuitMsg:
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

func (m *robotDashboardModel) View() string {
	if m.quitting {
		return m.renderFinal()
	}

	var b strings.Builder
	elapsed := time.Since(m.startTime)

	// Fixed width to prevent layout shifts - matches build dashboard approach
	contentWidth := 50
	if m.width > 0 {
		contentWidth = min(m.width-6, 60) // Leave room for border
	}
	if contentWidth < 40 {
		contentWidth = 40
	}

	// Title with counter like build dashboard: "RCC Robot Run  robotname"
	title := fmt.Sprintf("RCC Robot Run  %s", m.robotName)
	b.WriteString(robotTitleStyle.Render(title))
	b.WriteString("\n\n")

	// Step 1: Task line with spinner (like build dashboard steps)
	b.WriteString(m.spinner.View())
	b.WriteString(" ")
	taskLine := m.taskName
	if m.envHash != "" {
		taskLine += "  " + robotLabelStyle.Render("env:"+m.envHash[:min(8, len(m.envHash))])
	}
	b.WriteString(robotValueStyle.Render(taskLine))
	b.WriteString("\n")

	// Step 2: Output line with spinner (streaming single line)
	b.WriteString(m.spinner.View())
	b.WriteString(" ")
	if len(m.outputLines) > 0 {
		lastLine := m.outputLines[len(m.outputLines)-1]
		if len(lastLine) > contentWidth-4 {
			lastLine = lastLine[:contentWidth-7] + "..."
		}
		b.WriteString(robotOutputStyle.Render(lastLine))
	} else {
		b.WriteString(robotLabelStyle.Render("Waiting for output..."))
	}
	b.WriteString("\n")

	// Progress bar with pulsing animation
	b.WriteString("\n")
	m.progress.Width = contentWidth
	pulseProgress := (float64(elapsed.Milliseconds()%2000) / 2000.0)
	if pulseProgress > 0.5 {
		pulseProgress = 1.0 - pulseProgress
	}
	pulseProgress = pulseProgress * 2
	b.WriteString(m.progress.ViewAs(pulseProgress))

	// ETA/duration line like build dashboard
	if m.platform != "" || m.workers > 0 {
		contextInfo := fmt.Sprintf("  %s", m.platform)
		if m.workers > 0 && m.cpus > 0 {
			contextInfo += fmt.Sprintf(" (%d/%d workers)", m.workers, m.cpus)
		}
		b.WriteString(robotDurationStyle.Render(contextInfo))
	}
	b.WriteString(robotDurationStyle.Render(fmt.Sprintf("  [%s]", formatDuration(elapsed))))
	b.WriteString("\n")

	// Wrap in box - same style as build dashboard
	return robotBoxStyle.Render(b.String())
}

func (m *robotDashboardModel) renderFinal() string {
	elapsed := time.Since(m.startTime)

	var result string
	if m.success {
		result = robotStatusComplete.Render(fmt.Sprintf("+ Robot run completed successfully in %s", formatDuration(elapsed)))
	} else {
		result = robotStatusFailed.Render(fmt.Sprintf("x Robot run failed after %s", formatDuration(elapsed)))
	}

	// Show final warnings if any
	if len(m.warnings) > 0 {
		result += "\n\n"
		result += robotWarningStyle.Render("Warnings encountered:")
		for _, warning := range m.warnings {
			result += "\n"
			result += robotWarningStyle.Render("  ! " + warning)
		}
	}

	return result
}

// Helper to truncate paths for display
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Show beginning and end
	half := (maxLen - 3) / 2
	return path[:half] + "..." + path[len(path)-half:]
}
