// Package progresscore provides shared progress tracking types used by both pretty and interactive packages.
// This package breaks the import cycle by providing common types that both can import.
package progresscore

import (
	"sync"
	"time"

	"github.com/joshyorko/rcc/dashcore"
)

// ProgressTracker ensures progress only moves forward, never backwards
// This prevents the confusing UX of progress bars moving in reverse
type ProgressTracker struct {
	steps          []TrackedStep
	minProgress    float64   // Progress can never go below this
	startTime      time.Time
	lastUpdateTime time.Time
	mu             sync.RWMutex
	onUpdate       func() // Callback when progress changes
}

// TrackedStep represents a single step with timing info
type TrackedStep struct {
	Name      string
	Status    dashcore.StepStatus
	Message   string
	StartTime time.Time
	EndTime   time.Time
}

// Duration returns how long this step took (or has been running)
func (s TrackedStep) Duration() time.Duration {
	if s.StartTime.IsZero() {
		return 0
	}
	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}

// NewProgressTracker creates a tracker for the given steps
func NewProgressTracker(stepNames []string) *ProgressTracker {
	steps := make([]TrackedStep, len(stepNames))
	for i, name := range stepNames {
		steps[i] = TrackedStep{
			Name:   name,
			Status: dashcore.StepPending,
		}
	}

	return &ProgressTracker{
		steps:       steps,
		minProgress: 0,
		startTime:   time.Now(),
	}
}

// SetOnUpdate sets a callback for when progress changes
func (pt *ProgressTracker) SetOnUpdate(fn func()) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.onUpdate = fn
}

// canTransition checks if a status transition is valid (forward-only)
func canTransition(from, to dashcore.StepStatus) bool {
	// Valid transitions:
	// Pending -> Running, Skipped
	// Running -> Complete, Failed
	// Complete, Failed, Skipped -> (terminal states)
	switch from {
	case dashcore.StepPending:
		return to == dashcore.StepRunning || to == dashcore.StepSkipped
	case dashcore.StepRunning:
		return to == dashcore.StepComplete || to == dashcore.StepFailed
	default:
		return false // Terminal states cannot transition
	}
}

// SetStep updates a step's status with forward-only enforcement
func (pt *ProgressTracker) SetStep(index int, status dashcore.StepStatus, message string) bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if index < 0 || index >= len(pt.steps) {
		return false
	}

	current := pt.steps[index].Status

	// Enforce forward-only transitions
	if !canTransition(current, status) {
		// Log but don't fail - this helps debug issues
		return false
	}

	// Update the step
	pt.steps[index].Status = status
	pt.steps[index].Message = message

	// Track timing
	now := time.Now()
	pt.lastUpdateTime = now

	if status == dashcore.StepRunning && pt.steps[index].StartTime.IsZero() {
		pt.steps[index].StartTime = now
	}
	if status == dashcore.StepComplete || status == dashcore.StepFailed || status == dashcore.StepSkipped {
		pt.steps[index].EndTime = now
	}

	// Update minimum progress (can only increase)
	newProgress := pt.calculateProgress()
	if newProgress > pt.minProgress {
		pt.minProgress = newProgress
	}

	// Notify listener
	if pt.onUpdate != nil {
		pt.onUpdate()
	}

	return true
}

// StartStep marks a step as running (convenience method)
func (pt *ProgressTracker) StartStep(index int, message string) bool {
	return pt.SetStep(index, dashcore.StepRunning, message)
}

// CompleteStep marks a step as complete (convenience method)
func (pt *ProgressTracker) CompleteStep(index int) bool {
	return pt.SetStep(index, dashcore.StepComplete, "")
}

// FailStep marks a step as failed (convenience method)
func (pt *ProgressTracker) FailStep(index int, reason string) bool {
	return pt.SetStep(index, dashcore.StepFailed, reason)
}

// SkipStep marks a step as skipped (convenience method)
func (pt *ProgressTracker) SkipStep(index int, reason string) bool {
	return pt.SetStep(index, dashcore.StepSkipped, reason)
}

// calculateProgress returns the completion ratio (0.0 to 1.0)
func (pt *ProgressTracker) calculateProgress() float64 {
	if len(pt.steps) == 0 {
		return 0
	}

	completed := 0
	for _, step := range pt.steps {
		if step.Status == dashcore.StepComplete || step.Status == dashcore.StepSkipped {
			completed++
		}
	}
	return float64(completed) / float64(len(pt.steps))
}

// Progress returns the current progress (forward-only, never decreases)
func (pt *ProgressTracker) Progress() float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.minProgress
}

// Steps returns a copy of all steps
func (pt *ProgressTracker) Steps() []TrackedStep {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	result := make([]TrackedStep, len(pt.steps))
	copy(result, pt.steps)
	return result
}

// CurrentStep returns the index and info of the currently running step
func (pt *ProgressTracker) CurrentStep() (int, *TrackedStep) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	for i, step := range pt.steps {
		if step.Status == dashcore.StepRunning {
			s := step // Copy
			return i, &s
		}
	}
	return -1, nil
}

// ProgressStats holds progress statistics
type ProgressStats struct {
	Total     int
	Completed int
	Failed    int
	Skipped   int
	Running   int
	Pending   int
	Progress  float64
	Elapsed   time.Duration
	ETA       time.Duration
}

func (pt *ProgressTracker) Stats() ProgressStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	stats := ProgressStats{
		Total:    len(pt.steps),
		Progress: pt.minProgress,
		Elapsed:  time.Since(pt.startTime),
	}

	var totalDuration time.Duration
	var completedDuration time.Duration

	for _, step := range pt.steps {
		switch step.Status {
		case dashcore.StepComplete:
			stats.Completed++
			completedDuration += step.Duration()
		case dashcore.StepFailed:
			stats.Failed++
		case dashcore.StepSkipped:
			stats.Skipped++
		case dashcore.StepRunning:
			stats.Running++
		case dashcore.StepPending:
			stats.Pending++
		}
		totalDuration += step.Duration()
	}

	// Calculate ETA based on average step time
	if stats.Completed > 0 && stats.Pending > 0 {
		avgStepTime := completedDuration / time.Duration(stats.Completed)
		stats.ETA = avgStepTime * time.Duration(stats.Pending+stats.Running)
	}

	return stats
}

// IsComplete returns true if all steps are in terminal states
func (pt *ProgressTracker) IsComplete() bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	for _, step := range pt.steps {
		if step.Status == dashcore.StepPending || step.Status == dashcore.StepRunning {
			return false
		}
	}
	return true
}

// HasFailed returns true if any step has failed
func (pt *ProgressTracker) HasFailed() bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	for _, step := range pt.steps {
		if step.Status == dashcore.StepFailed {
			return true
		}
	}
	return false
}

// Reset clears all progress (use sparingly - defeats forward-only purpose)
func (pt *ProgressTracker) Reset() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	for i := range pt.steps {
		pt.steps[i].Status = dashcore.StepPending
		pt.steps[i].Message = ""
		pt.steps[i].StartTime = time.Time{}
		pt.steps[i].EndTime = time.Time{}
	}
	pt.minProgress = 0
	pt.startTime = time.Now()
	pt.lastUpdateTime = time.Time{}
}
