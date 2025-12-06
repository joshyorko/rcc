package pretty

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	teaprogress "github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
)

// RootModel is the central state for the unified dashboard
type RootModel struct {
	// Core state
	Mode      DashboardMode
	Width     int
	Height    int
	StartTime time.Time
	Quitting  bool
	Success   bool

	// UI toggles
	ShowLogs    bool
	ShowDetails bool

	// Data
	Title    string
	SubTitle string
	Version  string

	// Components
	Spinner       spinner.Model
	LogViewport   viewport.Model
	ProgressModel teaprogress.Model
	Styles        Styles

	// Tracking
	Progress *ProgressTracker
	Logs     *StyledLogBuffer

	// Mode-specific data
	EnvState   *EnvState
	RobotState *RobotState

	// Update channel
	updateChan chan UnifiedUpdateMsg
}

// Bubble Tea messages
type rootTickMsg time.Time

func rootTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return rootTickMsg(t)
	})
}

// NewRootModel creates a new RootModel with default settings
func NewRootModel() *RootModel {
	theme := DefaultTheme()
	styles := NewStyles(theme)

	// Create a beautiful spinner
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		FPS:    time.Second / 10,
	}
	s.Style = styles.Spinner

	// Progress bar with vibrant gradient (cyan -> green)
	p := teaprogress.New(
		teaprogress.WithGradient("#89ddff", "#c3e88d"),
		teaprogress.WithWidth(80),
		teaprogress.WithoutPercentage(),
	)

	// Log viewport - larger
	vp := viewport.New(100, 12)
	vp.Style = styles.LogBox

	// Get hostname for display
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	return &RootModel{
		Mode:          ModeEnvironment,
		StartTime:     time.Now(),
		Version:       common.Version,
		Styles:        styles,
		Spinner:       s,
		ProgressModel: p,
		LogViewport:   vp,
		Logs:          NewStyledLogBuffer(500),
		EnvState: &EnvState{
			Steps: make([]EnvStep, 0),
		},
		RobotState: &RobotState{
			Hostname:   hostname,
			Controller: common.ControllerIdentity(),
		},
		updateChan: make(chan UnifiedUpdateMsg, 100),
	}
}

func (m *RootModel) Init() tea.Cmd {
	return tea.Batch(
		m.Spinner.Tick,
		rootTickCmd(),
	)
}

