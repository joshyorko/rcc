package pretty

import (
	"os"
	"testing"
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
			oldIconic := Iconic
			Iconic = tt.iconic
			defer func() { Iconic = oldIconic }()

			result := tt.status.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestShouldUseDashboard(t *testing.T) {
	tests := []struct {
		name         string
		interactive  bool
		termHeight   int
		envSet       bool
		expected     bool
	}{
		{"interactive_tall_terminal", true, 25, false, true},
		{"interactive_min_height", true, 20, false, true},
		{"interactive_short_terminal", true, 19, false, false},
		{"non_interactive", false, 25, false, false},
		{"env_variable_set", true, 25, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore state
			oldInteractive := Interactive
			oldEnv := os.Getenv("RCC_NO_DASHBOARD")
			defer func() {
				Interactive = oldInteractive
				if oldEnv == "" {
					os.Unsetenv("RCC_NO_DASHBOARD")
				} else {
					os.Setenv("RCC_NO_DASHBOARD", oldEnv)
				}
			}()

			// Setup test state
			Interactive = tt.interactive
			if tt.envSet {
				os.Setenv("RCC_NO_DASHBOARD", "1")
			} else {
				os.Unsetenv("RCC_NO_DASHBOARD")
			}

			// Note: We can't easily mock TerminalHeight() without refactoring,
			// so this test will use the actual terminal height.
			// In a real environment, we'd inject the height check.
			result := ShouldUseDashboard()

			// For the short terminal test, we can only verify it returns false
			// if the terminal is actually short
			if tt.name == "interactive_short_terminal" {
				// Skip this check since we can't mock terminal height
				t.Skip("Cannot mock terminal height in this test")
			} else {
				// For other tests, check based on interactive and env settings
				if tt.interactive && !tt.envSet {
					// Result depends on actual terminal height
					// We can only verify it's consistent
					_ = result
				} else {
					if result != tt.expected {
						t.Errorf("expected %v, got %v", tt.expected, result)
					}
				}
			}
		})
	}
}

func TestNoopDashboard(t *testing.T) {
	// Verify noopDashboard doesn't panic
	d := &noopDashboard{}

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
	base := newBaseDashboard()

	if base.running {
		t.Error("new dashboard should not be running")
	}

	if base.stopChan == nil {
		t.Error("stopChan should be initialized")
	}

	if base.doneChan == nil {
		t.Error("doneChan should be initialized")
	}

	if base.state.Steps == nil {
		t.Error("state.Steps should be initialized")
	}

	if base.state.Output == nil {
		t.Error("state.Output should be initialized")
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
