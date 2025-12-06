package interactive

// Dashboard framework for RCC's interactive UI engine
//
// This package provides the core dashboard system with interface and common functionality
// for displaying interactive, real-time progress information in the terminal.
//
// Architecture:
// - Dashboard interface: Common contract for all dashboard implementations
// - StepStatus: Enumeration for step states with visual representations
// - DashboardState: Shared state structure across dashboard types

import (
	"github.com/joshyorko/rcc/dashcore"
)

// Type aliases for dashboard core types
type Dashboard = dashcore.Dashboard
type StepStatus = dashcore.StepStatus
type StepState = dashcore.StepState
type DashboardState = dashcore.DashboardState
type DashboardMode = dashcore.DashboardMode
type EnvState = dashcore.EnvState
type RobotState = dashcore.RobotState
type EnvStep = dashcore.EnvStep
type UnifiedUpdateMsg = dashcore.UnifiedUpdateMsg

// Step status constants
const (
	StepPending  = dashcore.StepPending
	StepRunning  = dashcore.StepRunning
	StepComplete = dashcore.StepComplete
	StepFailed   = dashcore.StepFailed
	StepSkipped  = dashcore.StepSkipped
)

// Dashboard mode constants
const (
	ModeEnvironment = dashcore.ModeEnvironment
	ModeRobotRun    = dashcore.ModeRobotRun
	ModeRunComplete = dashcore.ModeRunComplete
	ModeDiagnostics = dashcore.ModeDiagnostics
	ModeDownload    = dashcore.ModeDownload
)

// Type aliases for internal types
type baseDashboard = dashcore.BaseDashboard

// Function aliases
var newBaseDashboard = dashcore.NewBaseDashboard
var setupDashboardSignals = dashcore.SetupDashboardSignals

// IsDashboardActive returns true if any dashboard is currently rendering
var IsDashboardActive = dashcore.IsDashboardActive

// SetDashboardActive increments or decrements the active dashboard counter
var SetDashboardActive = dashcore.SetDashboardActive

// SetKeepDashboardAlive sets whether the dashboard should remain active after Stop()
var SetKeepDashboardAlive = dashcore.SetKeepDashboardAlive

// IsKeepDashboardAlive returns true if the dashboard should remain active
var IsKeepDashboardAlive = dashcore.IsKeepDashboardAlive

// NewNoopDashboard returns a no-op dashboard implementation that does nothing.
// Use this when you need a Dashboard interface but don't want any visual output.
func NewNoopDashboard() Dashboard {
	return dashcore.NewNoopDashboard()
}