func (m *RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.Quitting = true
			return m, tea.Quit
		case "esc":
			// In run complete mode, Esc signals return to previous view
			if m.Mode == ModeRunComplete {
				m.Quitting = true
				return m, tea.Quit
			}
		case "l":
			m.ShowLogs = !m.ShowLogs
		case "d":
			m.ShowDetails = !m.ShowDetails
		case "up", "k":
			if m.Mode == ModeRunComplete {
				m.RobotState.LogScroll--
				if m.RobotState.LogScroll < 0 {
					m.RobotState.LogScroll = 0
				}
			} else if m.ShowLogs {
				m.LogViewport.LineUp(1)
			}
		case "down", "j":
			if m.Mode == ModeRunComplete {
				m.RobotState.LogScroll++
			} else if m.ShowLogs {
				m.LogViewport.LineDown(1)
			}
		case "pgup":
			if m.Mode == ModeRunComplete {
				m.RobotState.LogScroll -= 10
				if m.RobotState.LogScroll < 0 {
					m.RobotState.LogScroll = 0
				}
			} else if m.ShowLogs {
				m.LogViewport.HalfViewUp()
			}
		case "pgdown":
			if m.Mode == ModeRunComplete {
				m.RobotState.LogScroll += 10
			} else if m.ShowLogs {
				m.LogViewport.HalfViewDown()
			}
		case "g":
			// Go to top in run complete mode
			if m.Mode == ModeRunComplete {
				m.RobotState.LogScroll = 0
			}
		case "G":
			// Go to bottom in run complete mode
			if m.Mode == ModeRunComplete && len(m.RobotState.LogLines) > 0 {
				m.RobotState.LogScroll = len(m.RobotState.LogLines)
			}
		}

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height

		// Adjust component sizes
		contentWidth := msg.Width - 4
		if contentWidth < 40 {
			contentWidth = 40
		}
		if contentWidth > 100 {
			contentWidth = 100
		}

		m.LogViewport.Width = contentWidth
		m.LogViewport.Height = minInt(msg.Height/3, 15)
		m.ProgressModel.Width = contentWidth - 10

	case rootTickMsg:
		// Refresh display
		cmds = append(cmds, rootTickCmd())

	case spinner.TickMsg:
		m.Spinner, cmd = m.Spinner.Update(msg)
		cmds = append(cmds, cmd)

	case UnifiedUpdateMsg:
		m.handleUpdate(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m *RootModel) handleUpdate(msg UnifiedUpdateMsg) {
	// Handle phase change
	if msg.PhaseChange != nil {
		m.Mode = *msg.PhaseChange
	}

	// Handle environment updates
	if m.EnvState != nil && msg.StepIndex >= 0 && msg.StepIndex < len(m.EnvState.Steps) {
		if msg.StepStatus != 0 {
			m.EnvState.Steps[msg.StepIndex].Status = msg.StepStatus
			m.EnvState.Steps[msg.StepIndex].Message = msg.StepMessage
		}
		if msg.StepStatus == StepRunning {
			m.EnvState.CurrentStep = msg.StepIndex + 1
		}
	}

	// Handle robot updates
	if m.RobotState != nil {
		if msg.RobotStatus != "" {
			m.RobotState.Status = msg.RobotStatus
		}
		if msg.TaskName != "" {
			m.RobotState.TaskName = msg.TaskName
		}
	}

	// Handle output/logs
	if msg.OutputLine != "" {
		m.Logs.AddLine(msg.OutputLine)
		m.LogViewport.SetContent(m.Logs.Render(m.Styles, 100, true))
		m.LogViewport.GotoBottom()
	}
	if msg.Warning != "" {
		m.Logs.Add(LogWarn, "", msg.Warning)
	}
	if msg.Notice != "" {
		m.Logs.Add(LogInfo, "", msg.Notice)
	}
}

func (m *RootModel) View() string {
	if m.Width == 0 {
		return "Initializing..."
	}

	if m.Quitting {
		return m.renderFinal()
	}

	var sections []string

	// Header
	sections = append(sections, m.renderHeader())

	// Separator
	sections = append(sections, m.renderSeparator())

	// Main content based on mode
	switch m.Mode {
	case ModeEnvironment:
		sections = append(sections, m.renderEnvironment())
	case ModeRobotRun:
		sections = append(sections, m.renderRobotRun())
	case ModeRunComplete:
		sections = append(sections, m.renderRunComplete())
	case ModeDiagnostics:
		sections = append(sections, m.renderDiagnostics())
	case ModeDownload:
		sections = append(sections, m.renderDownload())
	}

	// Log section (if enabled)
	if m.ShowLogs && m.Logs.Len() > 0 {
		sections = append(sections, m.renderSeparator())
		sections = append(sections, m.renderLogSection())
	}

	// Footer
	sections = append(sections, m.renderSeparator())
	sections = append(sections, m.renderFooter())

	// Join and wrap in box
	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Center in terminal - use more width (up to 120 chars)
	boxWidth := minInt(m.Width-4, 120)
	if boxWidth < 60 {
		boxWidth = 60
	}
	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		m.Styles.Box.Width(boxWidth).Render(content),
	)
}

