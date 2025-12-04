package interactive

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// LogEntry represents a log entry
type LogEntry struct {
	Level   string
	Message string
	Time    string
}

// LogsView displays logs
type LogsView struct {
	styles   *Styles
	logs     []LogEntry
	cursor   int
	filter   string
	viewport int // scroll position
}

// NewLogsView creates a new logs view
func NewLogsView(styles *Styles) *LogsView {
	return &LogsView{
		styles: styles,
		logs: []LogEntry{
			{Level: "INFO", Message: "Interactive mode started", Time: "now"},
			{Level: "INFO", Message: "Use views 1-5 to navigate", Time: "now"},
		},
	}
}

// Init implements View
func (v *LogsView) Init() tea.Cmd {
	return nil
}

// Update implements View
func (v *LogsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Down):
			if v.viewport < len(v.logs)-1 {
				v.viewport++
			}
		case key.Matches(msg, keys.Up):
			if v.viewport > 0 {
				v.viewport--
			}
		case key.Matches(msg, keys.Top):
			v.viewport = 0
		case key.Matches(msg, keys.Bottom):
			v.viewport = len(v.logs) - 1
		}
	}
	return v, nil
}

// View implements View
func (v *LogsView) View() string {
	var b strings.Builder

	b.WriteString(v.styles.Subtitle.Render("Activity Log"))
	b.WriteString("\n")
	b.WriteString(v.styles.Subtle.Render("Recent RCC activity"))
	b.WriteString("\n\n")

	if len(v.logs) == 0 {
		b.WriteString(v.styles.Subtle.Render("No log entries."))
		return b.String()
	}

	// Show logs
	for i, log := range v.logs {
		var levelStyle = v.styles.Subtle
		switch log.Level {
		case "ERROR":
			levelStyle = v.styles.Error
		case "WARN":
			levelStyle = v.styles.Warning
		case "INFO":
			levelStyle = v.styles.Success
		case "DEBUG":
			levelStyle = v.styles.Subtle
		}

		line := levelStyle.Render("["+log.Level+"]") + " " + v.styles.TreeLeaf.Render(log.Message)

		if i == v.viewport {
			b.WriteString("▶ " + line)
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// Name implements View
func (v *LogsView) Name() string {
	return "Logs"
}

// ShortHelp implements View
func (v *LogsView) ShortHelp() string {
	return "j/k:scroll  /:filter"
}
