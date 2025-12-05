package pretty

import (
	"testing"
	"time"

	"github.com/joshyorko/rcc/dashcore"
)

func TestNewDiagnosticsDashboard(t *testing.T) {
	checks := []string{
		"Path configuration",
		"Python installation",
		"Network connectivity",
		"Holotree permissions",
		"Disk space",
	}

	dashboard := NewDiagnosticsDashboard(checks)

	// Should return noop dashboard when not in interactive mode or dashboard disabled
	// We can't directly check the type since noopDashboard is private in dashcore
	// Instead, just verify we got a valid Dashboard interface
	if dashboard == nil {
		t.Error("Expected valid Dashboard interface")
	}

	// If we got a real dashboard, verify it was properly initialized
	if diag, ok := dashboard.(*diagnosticsDashboard); ok {
		if len(diag.checks) != len(checks) {
			t.Errorf("Expected %d checks, got %d", len(checks), len(diag.checks))
		}

		for i, check := range diag.checks {
			if check.Name != checks[i] {
				t.Errorf("Check %d: expected name %q, got %q", i, checks[i], check.Name)
			}
			if check.Status != StepPending {
				t.Errorf("Check %d: expected status StepPending, got %v", i, check.Status)
			}
		}
	}
}

func TestNewDiagnosticsDashboardWithCategories(t *testing.T) {
	checks := []string{
		"System Checks:Operating system",
		"System Checks:User permissions",
		"System Checks:Temp directory",
		"Network Checks:DNS resolution",
		"Network Checks:HTTPS connectivity",
		"Environment Checks:Micromamba version",
	}

	dashboard := NewDiagnosticsDashboard(checks)

	// If we got a real dashboard, verify categories were parsed correctly
	if diag, ok := dashboard.(*diagnosticsDashboard); ok {
		if len(diag.checks) != len(checks) {
			t.Errorf("Expected %d checks, got %d", len(checks), len(diag.checks))
		}

		expectedChecks := []struct {
			category string
			name     string
		}{
			{"System Checks", "Operating system"},
			{"System Checks", "User permissions"},
			{"System Checks", "Temp directory"},
			{"Network Checks", "DNS resolution"},
			{"Network Checks", "HTTPS connectivity"},
			{"Environment Checks", "Micromamba version"},
		}

		for i, expected := range expectedChecks {
			if diag.checks[i].Category != expected.category {
				t.Errorf("Check %d: expected category %q, got %q", i, expected.category, diag.checks[i].Category)
			}
			if diag.checks[i].Name != expected.name {
				t.Errorf("Check %d: expected name %q, got %q", i, expected.name, diag.checks[i].Name)
			}
			if diag.checks[i].Status != StepPending {
				t.Errorf("Check %d: expected status StepPending, got %v", i, diag.checks[i].Status)
			}
		}

		// Test getCategories function
		categories := diag.getCategories()
		expectedCategories := []string{"System Checks", "Network Checks", "Environment Checks"}
		if len(categories) != len(expectedCategories) {
			t.Errorf("Expected %d categories, got %d", len(expectedCategories), len(categories))
		}
		for i, expected := range expectedCategories {
			if i >= len(categories) {
				break
			}
			if categories[i] != expected {
				t.Errorf("Category %d: expected %q, got %q", i, expected, categories[i])
			}
		}
	}
}

