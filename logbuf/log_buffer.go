package logbuf

import (
	"strings"
	"sync"
	"time"
)

// Iconic controls whether to use Unicode icons or ASCII fallbacks
var Iconic = true

// LogLevel represents the severity of a log entry
type LogLevel int

const (
	LogTrace LogLevel = iota
	LogDebug
	LogInfo
	LogWarn
	LogError
)

func (l LogLevel) String() string {
	switch l {
	case LogTrace:
		return "TRACE"
	case LogDebug:
		return "DEBUG"
	case LogInfo:
		return "INFO"
	case LogWarn:
		return "WARN"
	case LogError:
		return "ERROR"
	default:
		return "???"
	}
}

func (l LogLevel) Icon() string {
	if !Iconic {
		return l.String()[:1]
	}
	switch l {
	case LogTrace:
		return "·"
	case LogDebug:
		return "○"
	case LogInfo:
		return "●"
	case LogWarn:
		return "▲"
	case LogError:
		return "✗"
	default:
		return "?"
	}
}

// LogEntry represents a single log line with metadata
type LogEntry struct {
	Time    time.Time
	Level   LogLevel
	Source  string // Component name (e.g., "conda", "pip", "holotree")
	Message string
}

// LogBuffer is a thread-safe circular buffer for log entries
type LogBuffer struct {
	entries  []LogEntry
	maxSize  int
	mu       sync.RWMutex
	onChange func() // Callback when new entry is added
}

// NewLogBuffer creates a new log buffer with specified max size
func NewLogBuffer(maxSize int) *LogBuffer {
	if maxSize < 10 {
		maxSize = 10
	}
	return &LogBuffer{
		entries: make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// SetOnChange sets a callback to be called when entries change
func (lb *LogBuffer) SetOnChange(fn func()) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.onChange = fn
}

// Add appends a new log entry
func (lb *LogBuffer) Add(level LogLevel, source, message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	entry := LogEntry{
		Time:    time.Now(),
		Level:   level,
		Source:  source,
		Message: strings.TrimSpace(message),
	}

	lb.entries = append(lb.entries, entry)

	// Trim if over capacity (circular buffer behavior)
	if len(lb.entries) > lb.maxSize {
		lb.entries = lb.entries[len(lb.entries)-lb.maxSize:]
	}

	// Notify listener
	if lb.onChange != nil {
		lb.onChange()
	}
}

// AddLine adds a plain text line (auto-detects level from content)
func (lb *LogBuffer) AddLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	level := LogInfo
	source := ""

	// Try to detect level from line content
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		level = LogError
	case strings.Contains(lower, "warning") || strings.Contains(lower, "warn"):
		level = LogWarn
	case strings.Contains(lower, "debug"):
		level = LogDebug
	case strings.Contains(lower, "trace"):
		level = LogTrace
	}

	// Try to detect source from common prefixes
	if strings.HasPrefix(line, "pip:") || strings.Contains(lower, "pip install") {
		source = "pip"
		line = strings.TrimPrefix(line, "pip:")
	} else if strings.HasPrefix(line, "conda:") || strings.Contains(lower, "conda") {
		source = "conda"
		line = strings.TrimPrefix(line, "conda:")
	} else if strings.HasPrefix(line, "micromamba:") || strings.Contains(lower, "micromamba") {
		source = "mamba"
		line = strings.TrimPrefix(line, "micromamba:")
	}

	lb.Add(level, source, strings.TrimSpace(line))
}

// Recent returns the N most recent entries
func (lb *LogBuffer) Recent(n int) []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if n <= 0 || len(lb.entries) == 0 {
		return nil
	}
	if n > len(lb.entries) {
		n = len(lb.entries)
	}

	// Return a copy to avoid race conditions
	result := make([]LogEntry, n)
	copy(result, lb.entries[len(lb.entries)-n:])
	return result
}

// All returns all entries
func (lb *LogBuffer) All() []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	result := make([]LogEntry, len(lb.entries))
	copy(result, lb.entries)
	return result
}

// Len returns the number of entries
func (lb *LogBuffer) Len() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return len(lb.entries)
}

// Clear removes all entries
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.entries = lb.entries[:0]
}

// LogStats holds statistics about the log buffer
type LogStats struct {
	Total  int
	Errors int
	Warns  int
	Infos  int
	Debugs int
	Traces int
}

func (lb *LogBuffer) Stats() LogStats {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	stats := LogStats{Total: len(lb.entries)}
	for _, e := range lb.entries {
		switch e.Level {
		case LogError:
			stats.Errors++
		case LogWarn:
			stats.Warns++
		case LogInfo:
			stats.Infos++
		case LogDebug:
			stats.Debugs++
		case LogTrace:
			stats.Traces++
		}
	}
	return stats
}
