package interactive

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

// LogsView displays RCC activity logs
type LogsView struct {
	styles *Styles
	width  int
	height int
	logs   *StyledLogBuffer
	scroll int
}

// NewLogsView creates a new logs view
func NewLogsView(styles *Styles) *LogsView {
	return &LogsView{
		styles: styles,
		width:  120,
		height: 30,
		logs:   NewStyledLogBuffer(500),
		scroll: 0,
	}
}

// Init implements View
func (v *LogsView) Init() tea.Cmd {
	return nil
}

// Update implements View
func (v *LogsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			v.scroll++
		case "k", "up":
			if v.scroll > 0 {
				v.scroll--
			}
		case "g":
			v.scroll = 0
		case "G":
			v.scroll = v.logs.Len()
		case "c":
			v.logs.Clear()
			v.scroll = 0
		}
	}
	return v, nil
}

// View implements View
func (v *LogsView) View() string {
	var b strings.Builder

	// Calculate available space for the panel
	availableHeight := v.height - 6 // Reserve space for title and footer
	if availableHeight < 10 {
		availableHeight = 10
	}
	panelWidth := v.width - 4
	if panelWidth < 40 {
		panelWidth = 40
	}

	// Build stats bar
	stats := v.buildStatsBar()

	// Build log content
	logContent := v.buildLogContent(availableHeight - 4) // Reserve space for stats and borders

	// Create the panel content
	var panelContent strings.Builder
	panelContent.WriteString(stats)
	panelContent.WriteString("\n")
	panelContent.WriteString(v.styles.Divider.Render(strings.Repeat("─", panelWidth-4)))
	panelContent.WriteString("\n\n")
	panelContent.WriteString(logContent)

	// Wrap in panel
	panel := v.styles.Panel.
		Width(panelWidth).
		Height(availableHeight).
		Render(panelContent.String())

	// Build the complete view
	b.WriteString("\n")
	b.WriteString(v.styles.PanelTitle.Render("  Activity Logs"))
	b.WriteString("\n\n")
	b.WriteString(panel)
	b.WriteString("\n\n")
	b.WriteString(v.buildFooter())
	b.WriteString("\n")

	return b.String()
}

// buildStatsBar creates a statistics bar showing log counts
func (v *LogsView) buildStatsBar() string {
	stats := v.logs.Stats()

	if stats.Total == 0 {
		return v.styles.Subtle.Render("No entries")
	}

	var parts []string

	// Total count
	parts = append(parts, v.styles.Info.Render(fmt.Sprintf("[*] %d total", stats.Total)))

	// Errors (if any)
	if stats.Errors > 0 {
		parts = append(parts, v.styles.Error.Render(fmt.Sprintf("[x] %d errors", stats.Errors)))
	}

	// Warnings (if any)
	if stats.Warns > 0 {
		parts = append(parts, v.styles.Warning.Render(fmt.Sprintf("[!] %d warnings", stats.Warns)))
	}

	// Info count
	if stats.Infos > 0 {
		parts = append(parts, v.styles.Subtle.Render(fmt.Sprintf("[i] %d info", stats.Infos)))
	}

	separator := v.styles.Divider.Render(" │ ")
	return strings.Join(parts, separator)
}

// buildLogContent creates the formatted log viewer content
func (v *LogsView) buildLogContent(maxLines int) string {
	if v.logs.Len() == 0 {
		return v.renderEmptyState()
	}

	entries := v.logs.Recent(maxLines)
	if len(entries) == 0 {
		return v.renderEmptyState()
	}

	var lines []string
	totalEntries := v.logs.Len()
	startNum := totalEntries - len(entries) + 1

	// Calculate line number width for alignment
	lineNumWidth := len(fmt.Sprintf("%d", totalEntries))
	if lineNumWidth < 3 {
		lineNumWidth = 3
	}

	for i, entry := range entries {
		lineNum := startNum + i
		line := v.formatLogLine(lineNum, entry, lineNumWidth)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// formatLogLine formats a single log entry with line number and styling
func (v *LogsView) formatLogLine(lineNum int, entry LogEntry, lineNumWidth int) string {
	var b strings.Builder

	// Line number (right-aligned, dimmed)
	lineNumStr := fmt.Sprintf("%*d", lineNumWidth, lineNum)
	b.WriteString(v.styles.Subtle.Render(lineNumStr))
	b.WriteString(" ")
	b.WriteString(v.styles.Divider.Render("│"))
	b.WriteString(" ")

	// Timestamp (optional, compact)
	timeStr := entry.Time.Format("15:04:05")
	b.WriteString(v.styles.Subtle.Render(timeStr))
	b.WriteString(" ")

	// Level indicator with appropriate styling
	levelStyle := v.getLevelStyle(entry.Level)
	levelIcon := entry.Level.Icon()
	b.WriteString(levelStyle.Render(levelIcon))
	b.WriteString(" ")

	// Source tag (if present)
	if entry.Source != "" {
		sourceTag := fmt.Sprintf("[%s]", entry.Source)
		b.WriteString(v.styles.Accent.Render(sourceTag))
		b.WriteString(" ")
	}

	// Message content
	messageStyle := v.getMessageStyle(entry.Level)
	b.WriteString(messageStyle.Render(entry.Message))

	return b.String()
}

// getLevelStyle returns the appropriate style for a log level icon
func (v *LogsView) getLevelStyle(level LogLevel) lipgloss.Style {
	switch level {
	case LogTrace:
		return v.styles.Subtle
	case LogDebug:
		return v.styles.Subtle
	case LogInfo:
		return v.styles.Info
	case LogWarn:
		return v.styles.Warning
	case LogError:
		return v.styles.Error
	default:
		return v.styles.ListItem
	}
}

// getMessageStyle returns the appropriate style for a log message
func (v *LogsView) getMessageStyle(level LogLevel) lipgloss.Style {
	switch level {
	case LogError:
		return v.styles.ListItem.Copy().Foreground(v.styles.theme.Error)
	case LogWarn:
		return v.styles.ListItem
	default:
		return v.styles.ListItem
	}
}

// renderEmptyState creates a nice empty state message
func (v *LogsView) renderEmptyState() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render("    [i] No activity logs yet"))
	b.WriteString("\n\n")
	b.WriteString(v.styles.Subtle.Render("    Logs from RCC operations will appear here"))
	b.WriteString("\n")

	return b.String()
}

// buildFooter creates the help/command footer
func (v *LogsView) buildFooter() string {
	var parts []string

	// Navigation commands
	parts = append(parts, v.styles.HelpKey.Render("j/k")+" "+v.styles.HelpDesc.Render("scroll"))
	parts = append(parts, v.styles.HelpKey.Render("g/G")+" "+v.styles.HelpDesc.Render("top/bottom"))
	parts = append(parts, v.styles.HelpKey.Render("c")+" "+v.styles.HelpDesc.Render("clear"))

	separator := v.styles.MenuSeparator.Render(" • ")
	return "  " + strings.Join(parts, separator)
}

// Name implements View
func (v *LogsView) Name() string {
	return "Logs"
}

// ShortHelp implements View
func (v *LogsView) ShortHelp() string {
	return "j/k:scroll g/G:top/bot c:clear"
}
