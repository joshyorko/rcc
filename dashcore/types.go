// Package dashcore provides shared dashboard types used by both pretty and interactive packages.
// This package breaks the import cycle by providing common types that both can import.
package dashcore

import (
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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

// SetDashboardActive increments or decrements the active dashboard counter
func SetDashboardActive(active bool) {
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

// Iconic controls whether to use Unicode icons or ASCII fallback
// This should be set by the pretty package during Setup()
var Iconic = true

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

// BaseDashboard provides common functionality for dashboard implementations
type BaseDashboard struct {
	Running   bool
	Mu        sync.Mutex
	StopChan  chan struct{}
	DoneChan  chan struct{}
	StartTime time.Time
	State     DashboardState
}

// NewBaseDashboard creates a new base dashboard with initialized channels
func NewBaseDashboard() BaseDashboard {
	return BaseDashboard{
		Running:   false,
		StopChan:  make(chan struct{}),
		DoneChan:  make(chan struct{}),
		StartTime: time.Now(),
		State: DashboardState{
			StartTime: time.Now(),
			Steps:     []StepState{},
			Output:    []string{},
		},
	}
}

// SetupDashboardSignals registers signal handlers for graceful shutdown
// This ensures the dashboard cleans up properly on Ctrl+C or termination
func SetupDashboardSignals(cleanup func()) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cleanup()
		os.Exit(1)
	}()
}

// StartRenderLoop begins the dashboard render loop at 20fps (50ms cycle)
func (b *BaseDashboard) StartRenderLoop(renderFunc func()) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-b.StopChan:
			close(b.DoneChan)
			return
		case <-ticker.C:
			renderFunc()
		}
	}
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

// DashboardMode represents the current active view
type DashboardMode int

const (
	ModeEnvironment DashboardMode = iota
	ModeRobotRun
	ModeRunComplete // Post-run view showing logs and results
	ModeDiagnostics
	ModeDownload
)

func (m DashboardMode) String() string {
	switch m {
	case ModeEnvironment:
		return "Environment"
	case ModeRobotRun:
		return "Robot"
	case ModeRunComplete:
		return "Complete"
	case ModeDiagnostics:
		return "Diagnostics"
	case ModeDownload:
		return "Download"
	default:
		return "Unknown"
	}
}

// EnvState holds environment build state
type EnvState struct {
	Name        string
	Steps       []EnvStep
	CurrentStep int

	// System context
	Username   string
	Hostname   string
	Platform   string
	DistroID   string
	DistroDesc string
	Release    string

	// Build info
	Blueprint  string
	Workers    int
	CPUs       int
	ConfigFile string
	HolotreeID string
	DevMode    bool
}

// RobotState holds robot execution state
type RobotState struct {
	RobotName  string
	TaskName   string
	Status     string
	ExitCode   int
	Hostname   string
	Controller string
	Workers    int
	BuildTime  time.Duration
	RunTime    time.Duration

	// Run complete state
	ArtifactsDir string   // Path to output/artifacts directory
	LogLines     []string // Parsed log content for terminal display
	LogScroll    int      // Current scroll position in log view
	Success      bool     // Whether the run succeeded
}

// EnvStep represents a single environment build step
type EnvStep struct {
	Name    string
	Status  StepStatus
	Message string
	Elapsed time.Duration
}

// UnifiedUpdateMsg is the message type for unified dashboard updates
type UnifiedUpdateMsg struct {
	// Environment updates
	StepIndex   int
	StepStatus  StepStatus
	StepMessage string

	// Robot updates
	RobotStatus string
	TaskName    string
	OutputLine  string

	// Shared updates
	Warning     string
	Notice      string
	PhaseChange *DashboardMode
}
