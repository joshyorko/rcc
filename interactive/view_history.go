package interactive

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HistoryView displays run history
type HistoryView struct {
	styles   *Styles
	width    int
	height   int
	entries  []RunHistoryEntry
	selected int
	loading  bool
}

// NewHistoryView creates a new history view
func NewHistoryView(styles *Styles) *HistoryView {
	return &HistoryView{
		styles:   styles,
		width:    120,
		height:   30,
		entries:  []RunHistoryEntry{},
		selected: 0,
		loading:  true,
	}
}

// Init implements View
func (v *HistoryView) Init() tea.Cmd {
	return v.loadHistory
}

type historyLoadedMsg []RunHistoryEntry

func (v *HistoryView) loadHistory() tea.Msg {
	history := GetRunHistory()
	return historyLoadedMsg(history.GetLatest(50))
}

// Update implements View
func (v *HistoryView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case historyLoadedMsg:
		v.entries = []RunHistoryEntry(msg)
		v.loading = false
		if len(v.entries) > 0 && v.selected >= len(v.entries) {
			v.selected = len(v.entries) - 1
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if v.selected < len(v.entries)-1 {
				v.selected++
			}
		case "k", "up":
			if v.selected > 0 {
				v.selected--
			}
		case "g":
			v.selected = 0
		case "G":
			if len(v.entries) > 0 {
				v.selected = len(v.entries) - 1
			}
		case "R":
			v.loading = true
			return v, v.loadHistory
		case "r", "enter":
			// Re-run selected entry
			if v.selected >= 0 && v.selected < len(v.entries) {
				entry := v.entries[v.selected]
				action := ActionResult{
					Type:      ActionRunRobot,
					RobotPath: entry.RobotPath,
					RobotTask: entry.Task,
				}
				return v, func() tea.Msg { return actionMsg{action: action} }
			}
		case "c":
			// Clear history
			history := GetRunHistory()
			history.Clear()
			history.Save()
			v.entries = []RunHistoryEntry{}
			v.selected = 0
		}
	}
	return v, nil
}

// View implements View
func (v *HistoryView) View() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	boxWidth := v.width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}
	if boxWidth > 120 {
		boxWidth = 120
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	subtitle := ""
	if !v.loading {
		subtitle = fmt.Sprintf("(%d runs)", len(v.entries))
	}
	b.WriteString(RenderHeader(vs, "Run History", subtitle, contentWidth))

	if v.loading {
		b.WriteString(vs.Subtext.Render("Loading history..."))
	} else if len(v.entries) == 0 {
		b.WriteString(vs.Subtext.Render("No run history yet"))
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Tip "))
		b.WriteString(vs.Text.Render("Run a robot to start tracking history"))
	} else {
		// History list
		maxVisible := 12
		startIdx := 0
		if v.selected >= maxVisible {
			startIdx = v.selected - maxVisible + 1
		}

		for i := startIdx; i < len(v.entries) && i < startIdx+maxVisible; i++ {
			entry := v.entries[i]
			isSelected := i == v.selected

			// Status indicator
			statusIcon := ""
			statusStyle := vs.Subtext
			switch entry.Status {
			case RunSuccess:
				statusIcon = "✓"
				statusStyle = vs.Success
			case RunFailed:
				statusIcon = "✗"
				statusStyle = vs.Error
			default:
				statusIcon = "?"
				statusStyle = vs.Warning
			}

			// Robot name (truncated)
			name := entry.RobotName
			if len(name) > 20 {
				name = name[:17] + "..."
			}

			// Task (truncated)
			task := entry.Task
			if task == "" {
				task = "(default)"
			}
			if len(task) > 15 {
				task = task[:12] + "..."
			}

			// Time display
			timeStr := entry.StartTime.Format("Jan 02 15:04")

			// Duration
			durationStr := entry.Duration
			if durationStr == "" {
				durationStr = "-"
			}

			// Build line
			if isSelected {
				b.WriteString(vs.Selected.Render("> "))
			} else {
				b.WriteString("  ")
			}

			b.WriteString(statusStyle.Render(statusIcon))
			b.WriteString(" ")

			if isSelected {
				b.WriteString(vs.Selected.Render(name))
			} else {
				b.WriteString(vs.Text.Render(name))
			}
			b.WriteString("  ")
			b.WriteString(vs.Subtext.Render(task))
			b.WriteString("  ")
			b.WriteString(vs.Subtext.Render(timeStr))
			b.WriteString("  ")
			b.WriteString(vs.Info.Render(durationStr))
			b.WriteString("\n")

			// Show details for selected entry
			if isSelected && i < len(v.entries) {
				b.WriteString("\n")
				b.WriteString(vs.Label.Render("  Path "))
				pathStr := entry.RobotPath
				if len(pathStr) > contentWidth-10 {
					pathStr = "..." + pathStr[len(pathStr)-(contentWidth-13):]
				}
				b.WriteString(vs.Subtext.Render(pathStr))
				b.WriteString("\n")

				if entry.Controller != "" {
					b.WriteString(vs.Label.Render("  Controller "))
					b.WriteString(vs.Subtext.Render(entry.Controller))
					b.WriteString("\n")
				}

				if entry.Status == RunFailed && entry.ExitCode != 0 {
					b.WriteString(vs.Label.Render("  Exit Code "))
					b.WriteString(vs.Error.Render(fmt.Sprintf("%d", entry.ExitCode)))
					b.WriteString("\n")
				}
			}
		}

		// Show scroll hint if needed
		if len(v.entries) > maxVisible {
			remaining := len(v.entries) - startIdx - maxVisible
			if remaining > 0 {
				b.WriteString(vs.Subtext.Render(fmt.Sprintf("\n  ... +%d more", remaining)))
			}
		}
	}

	// Footer
	b.WriteString("\n")
	hints := []KeyHint{
		{"j/k", "nav"},
		{"r/Enter", "re-run"},
		{"c", "clear"},
		{"R", "refresh"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

// Name implements View
func (v *HistoryView) Name() string {
	return "History"
}

// ShortHelp implements View
func (v *HistoryView) ShortHelp() string {
	return "j/k:nav r:re-run c:clear R:refresh"
}
