package interactive

import (
	"fmt"
	"strings"

	"github.com/joshyorko/rcc/logbuf"
)

// Re-export types from logbuf package
type LogLevel = logbuf.LogLevel
type LogEntry = logbuf.LogEntry
type LogBuffer = logbuf.LogBuffer
type LogStats = logbuf.LogStats

// Re-export constants
const (
	LogTrace = logbuf.LogTrace
	LogDebug = logbuf.LogDebug
	LogInfo  = logbuf.LogInfo
	LogWarn  = logbuf.LogWarn
	LogError = logbuf.LogError
)

// NewLogBuffer creates a new log buffer with specified max size
func NewLogBuffer(maxSize int) *LogBuffer {
	return logbuf.NewLogBuffer(maxSize)
}

// FormatLogEntry returns a formatted log line using interactive.Styles
func FormatLogEntry(e LogEntry, styles *Styles, showTime bool) string {
	var b strings.Builder

	// Time (optional)
	if showTime {
		timeStr := e.Time.Format("15:04:05")
		b.WriteString(styles.Subtle.Render(timeStr))
		b.WriteString(" ")
	}

	// Level icon with color
	var levelStyle = styles.ListItem
	switch e.Level {
	case LogTrace:
		levelStyle = styles.Subtle
	case LogDebug:
		levelStyle = styles.Subtle
	case LogInfo:
		levelStyle = styles.ListItem
	case LogWarn:
		levelStyle = styles.Warning
	case LogError:
		levelStyle = styles.Error
	}
	b.WriteString(levelStyle.Render(e.Level.Icon()))
	b.WriteString(" ")

	// Source (if present)
	if e.Source != "" {
		b.WriteString(styles.Subtle.Render("[" + e.Source + "]"))
		b.WriteString(" ")
	}

	// Message
	b.WriteString(styles.ListItem.Render(e.Message))

	return b.String()
}

// RenderLogBuffer returns a formatted string of the N most recent logs
func RenderLogBuffer(lb *LogBuffer, styles *Styles, n int, showTime bool) string {
	entries := lb.Recent(n)
	if len(entries) == 0 {
		return styles.Subtle.Render("No logs yet...")
	}

	var lines []string
	for _, entry := range entries {
		lines = append(lines, FormatLogEntry(entry, styles, showTime))
	}
	return strings.Join(lines, "\n")
}

// FormatLogStats returns a formatted stats summary using interactive.Styles
func FormatLogStats(lb *LogBuffer, styles *Styles) string {
	stats := lb.Stats()
	if stats.Total == 0 {
		return ""
	}

	var parts []string
	if stats.Errors > 0 {
		parts = append(parts, styles.Error.Render(fmt.Sprintf("%d errors", stats.Errors)))
	}
	if stats.Warns > 0 {
		parts = append(parts, styles.Warning.Render(fmt.Sprintf("%d warnings", stats.Warns)))
	}
	parts = append(parts, styles.Subtle.Render(fmt.Sprintf("%d total", stats.Total)))

	return strings.Join(parts, " · ")
}