func TestDiagnosticsDashboardSetStep(t *testing.T) {
	checks := []string{
		"Check 1",
		"Check 2",
		"Check 3",
	}

	dashboard := &diagnosticsDashboard{
		baseDashboard: dashcore.NewBaseDashboard(),
		checks:        make([]diagnosticsCheck, len(checks)),
	}

	// Initialize checks
	for i, name := range checks {
		dashboard.checks[i] = diagnosticsCheck{
			Name:   name,
			Status: StepPending,
		}
	}

	// Test updating check status
	dashboard.SetStep(0, StepRunning, "")
	if dashboard.checks[0].Status != StepRunning {
		t.Error("Expected check 0 to be running")
	}

	dashboard.SetStep(0, StepComplete, "")
	if dashboard.checks[0].Status != StepComplete {
		t.Error("Expected check 0 to be complete")
	}

	dashboard.SetStep(1, StepFailed, "permission denied")
	if dashboard.checks[1].Status != StepFailed {
		t.Error("Expected check 1 to be failed")
	}
	if dashboard.checks[1].Message != "permission denied" {
		t.Errorf("Expected message 'permission denied', got %q", dashboard.checks[1].Message)
	}

	dashboard.SetStep(2, StepSkipped, "warning: low disk space")
	if dashboard.checks[2].Status != StepSkipped {
		t.Error("Expected check 2 to be skipped")
	}

	// Test out of bounds
	dashboard.SetStep(10, StepComplete, "")
	// Should not panic
}

func TestDiagnosticsDashboardGetCheckIconAndColor(t *testing.T) {
	dashboard := &diagnosticsDashboard{
		baseDashboard: dashcore.NewBaseDashboard(),
	}

	tests := []struct {
		status       StepStatus
		wantIconic   string
		wantColor    string
	}{
		{StepComplete, "✓", Green},
		{StepFailed, "✗", Red},
		{StepSkipped, "⚠", Yellow},
		{StepPending, "○", Grey},
		{StepRunning, "⠋", Cyan}, // First spinner frame
	}

	// Save original Iconic value and restore after test
	origIconic := dashcore.Iconic
	defer func() { dashcore.Iconic = origIconic }()

	dashcore.Iconic = true

	for _, tt := range tests {
		icon, color := dashboard.getCheckIconAndColor(tt.status, 0)
		if icon != tt.wantIconic {
			t.Errorf("Status %v: expected icon %q, got %q", tt.status, tt.wantIconic, icon)
		}
		if color != tt.wantColor {
			t.Errorf("Status %v: expected color %q, got %q", tt.status, tt.wantColor, color)
		}
	}
}

func TestDiagnosticsDashboardGetSummary(t *testing.T) {
	dashboard := &diagnosticsDashboard{
		baseDashboard: dashcore.NewBaseDashboard(),
		checks: []diagnosticsCheck{
			{Name: "Check 1", Status: StepComplete},
			{Name: "Check 2", Status: StepComplete},
			{Name: "Check 3", Status: StepFailed},
			{Name: "Check 4", Status: StepSkipped},
			{Name: "Check 5", Status: StepPending},
			{Name: "Check 6", Status: StepRunning},
		},
	}

	summary := dashboard.getSummary(dashboard.checks)

	// Summary should contain all counts
	// Pass: 2, Warn: 1, Fail: 1, Pending: 2 (pending + running)

	// Just verify it doesn't panic and returns a string
	if summary == "" {
		t.Error("Expected non-empty summary")
	}
}

func TestDiagnosticsDashboardUpdate(t *testing.T) {
	dashboard := &diagnosticsDashboard{
		baseDashboard: dashcore.NewBaseDashboard(),
		checks: []diagnosticsCheck{
			{Name: "Check 1", Status: StepPending},
			{Name: "Check 2", Status: StepPending},
		},
	}

	state := DashboardState{
		Steps: []StepState{
			{Index: 0, Status: StepRunning, Message: "checking..."},
			{Index: 1, Status: StepComplete, Message: "done"},
		},
		StartTime: time.Now(),
	}

	dashboard.Update(state)

	if dashboard.checks[0].Status != StepRunning {
		t.Error("Expected check 0 to be running")
	}
	if dashboard.checks[0].Message != "checking..." {
		t.Errorf("Expected message 'checking...', got %q", dashboard.checks[0].Message)
	}

	if dashboard.checks[1].Status != StepComplete {
		t.Error("Expected check 1 to be complete")
	}
	if dashboard.checks[1].Message != "done" {
		t.Errorf("Expected message 'done', got %q", dashboard.checks[1].Message)
	}
}
