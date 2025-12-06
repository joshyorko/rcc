package interactive

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/joshyorko/rcc/common"
)

// ViewStyles provides consistent styling for all views
type ViewStyles struct {
	theme        Theme
	Title        lipgloss.Style
	Subtext      lipgloss.Style
	Label        lipgloss.Style
	Text         lipgloss.Style
	Accent       lipgloss.Style
	Success      lipgloss.Style
	Warning      lipgloss.Style
	Error        lipgloss.Style
	Info         lipgloss.Style
	Separator    lipgloss.Style
	KeyHint      lipgloss.Style
	Selected     lipgloss.Style
	Normal       lipgloss.Style
	Badge        lipgloss.Style
	BadgeActive  lipgloss.Style
	ToastInfo    lipgloss.Style
	ToastSuccess lipgloss.Style
	ToastWarning lipgloss.Style
	ToastError   lipgloss.Style

	// Table/List styles
	TableHeader      lipgloss.Style
	TableRow         lipgloss.Style
	TableRowAlt      lipgloss.Style
	ListItemSelected lipgloss.Style

	// Help styles
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style
}

// NewViewStyles creates consistent styles from a theme
func NewViewStyles(theme Theme) ViewStyles {
	return ViewStyles{
		theme:        theme,
		Title:        lipgloss.NewStyle().Bold(true).Foreground(theme.Primary),
		Subtext:      lipgloss.NewStyle().Foreground(theme.TextMuted),
		Label:        lipgloss.NewStyle().Foreground(theme.TextDim).Width(14),
		Text:         lipgloss.NewStyle().Foreground(theme.Text),
		Accent:       lipgloss.NewStyle().Foreground(theme.Accent),
		Success:      lipgloss.NewStyle().Foreground(theme.Success),
		Warning:      lipgloss.NewStyle().Foreground(theme.Warning),
		Error:        lipgloss.NewStyle().Foreground(theme.Error),
		Info:         lipgloss.NewStyle().Foreground(theme.Info),
		Separator:    lipgloss.NewStyle().Foreground(theme.BorderDim),
		KeyHint:      lipgloss.NewStyle().Foreground(theme.TextDim).Background(theme.Surface).Padding(0, 1),
		Selected:     lipgloss.NewStyle().Foreground(theme.TextBright).Background(theme.Highlight).Bold(true).Padding(0, 1),
		Normal:       lipgloss.NewStyle().Foreground(theme.Text).Padding(0, 1),
		Badge:        lipgloss.NewStyle().Background(theme.Surface).Foreground(theme.Text).Padding(0, 1),
		BadgeActive:  lipgloss.NewStyle().Background(theme.Accent).Foreground(lipgloss.Color("#1a1b26")).Padding(0, 1).Bold(true),
		ToastInfo:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Info).Padding(0, 1).Width(30),
		ToastSuccess: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Success).Padding(0, 1).Width(30),
		ToastWarning: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Warning).Padding(0, 1).Width(30),
		ToastError:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.Error).Padding(0, 1).Width(30),

		// Table/List
		TableHeader:      lipgloss.NewStyle().Bold(true).Foreground(theme.Secondary).BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(theme.BorderDim),
		TableRow:         lipgloss.NewStyle().Foreground(theme.Text),
		TableRowAlt:      lipgloss.NewStyle().Foreground(theme.Text).Background(theme.Surface),
		ListItemSelected: lipgloss.NewStyle().Foreground(theme.TextBright).Background(theme.Highlight).Bold(true),

		// Help
		HelpKey:  lipgloss.NewStyle().Bold(true).Foreground(theme.Accent),
		HelpDesc: lipgloss.NewStyle().Foreground(theme.TextMuted),
	}
}

// ViewBox provides a consistent container for all views
type ViewBox struct {
	Width        int
	Height       int
	ContentWidth int
	Theme        Theme
	BoxStyle     lipgloss.Style
}

// NewViewBox creates a responsive box for view content
func NewViewBox(width, height int, theme Theme) ViewBox {
	boxWidth := width - 8
	if boxWidth < 60 {
		boxWidth = 60
	}

	return ViewBox{
		Width:        boxWidth,
		Height:       height,
		ContentWidth: boxWidth - 6,
		Theme:        theme,
		BoxStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Border).
			Padding(1, 2).
			Width(boxWidth),
	}
}

// Render wraps content in a centered box
func (vb ViewBox) Render(content string, termWidth, termHeight int) string {
	box := vb.BoxStyle.Render(content)
	return lipgloss.Place(
		termWidth,
		termHeight,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

// RenderHeader creates a consistent header with RCC version and view title
func RenderHeader(vs ViewStyles, viewTitle string, subtitle string, contentWidth int) string {
	var b strings.Builder

	// RCC + version on left, view title
	rccBadge := vs.Title.Render("RCC")
	version := vs.Subtext.Render(" " + common.Version + " ")
	title := vs.Accent.Bold(true).Render(viewTitle)

	b.WriteString(rccBadge)
	b.WriteString(version)
	b.WriteString(vs.Separator.Render("|"))
	b.WriteString(" ")
	b.WriteString(title)

	if subtitle != "" {
		b.WriteString(" ")
		b.WriteString(vs.Subtext.Render(subtitle))
	}
	b.WriteString("\n")
	b.WriteString(vs.Separator.Render(strings.Repeat("─", contentWidth)))
	b.WriteString("\n")
	return b.String()
}

// RenderFooter creates a consistent footer with key hints
func RenderFooter(vs ViewStyles, hints []KeyHint, contentWidth int) string {
	var b strings.Builder
	b.WriteString(vs.Separator.Render(strings.Repeat("─", contentWidth)))
	b.WriteString("\n")

	for i, hint := range hints {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(vs.KeyHint.Render(hint.Key))
		b.WriteString(" ")
		b.WriteString(vs.Subtext.Render(hint.Desc))
	}
	return b.String()
}

// KeyHint represents a keyboard shortcut hint
type KeyHint struct {
	Key  string
	Desc string
}
