package progresscore

import (
	"testing"

	"github.com/joshyorko/rcc/dashcore"
)

func TestNewProgressTracker(t *testing.T) {
	steps := []string{"Step 1", "Step 2", "Step 3"}
	tracker := NewProgressTracker(steps)

	if tracker == nil {
		t.Fatal("NewProgressTracker returned nil")
	}

	if len(tracker.steps) != 3 {
		t.Errorf("Expected 3 steps, got %d", len(tracker.steps))
	}

	// All steps should start as pending
	for i, step := range tracker.steps {
		if step.Status != dashcore.StepPending {
			t.Errorf("Step %d should be pending, got %v", i, step.Status)
		}
		if step.Name != steps[i] {
			t.Errorf("Step %d name mismatch: expected %s, got %s", i, steps[i], step.Name)
		}
	}
}

func TestProgressTrackerForwardOnly(t *testing.T) {
	steps := []string{"Step 1", "Step 2"}
	tracker := NewProgressTracker(steps)

	// Start step 0
	if !tracker.StartStep(0, "Starting") {
		t.Error("Failed to start step 0")
	}

	// Try to set it back to pending (should fail)
	if tracker.SetStep(0, dashcore.StepPending, "") {
		t.Error("Should not be able to set running step back to pending")
	}

	// Complete the step
	if !tracker.CompleteStep(0) {
		t.Error("Failed to complete step 0")
	}

	// Try to set it back to running (should fail)
	if tracker.SetStep(0, dashcore.StepRunning, "") {
		t.Error("Should not be able to set completed step back to running")
	}

	// Verify progress only moved forward
	progress := tracker.Progress()
	if progress < 0.5 {
		t.Errorf("Expected progress >= 0.5, got %f", progress)
	}
}

func TestProgressTrackerStats(t *testing.T) {
	steps := []string{"Step 1", "Step 2", "Step 3"}
	tracker := NewProgressTracker(steps)

	stats := tracker.Stats()
	if stats.Total != 3 {
		t.Errorf("Expected total=3, got %d", stats.Total)
	}
	if stats.Pending != 3 {
		t.Errorf("Expected pending=3, got %d", stats.Pending)
	}

	// Start and complete a step
	tracker.StartStep(0, "Running")
	tracker.CompleteStep(0)

	stats = tracker.Stats()
	if stats.Completed != 1 {
		t.Errorf("Expected completed=1, got %d", stats.Completed)
	}
	if stats.Pending != 2 {
		t.Errorf("Expected pending=2, got %d", stats.Pending)
	}

	// Fail a step
	tracker.StartStep(1, "Running")
	tracker.FailStep(1, "Error occurred")

	stats = tracker.Stats()
	if stats.Failed != 1 {
		t.Errorf("Expected failed=1, got %d", stats.Failed)
	}

	// Skip a step
	tracker.SkipStep(2, "Skipped")

	stats = tracker.Stats()
	if stats.Skipped != 1 {
		t.Errorf("Expected skipped=1, got %d", stats.Skipped)
	}
}

func TestProgressTrackerIsComplete(t *testing.T) {
	steps := []string{"Step 1", "Step 2"}
	tracker := NewProgressTracker(steps)

	if tracker.IsComplete() {
		t.Error("Tracker should not be complete initially")
	}

	tracker.StartStep(0, "Running")
	if tracker.IsComplete() {
		t.Error("Tracker should not be complete with running step")
	}

	tracker.CompleteStep(0)
	if tracker.IsComplete() {
		t.Error("Tracker should not be complete with pending step")
	}

	tracker.SkipStep(1, "Skipped")
	if !tracker.IsComplete() {
		t.Error("Tracker should be complete when all steps are terminal")
	}
}

func TestProgressTrackerHasFailed(t *testing.T) {
	steps := []string{"Step 1", "Step 2"}
	tracker := NewProgressTracker(steps)

	if tracker.HasFailed() {
		t.Error("Tracker should not have failed initially")
	}

	tracker.StartStep(0, "Running")
	tracker.CompleteStep(0)

	if tracker.HasFailed() {
		t.Error("Tracker should not have failed after completing step")
	}

	tracker.StartStep(1, "Running")
	tracker.FailStep(1, "Error")

	if !tracker.HasFailed() {
		t.Error("Tracker should have failed after failing a step")
	}
}

func TestProgressTrackerCurrentStep(t *testing.T) {
	steps := []string{"Step 1", "Step 2", "Step 3"}
	tracker := NewProgressTracker(steps)

	// No running step initially
	idx, step := tracker.CurrentStep()
	if idx != -1 || step != nil {
		t.Error("Should have no current step initially")
	}

	// Start step 1
	tracker.StartStep(1, "Running step 1")
	idx, step = tracker.CurrentStep()
	if idx != 1 {
		t.Errorf("Expected current step index 1, got %d", idx)
	}
	if step == nil || step.Name != "Step 2" {
		t.Error("Current step should be Step 2")
	}

	// Complete step 1, no running step
	tracker.CompleteStep(1)
	idx, step = tracker.CurrentStep()
	if idx != -1 || step != nil {
		t.Error("Should have no current step after completing")
	}
}

func TestTrackedStepDuration(t *testing.T) {
	step := TrackedStep{
		Name:   "Test Step",
		Status: dashcore.StepPending,
	}

	// No start time, duration should be 0
	if step.Duration() != 0 {
		t.Error("Duration should be 0 for step with no start time")
	}

	// Set start time (duration calculated from now)
	step.StartTime = step.StartTime.Add(-1000000000) // 1 second ago
	if step.Duration() == 0 {
		t.Error("Duration should be > 0 for running step")
	}

	// Set end time (fixed duration)
	step.EndTime = step.StartTime.Add(2000000000) // 2 seconds after start
	duration := step.Duration()
	if duration < 1000000000 || duration > 3000000000 {
		t.Errorf("Duration should be ~2 seconds, got %v", duration)
	}
}
