package pretty

// Bubble Tea based dashboard for RCC environment builds
// Provides a clean, modern UI that completely owns the terminal during operation

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
	"github.com/joshyorko/rcc/dashcore"
)

// TeaEnvironmentDashboard is a Bubble Tea-based dashboard for environment builds
type TeaEnvironmentDashboard struct {
	mu        sync.Mutex
	program   *tea.Program
	model     *envDashboardModel
	running   bool
	startTime time.Time
}

// envDashboardModel is the Bubble Tea model for the environment dashboard
type envDashboardModel struct {
	steps         []stepInfo
	spinner       spinner.Model
	progress      teaprogress.Model
	width         int
	height        int
	startTime     time.Time
	currentStep   int
	quitting      bool
	success       bool
	title         string
	updateChan    chan stepUpdate
}

type stepInfo struct {
	name    string
	status  StepStatus
	message string
}

type stepUpdate struct {
	index   int
	status  StepStatus
	message string
}

// Messages for Bubble Tea
type tickMsg time.Time
type stepUpdateMsg stepUpdate
type quitMsg struct{ success bool }

// Styles using lipgloss
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	stepPendingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	stepRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("39"))

	stepCompleteStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("42"))

	stepFailedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	progressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	etaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true)
)

// NewTeaEnvironmentDashboard creates a new Bubble Tea-based environment dashboard
func NewTeaEnvironmentDashboard(steps []string) *TeaEnvironmentDashboard {
	if !ShouldUseDashboard() {
		return nil
	}

	stepInfos := make([]stepInfo, len(steps))
	for i, name := range steps {
		stepInfos[i] = stepInfo{name: name, status: StepPending}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	p := teaprogress.New(
		teaprogress.WithDefaultGradient(),
		teaprogress.WithWidth(50),
		teaprogress.WithoutPercentage(),
	)

	model := &envDashboardModel{
		steps:      stepInfos,
		spinner:    s,
		progress:   p,
		startTime:  time.Now(),
		title:      "RCC Environment Build",
		updateChan: make(chan stepUpdate, 100),
	}

	return &TeaEnvironmentDashboard{
		model:     model,
		startTime: time.Now(),
	}
}

// Start begins the dashboard
func (d *TeaEnvironmentDashboard) Start() {
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
	dashcore.SetDashboardActive(true)

	// Suppress all log output during dashboard operation
	common.SetLogInterceptor(func(message string) bool {
		return true // Suppress all
	})

	// Start Bubble Tea program
	d.program = tea.NewProgram(d.model, tea.WithAltScreen())

	go func() {
		if _, err := d.program.Run(); err != nil {
			common.Error("dashboard", err)
		}
	}()

	// Start update listener
	go d.listenForUpdates()
}

func (d *TeaEnvironmentDashboard) listenForUpdates() {
	for update := range d.model.updateChan {
		if d.program != nil {
			d.program.Send(stepUpdateMsg(update))
		}
	}
}

// Stop terminates the dashboard
func (d *TeaEnvironmentDashboard) Stop(success bool) {
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
	dashcore.SetDashboardActive(false)

	// Send quit message
	if d.program != nil {
		d.program.Send(quitMsg{success: success})
		// Give it time to render final state
		time.Sleep(100 * time.Millisecond)
		d.program.Quit()
	}

	close(d.model.updateChan)
}

// Update implements Dashboard interface
func (d *TeaEnvironmentDashboard) Update(state DashboardState) {
	// Not used for this implementation
}

// SetStep updates a step's status
func (d *TeaEnvironmentDashboard) SetStep(index int, status StepStatus, message string) {
	if d == nil || d.model == nil {
		return
	}

	if index >= 0 && index < len(d.model.steps) {
		select {
		case d.model.updateChan <- stepUpdate{index: index, status: status, message: message}:
		default:
			// Channel full, skip update
		}
	}
}

// AddOutput is a no-op - we suppress all output
func (d *TeaEnvironmentDashboard) AddOutput(line string) {
	// No-op
}

// Bubble Tea methods for the model

func (m *envDashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *envDashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.progress.Width = min(msg.Width-20, 50)

	case tickMsg:
		return m, tickCmd()

	case stepUpdateMsg:
		if msg.index >= 0 && msg.index < len(m.steps) {
			m.steps[msg.index].status = msg.status
			m.steps[msg.index].message = msg.message
			if msg.status == StepRunning {
				m.currentStep = msg.index
			}
		}

	case quitMsg:
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

func (m *envDashboardModel) View() string {
	if m.quitting {
		return m.renderFinal()
	}

	var b strings.Builder

	// Count completed steps
	completed := 0
	for _, step := range m.steps {
		if step.status == StepComplete {
			completed++
		}
	}

	// Title with progress counter
	title := fmt.Sprintf("%s  %d/%d", m.title, completed, len(m.steps))
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// Steps
	for i, step := range m.steps {
		b.WriteString(m.renderStep(i, step))
		b.WriteString("\n")
	}

	// Progress bar
	b.WriteString("\n")
	progress := float64(completed) / float64(len(m.steps))
	b.WriteString(progressStyle.Render(m.progress.ViewAs(progress)))

	// ETA
	elapsed := time.Since(m.startTime)
	if completed > 0 && completed < len(m.steps) {
		avgTime := elapsed / time.Duration(completed)
		remaining := avgTime * time.Duration(len(m.steps)-completed)
		eta := fmt.Sprintf("  ETA: %s", formatDuration(remaining))
		b.WriteString(etaStyle.Render(eta))
	} else if completed == 0 {
		b.WriteString(etaStyle.Render("  Calculating..."))
	}

	b.WriteString("\n")

	// Wrap in box
	return boxStyle.Render(b.String())
}

func (m *envDashboardModel) renderStep(index int, step stepInfo) string {
	var icon string
	var style lipgloss.Style

	switch step.status {
	case StepPending:
		icon = "○"
		style = stepPendingStyle
	case StepRunning:
		icon = m.spinner.View()
		style = stepRunningStyle
	case StepComplete:
		icon = "●"
		style = stepCompleteStyle
	case StepFailed:
		icon = "●"
		style = stepFailedStyle
	case StepSkipped:
		icon = "○"
		style = stepPendingStyle
	}

	text := fmt.Sprintf("%s %2d. %s", icon, index+1, step.name)
	if step.message != "" {
		text += "  " + step.message
	}

	return style.Render(text)
}

func (m *envDashboardModel) renderFinal() string {
	var b strings.Builder

	completed := 0
	failed := 0
	for _, step := range m.steps {
		if step.status == StepComplete {
			completed++
		} else if step.status == StepFailed {
			failed++
		}
	}

	elapsed := time.Since(m.startTime)

	if m.success {
		b.WriteString(stepCompleteStyle.Render(fmt.Sprintf("✓ Environment build completed in %s", formatDuration(elapsed))))
	} else {
		b.WriteString(stepFailedStyle.Render(fmt.Sprintf("✗ Environment build failed after %s", formatDuration(elapsed))))
	}

	return b.String()
}

func formatDuration(d time.Duration) string {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
