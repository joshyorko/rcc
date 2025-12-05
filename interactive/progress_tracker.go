package interactive

import (
	"github.com/joshyorko/rcc/progresscore"
)

// Re-export types from progresscore for backward compatibility
type ProgressTracker = progresscore.ProgressTracker
type TrackedStep = progresscore.TrackedStep
type ProgressStats = progresscore.ProgressStats

// Re-export constructor from progresscore
var NewProgressTracker = progresscore.NewProgressTracker
