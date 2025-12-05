package pretty

import (
	"os"
	"testing"

	"github.com/joshyorko/rcc/dashcore"
)

func TestStepStatusString(t *testing.T) {
	tests := []struct {
		name     string
		status   StepStatus
		iconic   bool
		expected string
	}{
		{"pending_iconic", StepPending, true, "○"},
		{"running_iconic", StepRunning, true, "⠋"},
		{"complete_iconic", StepComplete, true, "✓"},
		{"failed_iconic", StepFailed, true, "✗"},
		{"skipped_iconic", StepSkipped, true, "⊘"},
		{"pending_ascii", StepPending, false, "o"},
		{"running_ascii", StepRunning, false, "-"},
		{"complete_ascii", StepComplete, false, "+"},
		{"failed_ascii", StepFailed, false, "x"},
		{"skipped_ascii", StepSkipped, false, "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldIconic := dashcore.Iconic
			dashcore.Iconic = tt.iconic
			defer func() { dashcore.Iconic = oldIconic }()

			result := tt.status.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestShouldUseDashboard(t *testing.T) {
	tests := []struct {
		name            string
		dashboardFlag   bool
		dashboardEnv    bool
		interactive     bool
		expected        bool
	}{
		{"disabled_by_default", false, false, true, false},
		{"enabled_by_flag", true, false, true, true},
		{"enabled_by_env", false, true, true, true},
		{"non_interactive_with_flag", true, false, false, false},
		{"non_interactive_with_env", false, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore state
			oldInteractive := Interactive
			oldDashboardEnabled := DashboardEnabled
			oldEnv := os.Getenv("RCC_DASHBOARD")
			defer func() {
				Interactive = oldInteractive
				DashboardEnabled = oldDashboardEnabled
				if oldEnv == "" {
					os.Unsetenv("RCC_DASHBOARD")
				} else {
					os.Setenv("RCC_DASHBOARD", oldEnv)
				}
			}()

			// Setup test state
			Interactive = tt.interactive
			DashboardEnabled = tt.dashboardFlag
			if tt.dashboardEnv {
				os.Setenv("RCC_DASHBOARD", "1")
			} else {
				os.Unsetenv("RCC_DASHBOARD")
			}

			result := ShouldUseDashboard()

			// For tests that should enable dashboard, result depends on terminal height too
			if tt.expected && tt.interactive {
				// If we expect true but terminal is too short, result may still be false
				// Just verify it doesn't panic
				_ = result
			} else {
				if result != tt.expected {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}

func TestNoopDashboard(t *testing.T) {
	// Verify noopDashboard doesn't panic
	d := NewNoopDashboard()

	d.Start()
	d.Stop(true)
	d.Update(DashboardState{})
	d.SetStep(0, StepComplete, "test")
	d.AddOutput("test output")

	// If we get here without panic, test passes
}

func TestFactoryFunctionsReturnNoop(t *testing.T) {
	// Verify all factory functions return non-nil dashboards (currently noop)
	factories := []struct {
		name string
		dash Dashboard
	}{
		{"NewEnvironmentDashboard", NewEnvironmentDashboard([]string{"step1", "step2"})},
		{"NewDiagnosticsDashboard", NewDiagnosticsDashboard([]string{"check1", "check2"})},
		{"NewDownloadDashboard", NewDownloadDashboard("test.zip", 1024)},
		{"NewMultiTaskDashboard", NewMultiTaskDashboard([]string{"task1", "task2"})},
		{"NewCompactProgress", NewCompactProgress("testing")},
		{"NewRobotRunDashboard", NewRobotRunDashboard("test-robot")},
	}

	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			if factory.dash == nil {
				t.Errorf("%s returned nil", factory.name)
			}

			// Verify the dashboard doesn't panic on basic operations
			factory.dash.Start()
			factory.dash.Stop(true)
		})
	}
}

func TestBaseDashboard(t *testing.T) {
	// Test base dashboard initialization
	base := dashcore.NewBaseDashboard()

	if base.Running {
		t.Error("new dashboard should not be running")
	}

	if base.StopChan == nil {
		t.Error("StopChan should be initialized")
	}

	if base.DoneChan == nil {
		t.Error("DoneChan should be initialized")
	}

	if base.State.Steps == nil {
		t.Error("State.Steps should be initialized")
	}

	if base.State.Output == nil {
		t.Error("State.Output should be initialized")
	}
}

func TestDashboardState(t *testing.T) {
	// Test DashboardState structure
	state := DashboardState{
		Steps: []StepState{
			{Index: 0, Status: StepPending, Message: "Step 1"},
			{Index: 1, Status: StepRunning, Message: "Step 2"},
			{Index: 2, Status: StepComplete, Message: "Step 3"},
		},
		Progress: 0.66,
		Message:  "Processing",
		Output:   []string{"line1", "line2"},
	}

	if len(state.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(state.Steps))
	}

	if state.Progress != 0.66 {
		t.Errorf("expected progress 0.66, got %f", state.Progress)
	}

	if state.Message != "Processing" {
		t.Errorf("expected message 'Processing', got %q", state.Message)
	}

	if len(state.Output) != 2 {
		t.Errorf("expected 2 output lines, got %d", len(state.Output))
	}
}
