package interactive

// UnifiedDashboard provides a seamless Bubble Tea dashboard that handles both
// environment build and robot execution phases in a single terminal session.
// This eliminates the output gap between separate dashboards.

import (
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joshyorko/rcc/common"
)

// UnifiedDashboard is a single Bubble Tea dashboard that handles all phases
type UnifiedDashboard struct {
	mu         sync.Mutex
	program    *tea.Program
	model      *RootModel
	running    bool
	startTime  time.Time
	updateChan chan UnifiedUpdateMsg

	// Keep alive flag for transitioning between phases
	keepAlive bool
}

// Global unified dashboard instance for the current operation
var globalUnifiedDashboard *UnifiedDashboard
var globalUnifiedMu sync.Mutex

// GetUnifiedDashboard returns the global unified dashboard if one exists
func GetUnifiedDashboard() *UnifiedDashboard {
	globalUnifiedMu.Lock()
	defer globalUnifiedMu.Unlock()
	return globalUnifiedDashboard
}

// GetOrCreateUnifiedDashboard returns the existing unified dashboard or creates a new one
// This is the main entry point for creating the dashboard at the start of an operation
func GetOrCreateUnifiedDashboard() *UnifiedDashboard {
	globalUnifiedMu.Lock()
	if globalUnifiedDashboard != nil && globalUnifiedDashboard.running {
		globalUnifiedMu.Unlock()
		return globalUnifiedDashboard
	}
	globalUnifiedMu.Unlock()

	// Default environment build steps
	envSteps := []string{
		"Context verification",
		"Holotree lock acquisition",
		"Blueprint composition",
		"Blueprint validation",
		"Remote catalog check",
		"Holotree stage preparation",
		"Environment build",
		"Partial environment restore",
		"Micromamba phase",
		"Pip/UV install phase",
		"Post-install scripts",
		"Activate environment",
		"Pip check",
		"Record to hololib",
		"Restore space / Finalize",
	}

	dashboard := NewUnifiedDashboard(envSteps)
	if dashboard != nil {
		dashboard.Start()
	}
	return dashboard
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
	// Check if dashboards should be used - defer to caller to check ShouldUseDashboard
	// This allows the interactive package to be self-contained

	model := NewRootModel()
	model.EnvState.Steps = make([]EnvStep, len(envSteps))
	for i, name := range envSteps {
		model.EnvState.Steps[i] = EnvStep{Name: name, Status: StepPending}
	}

	dashboard := &UnifiedDashboard{
		model:      model,
		startTime:  time.Now(),
		updateChan: make(chan UnifiedUpdateMsg, 100),
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
	SetDashboardActive(true)

	// Intercept all log output and route to dashboard
	common.SetLogInterceptor(func(message string) bool {
		message = strings.TrimSpace(message)
		if message == "" {
			return true
		}

		// Route to appropriate handler
		if strings.Contains(message, "Warning:") {
			d.AddWarning(strings.TrimPrefix(message, "Warning: "))
		} else if strings.Contains(message, "Note:") {
			d.AddNotice(strings.TrimPrefix(message, "Note: "))
		} else {
			// Add to log buffer
			d.AddOutput(message)
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
	for update := range d.updateChan {
		if d.program != nil {
			d.program.Send(update)
		}
	}
}

// Stop terminates the dashboard
func (d *UnifiedDashboard) Stop(success bool) {
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

	// Update model with final status
	if d.model != nil {
		d.model.Success = success
		d.model.Quitting = true
	}

	// Clear log interceptor
	common.ClearLogInterceptor()
	SetDashboardActive(false)

	// Clear global reference
	globalUnifiedMu.Lock()
	globalUnifiedDashboard = nil
	globalUnifiedMu.Unlock()

	// Send quit message and wait briefly for final render
	if d.program != nil {
		d.program.Quit()
		time.Sleep(50 * time.Millisecond)
	}

	// Close update channel
	close(d.updateChan)
}

// Update implements Dashboard interface
func (d *UnifiedDashboard) Update(state DashboardState) {
	if d == nil || d.model == nil {
		return
	}

	if state.Message != "" {
		select {
		case d.updateChan <- UnifiedUpdateMsg{RobotStatus: state.Message}:
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
	case d.updateChan <- UnifiedUpdateMsg{
		StepIndex:   index,
		StepStatus:  status,
		StepMessage: message,
	}:
	default:
	}
}

// AddOutput adds output to the log buffer
func (d *UnifiedDashboard) AddOutput(line string) {
	if d == nil || d.model == nil {
		return
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	select {
	case d.updateChan <- UnifiedUpdateMsg{OutputLine: line}:
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
	case d.updateChan <- UnifiedUpdateMsg{Warning: warning}:
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
	case d.updateChan <- UnifiedUpdateMsg{Notice: notice}:
	default:
	}
}

// TransitionToRobotPhase switches the dashboard to robot execution mode
func (d *UnifiedDashboard) TransitionToRobotPhase(robotName, taskName string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	// Record build time before transitioning
	d.model.RobotState.BuildTime = time.Since(d.model.StartTime)
	// Reset start time for robot run timing
	d.model.StartTime = time.Now()
	// Switch to robot mode
	d.model.Mode = ModeRobotRun
	d.model.RobotState.RobotName = robotName
	d.model.RobotState.TaskName = taskName
	d.mu.Unlock()

	// Also send through update channel for the tea program
	phase := ModeRobotRun
	select {
	case d.updateChan <- UnifiedUpdateMsg{
		PhaseChange: &phase,
		TaskName:    taskName,
	}:
	default:
	}
}

// SetRobotStatus updates the robot execution status
func (d *UnifiedDashboard) SetRobotStatus(status string) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.updateChan <- UnifiedUpdateMsg{RobotStatus: status}:
	default:
	}
}

// SetTaskName updates the current task name
func (d *UnifiedDashboard) SetTaskName(taskName string) {
	if d == nil || d.model == nil {
		return
	}

	select {
	case d.updateChan <- UnifiedUpdateMsg{TaskName: taskName}:
	default:
	}
}

// SetEnvironmentName sets the environment name being built
func (d *UnifiedDashboard) SetEnvironmentName(name string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	d.model.EnvState.Name = name
	d.mu.Unlock()
}

// GetModel returns the underlying model (for advanced usage)
func (d *UnifiedDashboard) GetModel() *RootModel {
	if d == nil {
		return nil
	}
	return d.model
}

// IsRunning returns whether the dashboard is currently active
func (d *UnifiedDashboard) IsRunning() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

// SetVersion updates the version display
func (d *UnifiedDashboard) SetVersion(version string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	d.model.Version = version
	d.mu.Unlock()
}

// SetSystemContext sets the system context information for display
func (d *UnifiedDashboard) SetSystemContext(username, hostname, platform, distroID, distroDesc, release string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	d.model.EnvState.Username = username
	d.model.EnvState.Hostname = hostname
	d.model.EnvState.Platform = platform
	d.model.EnvState.DistroID = distroID
	d.model.EnvState.DistroDesc = distroDesc
	d.model.EnvState.Release = release
	d.mu.Unlock()
}

// SetBuildInfo sets the build configuration information
func (d *UnifiedDashboard) SetBuildInfo(blueprint string, workers, cpus int, configFile string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	d.model.EnvState.Blueprint = blueprint
	d.model.EnvState.Workers = workers
	d.model.EnvState.CPUs = cpus
	d.model.EnvState.ConfigFile = configFile
	d.mu.Unlock()
}

// SetHolotreeID sets the holotree environment ID
func (d *UnifiedDashboard) SetHolotreeID(id string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	d.model.EnvState.HolotreeID = id
	d.mu.Unlock()
}

// SetDevMode sets whether developer mode is active
func (d *UnifiedDashboard) SetDevMode(devMode bool) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	d.model.EnvState.DevMode = devMode
	d.mu.Unlock()
}

// TransitionToRunComplete switches the dashboard to run complete mode
// This shows the final results with scrollable logs
func (d *UnifiedDashboard) TransitionToRunComplete(success bool, exitCode int, artifactsDir string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	// Record run time
	d.model.RobotState.RunTime = time.Since(d.model.StartTime)
	d.model.RobotState.Success = success
	d.model.RobotState.ExitCode = exitCode
	d.model.RobotState.ArtifactsDir = artifactsDir

	// Parse log files for display
	if artifactsDir != "" {
		if logLines, err := ParseLogHTML(artifactsDir); err == nil {
			d.model.RobotState.LogLines = logLines
		}
	}

	// If no parsed logs, use the buffered logs
	if len(d.model.RobotState.LogLines) == 0 && d.model.Logs != nil {
		entries := d.model.Logs.Recent(500)
		for _, e := range entries {
			d.model.RobotState.LogLines = append(d.model.RobotState.LogLines, e.Message)
		}
	}

	d.model.RobotState.LogScroll = 0
	d.model.Mode = ModeRunComplete
	d.mu.Unlock()

	// Send phase change through update channel
	phase := ModeRunComplete
	select {
	case d.updateChan <- UnifiedUpdateMsg{PhaseChange: &phase}:
	default:
	}
}

// SetArtifactsDir sets the artifacts directory path
func (d *UnifiedDashboard) SetArtifactsDir(dir string) {
	if d == nil || d.model == nil {
		return
	}

	d.mu.Lock()
	d.model.RobotState.ArtifactsDir = dir
	d.mu.Unlock()
}