func (m *RootModel) renderHeader() string {
	// Left: Logo + Title
	logo := m.Styles.Title.Render("◆ RCC")
	version := m.Styles.Subtext.Render(m.Version)
	left := lipgloss.JoinHorizontal(lipgloss.Center, logo, " ", version)

	// Right: Context info
	var contextInfo string
	switch m.Mode {
	case ModeEnvironment:
		if m.EnvState != nil && m.EnvState.Name != "" {
			contextInfo = m.EnvState.Name
		} else {
			contextInfo = "Building Environment"
		}
	case ModeRobotRun:
		if m.RobotState != nil && m.RobotState.RobotName != "" {
			contextInfo = m.RobotState.RobotName
		} else {
			contextInfo = "Running Robot"
		}
	case ModeRunComplete:
		if m.RobotState != nil && m.RobotState.RobotName != "" {
			contextInfo = m.RobotState.RobotName
		} else {
			contextInfo = "Run Complete"
		}
	case ModeDiagnostics:
		contextInfo = "System Diagnostics"
	case ModeDownload:
		contextInfo = "Downloading"
	}
	right := m.Styles.Subtitle.Render(contextInfo)

	// Spacer
	availableWidth := m.contentWidth() - lipgloss.Width(left) - lipgloss.Width(right)
	if availableWidth < 1 {
		availableWidth = 1
	}
	spacer := strings.Repeat(" ", availableWidth)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, spacer, right)
}

func (m *RootModel) renderSeparator() string {
	width := m.contentWidth()
	if Iconic {
		return m.Styles.Label.Render(strings.Repeat("─", width))
	}
	return m.Styles.Label.Render(strings.Repeat("-", width))
}

