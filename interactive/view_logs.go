package interactive

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
		maxScroll := v.logs.Len() - 10
		if maxScroll < 0 {
			maxScroll = 0
		}

		switch msg.String() {
		case "j", "down":
			if v.scroll < maxScroll {
				v.scroll++
			}
		case "k", "up":
			if v.scroll > 0 {
				v.scroll--
			}
		case "g":
			v.scroll = 0
		case "G":
			v.scroll = maxScroll
		case "c":
			v.logs.Clear()
			v.scroll = 0
		case "d":
			// Page down
			v.scroll += 10
			if v.scroll > maxScroll {
				v.scroll = maxScroll
			}
		case "u":
			// Page up
			v.scroll -= 10
			if v.scroll < 0 {
				v.scroll = 0
			}
		}
	}
	return v, nil
}

// View implements View
func (v *LogsView) View() string {
	theme := v.styles.theme
	vs := NewViewStyles(theme)

	// Dynamic box sizing - logs need more width
	boxWidth := v.width - 8
	if boxWidth < 70 {
		boxWidth = 70
	}
	if boxWidth > 140 {
		boxWidth = 140
	}
	contentWidth := boxWidth - 6

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Border).
		Padding(1, 2).
		Width(boxWidth)

	var b strings.Builder

	// Header with RCC version
	stats := v.logs.Stats()
	subtitle := fmt.Sprintf("(%d entries)", stats.Total)
	b.WriteString(RenderHeader(vs, "Activity Logs", subtitle, contentWidth))

	// Stats row with colored badges
	if stats.Total > 0 {
		if stats.Errors > 0 {
			b.WriteString(vs.BadgeActive.Copy().Background(theme.Error).Render(fmt.Sprintf(" %d ERR ", stats.Errors)))
			b.WriteString(" ")
		}
		if stats.Warns > 0 {
			b.WriteString(vs.BadgeActive.Copy().Background(theme.Warning).Render(fmt.Sprintf(" %d WRN ", stats.Warns)))
			b.WriteString(" ")
		}
		if stats.Infos > 0 {
			b.WriteString(vs.Badge.Render(fmt.Sprintf(" %d INF ", stats.Infos)))
			b.WriteString(" ")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Log content area
	if v.logs.Len() == 0 {
		// Empty state - show helpful message
		b.WriteString(vs.Subtext.Render("No activity logs yet"))
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Info"))
		b.WriteString(vs.Text.Render("Logs from RCC operations appear here"))
		b.WriteString("\n\n")
		b.WriteString(vs.Label.Render("Tip"))
		b.WriteString(vs.Text.Render("Run a robot task to generate logs"))
	} else {
		// Calculate visible area
		maxVisibleLines := 12
		entries := v.logs.Recent(v.logs.Len())

		// Apply scroll offset
		startIdx := v.scroll
		endIdx := startIdx + maxVisibleLines
		if endIdx > len(entries) {
			endIdx = len(entries)
		}
		if startIdx > len(entries) {
			startIdx = len(entries) - maxVisibleLines
			if startIdx < 0 {
				startIdx = 0
			}
		}

		visibleEntries := entries
		if startIdx < len(entries) && endIdx <= len(entries) {
			visibleEntries = entries[startIdx:endIdx]
		}

		// Render log entries
		for i, entry := range visibleEntries {
			lineNum := startIdx + i + 1

			// Line number - dim
			numStr := fmt.Sprintf("%3d", lineNum)
			b.WriteString(vs.Subtext.Render(numStr))
			b.WriteString(" ")

			// Time
			timeStr := entry.Time.Format("15:04:05")
			b.WriteString(vs.Subtext.Render(timeStr))
			b.WriteString(" ")

			// Level badge
			switch entry.Level {
			case LogError:
				b.WriteString(vs.Error.Render("[ERR]"))
			case LogWarn:
				b.WriteString(vs.Warning.Render("[WRN]"))
			case LogInfo:
				b.WriteString(vs.Info.Render("[INF]"))
			case LogDebug:
				b.WriteString(vs.Subtext.Render("[DBG]"))
			default:
				b.WriteString(vs.Subtext.Render("[---]"))
			}
			b.WriteString(" ")

			// Source if present
			if entry.Source != "" {
				b.WriteString(vs.Accent.Render(entry.Source))
				b.WriteString(vs.Subtext.Render(":"))
				b.WriteString(" ")
			}

			// Message - truncate if too long
			msg := entry.Message
			maxMsgLen := contentWidth - 30
			if len(msg) > maxMsgLen {
				msg = msg[:maxMsgLen-3] + "..."
			}

			msgStyle := vs.Text
			if entry.Level == LogError {
				msgStyle = vs.Error
			} else if entry.Level == LogWarn {
				msgStyle = vs.Warning
			}
			b.WriteString(msgStyle.Render(msg))
			b.WriteString("\n")
		}

		// Scroll indicator
		if v.logs.Len() > maxVisibleLines {
			scrollPct := 0
			maxScroll := v.logs.Len() - maxVisibleLines
			if maxScroll > 0 {
				scrollPct = (v.scroll * 100) / maxScroll
			}
			b.WriteString("\n")
			b.WriteString(vs.Subtext.Render(fmt.Sprintf("Showing %d-%d of %d (%d%%)",
				startIdx+1, endIdx, v.logs.Len(), scrollPct)))
		}
	}

	// Footer
	b.WriteString("\n\n")
	hints := []KeyHint{
		{"j/k", "scroll"},
		{"g/G", "top/bot"},
		{"d/u", "page"},
		{"c", "clear"},
	}
	b.WriteString(RenderFooter(vs, hints, contentWidth))

	return lipgloss.Place(v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		boxStyle.Render(b.String()),
	)
}

// Name implements View
func (v *LogsView) Name() string {
	return "Logs"
}

// ShortHelp implements View
func (v *LogsView) ShortHelp() string {
	return "j/k:scroll g/G:top/bot c:clear"
}
