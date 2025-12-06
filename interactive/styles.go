package interactive

import (
	"github.com/charmbracelet/lipgloss"
)

// Styles holds all the lipgloss styles for the interactive TUI.
// Uses the local Theme for consistency across all dashboard components.
type Styles struct {
	theme Theme

	// Text styles
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	Subtle    lipgloss.Style
	Highlight lipgloss.Style
	Accent    lipgloss.Style
	Error     lipgloss.Style
	Success   lipgloss.Style
	Warning   lipgloss.Style
	Info      lipgloss.Style

	// Layout
	Divider lipgloss.Style

	// Logo/Header
	LogoText   lipgloss.Style
	LogoSubtle lipgloss.Style

	// Breadcrumbs
	CrumbActive   lipgloss.Style
	CrumbInactive lipgloss.Style

	// Status indicators
	StatusKey   lipgloss.Style
	StatusValue lipgloss.Style

	// Tabs
	Tab       lipgloss.Style
	ActiveTab lipgloss.Style

	// Menu/Help bar
	MenuKey       lipgloss.Style
	MenuDesc      lipgloss.Style
	MenuSeparator lipgloss.Style

	// List items
	ListItem         lipgloss.Style
	ListItemSelected lipgloss.Style
	ListItemDesc     lipgloss.Style

	// Tree
	TreeBranch lipgloss.Style
	TreeLeaf   lipgloss.Style

	// Panels
	Panel       lipgloss.Style
	PanelTitle  lipgloss.Style
	PanelBorder lipgloss.Style

	// Table
	TableHeader lipgloss.Style
	TableRow    lipgloss.Style
	TableRowAlt lipgloss.Style

	// Help
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style

	// Spinner
	Spinner lipgloss.Style

	// Step indicators
	StepPending lipgloss.Style
	StepRunning lipgloss.Style
	StepDone    lipgloss.Style
	StepFail    lipgloss.Style

	// Toasts
	ToastInfo    lipgloss.Style
	ToastSuccess lipgloss.Style
	ToastWarning lipgloss.Style
	ToastError   lipgloss.Style

	// Cards & Stats
	StatBox   lipgloss.Style
	StatLabel lipgloss.Style
	StatValue lipgloss.Style
	Badge     lipgloss.Style
}

// NewStyles creates a new Styles instance using the DefaultTheme
func NewStyles() *Styles {
	theme := DefaultTheme()
	return NewStylesWithTheme(theme)
}

// NewStylesWithTheme creates styles using a specific theme
func NewStylesWithTheme(theme Theme) *Styles {
	return &Styles{
		theme: theme,

		// Text styles
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Primary),

		Subtitle: lipgloss.NewStyle().
			Foreground(theme.Secondary),

		Subtle: lipgloss.NewStyle().
			Foreground(theme.TextMuted),

		Highlight: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Accent),

		Accent: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Secondary),

		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Error),

		Success: lipgloss.NewStyle().
			Foreground(theme.Success),

		Warning: lipgloss.NewStyle().
			Foreground(theme.Warning),

		Info: lipgloss.NewStyle().
			Foreground(theme.Info),

		// Layout
		Divider: lipgloss.NewStyle().
			Foreground(theme.BorderDim),

		// Logo/Header
		LogoText: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.TextBright).
			Background(theme.Primary),

		LogoSubtle: lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			PaddingLeft(1),

		// Breadcrumbs
		CrumbActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.TextBright).
			Background(theme.Secondary),

		CrumbInactive: lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Background(theme.Surface),

		// Status indicators
		StatusKey: lipgloss.NewStyle().
			Foreground(theme.TextMuted),

		StatusValue: lipgloss.NewStyle().
			Foreground(theme.Accent),

		// Menu/Help bar
		MenuKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Accent),

		MenuDesc: lipgloss.NewStyle().
			Foreground(theme.Text),

		MenuSeparator: lipgloss.NewStyle().
			Foreground(theme.BorderDim),

		// Tabs
		Tab: lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(theme.TextMuted),

		ActiveTab: lipgloss.NewStyle().
			Padding(0, 2).
			Bold(true).
			Foreground(theme.Primary).
			Background(theme.Highlight),

		// List items
		ListItem: lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(theme.Text),

		ListItemSelected: lipgloss.NewStyle().
			PaddingLeft(2).
			Bold(true).
			Foreground(theme.TextBright).
			Background(theme.Highlight),

		ListItemDesc: lipgloss.NewStyle().
			Foreground(theme.TextMuted),

		// Tree
		TreeBranch: lipgloss.NewStyle().
			Foreground(theme.Border),

		TreeLeaf: lipgloss.NewStyle().
			Foreground(theme.Accent),

		// Panels
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Border).
			Padding(1, 2),

		PanelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Primary),

		PanelBorder: lipgloss.NewStyle().
			Foreground(theme.Border),

		// Table
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Secondary).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(theme.BorderDim),

		TableRow: lipgloss.NewStyle().
			Foreground(theme.Text),

		TableRowAlt: lipgloss.NewStyle().
			Foreground(theme.Text).
			Background(theme.Surface),

		// Help
		HelpKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Accent),

		HelpDesc: lipgloss.NewStyle().
			Foreground(theme.TextMuted),

		// Spinner
		Spinner: lipgloss.NewStyle().
			Foreground(theme.Accent),

		// Step indicators
		StepPending: lipgloss.NewStyle().
			Foreground(theme.TextDim),

		StepRunning: lipgloss.NewStyle().
			Foreground(theme.Accent),

		StepDone: lipgloss.NewStyle().
			Foreground(theme.Success),

		StepFail: lipgloss.NewStyle().
			Foreground(theme.Error),

		// Toasts
		ToastInfo: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Info).
			Padding(0, 1).
			Width(30),

		ToastSuccess: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Success).
			Padding(0, 1).
			Width(30),

		ToastWarning: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Warning).
			Padding(0, 1).
			Width(30),

		ToastError: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Error).
			Padding(0, 1).
			Width(30),

		// Cards & Stats
		StatBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Border).
			Padding(0, 1).
			Width(24),

		StatLabel: lipgloss.NewStyle().
			Foreground(theme.TextMuted).
			Bold(true),

		StatValue: lipgloss.NewStyle().
			Foreground(theme.Accent).
			Bold(true),

		Badge: lipgloss.NewStyle().
			Foreground(theme.Background).
			Background(theme.Accent).
			Padding(0, 1).
			Bold(true),
	}
}

// StepStyle returns the appropriate style for a step status
func (s *Styles) StepStyle(status StepStatus) lipgloss.Style {
	switch status {
	case StepPending:
		return s.StepPending
	case StepRunning:
		return s.StepRunning
	case StepComplete:
		return s.StepDone
	case StepFailed:
		return s.StepFail
	case StepSkipped:
		return s.StepPending
	default:
		return s.ListItem
	}
}

// StepIcon returns the appropriate icon for a step status
func (s *Styles) StepIcon(status StepStatus, spinnerFrame string) string {
	if Iconic {
		switch status {
		case StepPending:
			return "○"
		case StepRunning:
			return spinnerFrame
		case StepComplete:
			return "●"
		case StepFailed:
			return "✗"
		case StepSkipped:
			return "◌"
		default:
			return "○"
		}
	}
	// ASCII fallback
	switch status {
	case StepPending:
		return "o"
	case StepRunning:
		return spinnerFrame
	case StepComplete:
		return "*"
	case StepFailed:
		return "x"
	case StepSkipped:
		return "-"
	default:
		return "o"
	}
}