func (m *RootModel) renderEnvironment() string {
	var b strings.Builder

	// Progress summary
	completed := 0
	total := len(m.EnvState.Steps)
	for _, step := range m.EnvState.Steps {
		if step.Status == StepComplete {
			completed++
		}
	}

	// Title with count
	title := fmt.Sprintf("Building Environment  %s", m.Styles.Subtext.Render(fmt.Sprintf("%d/%d", completed, total)))
	b.WriteString(m.Styles.Text.Bold(true).Render(title))
	b.WriteString("\n\n")

	// System context info in table format
	hasContext := m.EnvState.Username != "" || m.EnvState.Platform != "" || m.EnvState.Blueprint != ""
	if hasContext {
		labelWidth := 12
		formatLabel := func(label string) string {
			paddedLabel := label + strings.Repeat(" ", labelWidth-len(label))
			return m.Styles.Label.Render(paddedLabel)
		}

		// User row - cyan accent color
		if m.EnvState.Username != "" && m.EnvState.Hostname != "" {
			b.WriteString(formatLabel("User"))
			b.WriteString(m.Styles.Accent.Render(fmt.Sprintf("%s@%s", m.EnvState.Username, m.EnvState.Hostname)))
			b.WriteString("\n")
		}

		// Platform row - subtle color
		if m.EnvState.Platform != "" {
			platform := m.EnvState.Platform
			if m.EnvState.DistroID != "" {
				platform += " · " + m.EnvState.DistroID
				if m.EnvState.Release != "" {
					platform += " " + m.EnvState.Release
				}
			}
			b.WriteString(formatLabel("Platform"))
			b.WriteString(m.Styles.Subtext.Render(platform))
			b.WriteString("\n")
		}

		// Blueprint row - green/success color for the hash
		if m.EnvState.Blueprint != "" {
			b.WriteString(formatLabel("Blueprint"))
			b.WriteString(m.Styles.Success.Render(m.EnvState.Blueprint))
			b.WriteString("\n")
		}

		// Workers row - subtitle color
		if m.EnvState.Workers > 0 {
			b.WriteString(formatLabel("Workers"))
			b.WriteString(m.Styles.Subtitle.Render(fmt.Sprintf("%d workers / %d CPUs", m.EnvState.Workers, m.EnvState.CPUs)))
			b.WriteString("\n")
		}

		// Config file row - subtle color (skip if it's the same as blueprint hash)
		if m.EnvState.ConfigFile != "" && m.EnvState.ConfigFile != m.EnvState.Blueprint {
			b.WriteString(formatLabel("Config"))
			b.WriteString(m.Styles.Subtext.Render(m.EnvState.ConfigFile))
			b.WriteString("\n")
		}

		// Dev mode indicator
		if m.EnvState.DevMode {
			b.WriteString(m.Styles.Warning.Render("⚡ Developer Mode"))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// Steps - show all steps, but indicate completed ones compactly if many
	steps := m.EnvState.Steps

	// Render all steps with consistent formatting
	for i, step := range steps {
		icon := m.Styles.StepIcon(step.Status, m.Spinner.View())
		style := m.Styles.StepStyle(step.Status)

		// Step number and name with fixed-width alignment
		line := fmt.Sprintf("  %s %2d. %s", icon, i+1, step.Name)

		// Add message if present (only for running steps)
		if step.Message != "" && step.Status == StepRunning {
			// Truncate long messages
			msg := step.Message
			if len(msg) > 40 {
				msg = msg[:37] + "..."
			}
			line += m.Styles.Label.Render("  " + msg)
		}

		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	// Progress bar
	b.WriteString("\n")
	progress := float64(completed) / float64(maxInt(total, 1))
	progressBar := m.ProgressModel.ViewAs(progress)
	percentage := m.Styles.Subtext.Render(fmt.Sprintf(" %3.0f%%", progress*100))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, progressBar, percentage))

	// ETA
	elapsed := time.Since(m.StartTime)
	if completed > 0 && completed < total {
		avgTime := elapsed / time.Duration(completed)
		eta := avgTime * time.Duration(total-completed)
		b.WriteString("\n")
		b.WriteString(m.Styles.Label.Render(fmt.Sprintf("  ETA: %s", formatDurationShort(eta))))
	}

	return b.String()
}

func (m *RootModel) renderRobotRun() string {
	var b strings.Builder

	// Title
	b.WriteString(m.Styles.Text.Bold(true).Render("Running Robot"))
	b.WriteString("\n\n")

	// Table layout helper
	labelWidth := 12
	formatRow := func(label, value string) string {
		paddedLabel := label + strings.Repeat(" ", labelWidth-len(label))
		return m.Styles.Label.Render(paddedLabel) + value + "\n"
	}

	// Robot info section
	robotName := m.RobotState.RobotName
	if robotName == "" {
		robotName = "Unknown"
	}
	b.WriteString(formatRow("Robot", m.Styles.Text.Bold(true).Render(robotName)))

	taskName := m.RobotState.TaskName
	if taskName == "" {
		taskName = "-"
	}
	b.WriteString(formatRow("Task", m.Styles.Subtitle.Render(taskName)))

	// System info
	hostname := m.RobotState.Hostname
	if hostname == "" {
		hostname = "-"
	}
	b.WriteString(formatRow("Host", m.Styles.Text.Render(hostname)))

	if m.RobotState.Workers > 0 {
		b.WriteString(formatRow("Workers", m.Styles.Text.Render(fmt.Sprintf("%d", m.RobotState.Workers))))
	}

	b.WriteString("\n")

	// Status with spinner
	status := m.RobotState.Status
	if status == "" {
		status = "Executing..."
	}
	statusValue := m.Spinner.View() + " " + m.Styles.Accent.Render(status)
	b.WriteString(formatRow("Status", statusValue))

	// Timing info
	elapsed := time.Since(m.StartTime)

	if m.RobotState.BuildTime > 0 {
		b.WriteString(formatRow("Build Time", m.Styles.Subtext.Render(formatDurationShort(m.RobotState.BuildTime))))
	}

	b.WriteString(formatRow("Run Time", m.Styles.Success.Render(formatDurationShort(elapsed))))

	// Single line log preview - show the most recent output line
	if m.Logs != nil && m.Logs.Len() > 0 {
		entries := m.Logs.Recent(1)
		if len(entries) > 0 {
			lastLine := entries[0].Message
			// Truncate if too long
			maxWidth := m.contentWidth() - 4
			if len(lastLine) > maxWidth {
				lastLine = lastLine[:maxWidth-3] + "..."
			}
			b.WriteString("\n")
			b.WriteString(m.Styles.Subtext.Render("> " + lastLine))
		}
	}

	return b.String()
}

func (m *RootModel) renderRunComplete() string {
	var b strings.Builder

	// Title with status
	var titleIcon string
	var titleStyle lipgloss.Style
	if m.RobotState.Success {
		if Iconic {
			titleIcon = "✓ "
		} else {
			titleIcon = "[OK] "
		}
		titleStyle = m.Styles.Success
	} else {
		if Iconic {
			titleIcon = "✗ "
		} else {
			titleIcon = "[FAIL] "
		}
		titleStyle = m.Styles.Error
	}
	b.WriteString(titleStyle.Bold(true).Render(titleIcon + "Run Complete"))
	b.WriteString("\n\n")

	// Summary info
	labelWidth := 12
	formatRow := func(label, value string) string {
		paddedLabel := label + strings.Repeat(" ", labelWidth-len(label))
		return m.Styles.Label.Render(paddedLabel) + value + "\n"
	}

	robotName := m.RobotState.RobotName
	if robotName == "" {
		robotName = "Unknown"
	}
	b.WriteString(formatRow("Robot", m.Styles.Text.Bold(true).Render(robotName)))

	taskName := m.RobotState.TaskName
	if taskName != "" {
		b.WriteString(formatRow("Task", m.Styles.Subtitle.Render(taskName)))
	}

	// Timing
	if m.RobotState.BuildTime > 0 {
		b.WriteString(formatRow("Build", m.Styles.Subtext.Render(formatDurationShort(m.RobotState.BuildTime))))
	}
	b.WriteString(formatRow("Run Time", m.Styles.Success.Render(formatDurationShort(m.RobotState.RunTime))))

	// Exit code if failed
	if !m.RobotState.Success && m.RobotState.ExitCode != 0 {
		b.WriteString(formatRow("Exit Code", m.Styles.Error.Render(fmt.Sprintf("%d", m.RobotState.ExitCode))))
	}

	b.WriteString("\n")

	// Log viewer section
	b.WriteString(m.Styles.Subtext.Render(strings.Repeat("─", m.contentWidth())))
	b.WriteString("\n")

	// Calculate visible log area
	visibleLines := m.Height - 20 // Reserve space for header/footer
	if visibleLines < 5 {
		visibleLines = 5
	}
	if visibleLines > 30 {
		visibleLines = 30
	}

	logLines := m.RobotState.LogLines
	if len(logLines) == 0 {
		b.WriteString(m.Styles.Subtext.Render("  No log output available"))
		b.WriteString("\n")
	} else {
		// Calculate scroll bounds
		maxScroll := len(logLines) - visibleLines
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.RobotState.LogScroll > maxScroll {
			m.RobotState.LogScroll = maxScroll
		}
		if m.RobotState.LogScroll < 0 {
			m.RobotState.LogScroll = 0
		}

		// Render visible lines
		start := m.RobotState.LogScroll
		end := start + visibleLines
		if end > len(logLines) {
			end = len(logLines)
		}

		for i := start; i < end; i++ {
			line := logLines[i]
			// Color based on content
			var lineStyle lipgloss.Style
			if strings.Contains(line, "[PASS]") || strings.Contains(line, "PASS") {
				lineStyle = m.Styles.Success
			} else if strings.Contains(line, "[FAIL]") || strings.Contains(line, "FAIL") {
				lineStyle = m.Styles.Error
			} else if strings.HasPrefix(line, "===") {
				lineStyle = m.Styles.Text.Bold(true)
			} else if strings.HasPrefix(line, "  >") {
				lineStyle = m.Styles.Accent
			} else {
				lineStyle = m.Styles.Text
			}
			b.WriteString(lineStyle.Render(line))
			b.WriteString("\n")
		}

		// Scroll indicator
		if len(logLines) > visibleLines {
			scrollPos := fmt.Sprintf(" [%d-%d of %d] ", start+1, end, len(logLines))
			scrollHint := ""
			if m.RobotState.LogScroll > 0 {
				scrollHint += "↑"
			}
			if m.RobotState.LogScroll < maxScroll {
				scrollHint += "↓"
			}
			b.WriteString(m.Styles.Subtext.Render(scrollPos + scrollHint))
		}
	}

	return b.String()
}

func (m *RootModel) renderDiagnostics() string {
	return m.Styles.Text.Render("Running diagnostics...")
}

func (m *RootModel) renderDownload() string {
	return m.Styles.Text.Render("Downloading...")
}

func (m *RootModel) renderLogSection() string {
	var b strings.Builder

	// Header with stats
	stats := m.Logs.FormatStats(m.Styles)
	header := "Logs"
	if stats != "" {
		header += "  " + stats
	}
	b.WriteString(m.Styles.Label.Render(header))
	b.WriteString("\n")

	// Log content
	logContent := m.Logs.Render(m.Styles, m.LogViewport.Height, true)
	b.WriteString(logContent)

	return b.String()
}

func (m *RootModel) renderFooter() string {
	// Left: Spinner + status
	var status string
	switch m.Mode {
	case ModeEnvironment:
		completed := 0
		for _, step := range m.EnvState.Steps {
			if step.Status == StepComplete {
				completed++
			}
		}
		if completed == len(m.EnvState.Steps) && len(m.EnvState.Steps) > 0 {
			status = m.Styles.Success.Render("● Complete")
		} else {
			status = m.Spinner.View() + " " + m.Styles.Text.Render("Building...")
		}
	case ModeRobotRun:
		status = m.Spinner.View() + " " + m.Styles.Text.Render("Running...")
	case ModeRunComplete:
		if m.RobotState.Success {
			status = m.Styles.Success.Render("● Finished")
		} else {
			status = m.Styles.Error.Render("● Failed")
		}
	default:
		status = m.Spinner.View() + " " + m.Styles.Text.Render("Working...")
	}

	// Right: Key hints
	hints := []string{}
	if m.Mode == ModeRunComplete {
		hints = append(hints, m.formatKeyHint("↑↓", "scroll"))
		hints = append(hints, m.formatKeyHint("g/G", "top/bottom"))
		hints = append(hints, m.formatKeyHint("esc", "back"))
	} else {
		hints = append(hints, m.formatKeyHint("q", "quit"))
		if m.Logs.Len() > 0 {
			if m.ShowLogs {
				hints = append(hints, m.formatKeyHint("l", "hide logs"))
				hints = append(hints, m.formatKeyHint("↑↓", "scroll"))
			} else {
				hints = append(hints, m.formatKeyHint("l", "show logs"))
			}
		}
	}
	hintsStr := strings.Join(hints, "  ")

	// Spacer
	availableWidth := m.contentWidth() - lipgloss.Width(status) - lipgloss.Width(hintsStr)
	if availableWidth < 1 {
		availableWidth = 1
	}
	spacer := strings.Repeat(" ", availableWidth)

	return lipgloss.JoinHorizontal(lipgloss.Top, status, spacer, hintsStr)
}

func (m *RootModel) formatKeyHint(key, action string) string {
	return m.Styles.KeyHint.Render(key) + " " + m.Styles.Label.Render(action)
}

func (m *RootModel) renderFinal() string {
	var b strings.Builder

	elapsed := time.Since(m.StartTime)

	if m.Success {
		if Iconic {
			b.WriteString(m.Styles.Success.Render("✓ "))
		} else {
			b.WriteString(m.Styles.Success.Render("[OK] "))
		}
		b.WriteString(m.Styles.Text.Render(fmt.Sprintf("Completed in %s", formatDurationShort(elapsed))))
	} else {
		if Iconic {
			b.WriteString(m.Styles.Error.Render("✗ "))
		} else {
			b.WriteString(m.Styles.Error.Render("[FAIL] "))
		}
		b.WriteString(m.Styles.Text.Render(fmt.Sprintf("Failed after %s", formatDurationShort(elapsed))))
	}

	// Show error count if any
	stats := m.Logs.Stats()
	if stats.Errors > 0 {
		b.WriteString("\n")
		b.WriteString(m.Styles.Error.Render(fmt.Sprintf("  %d errors occurred", stats.Errors)))
	}

	b.WriteString("\n")
	return b.String()
}

func (m *RootModel) contentWidth() int {
	width := m.Width - 10 // Account for box borders and padding
	if width < 50 {
		width = 50
	}
	if width > 116 {
		width = 116
	}
	return width
}

// Helper functions

func formatDurationShort(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	} else if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
