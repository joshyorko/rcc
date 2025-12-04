package interactive

import "github.com/charmbracelet/lipgloss"

// RCC color palette - matches the pretty package theme
var (
	// Core brand colors - matching RCC's existing style
	ColorPrimary   = lipgloss.Color("96") // Cyan (matches pretty.Cyan)
	ColorSecondary = lipgloss.Color("95") // Magenta (matches pretty.Magenta)
	ColorAccent    = lipgloss.Color("94") // Blue (matches pretty.Blue)

	// Semantic colors - matching RCC's existing style
	ColorSuccess = lipgloss.Color("92") // Green (matches pretty.Green)
	ColorWarning = lipgloss.Color("93") // Yellow (matches pretty.Yellow)
	ColorError   = lipgloss.Color("91") // Red (matches pretty.Red)
	ColorInfo    = lipgloss.Color("96") // Cyan

	// Text colors
	ColorTextBright = lipgloss.Color("97") // White (matches pretty.White)
	ColorText       = lipgloss.Color("15") // Light gray
	ColorTextMuted  = lipgloss.Color("90") // Grey (matches pretty.Grey)
	ColorTextDim    = lipgloss.Color("240")

	// UI elements
	ColorBorder     = lipgloss.Color("240")
	ColorBorderDim  = lipgloss.Color("236")
	ColorBackground = lipgloss.Color("234")
	ColorSurface    = lipgloss.Color("236")
	ColorHighlight  = lipgloss.Color("238")

	// Progress bar specific colors
	ColorProgress     = lipgloss.Color("96") // Cyan for progress
	ColorProgressDone = lipgloss.Color("92") // Green for complete
)

// Styles holds all the lipgloss styles for the app
type Styles struct {
	// Text styles
	Title       lipgloss.Style
	Subtitle    lipgloss.Style
	Subtle      lipgloss.Style
	Highlight   lipgloss.Style
	Error       lipgloss.Style
	Success     lipgloss.Style
	Warning     lipgloss.Style
	Info        lipgloss.Style

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

	// Status bar (legacy)
	StatusBarKey   lipgloss.Style
	StatusBarValue lipgloss.Style

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
}

// NewStyles creates a new Styles instance with Tokyo Night theme
func NewStyles() *Styles {
	return &Styles{
		// Text styles
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary),

		Subtitle: lipgloss.NewStyle().
			Foreground(ColorSecondary),

		Subtle: lipgloss.NewStyle().
			Foreground(ColorTextMuted),

		Highlight: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent),

		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorError),

		Success: lipgloss.NewStyle().
			Foreground(ColorSuccess),

		Warning: lipgloss.NewStyle().
			Foreground(ColorWarning),

		Info: lipgloss.NewStyle().
			Foreground(ColorInfo),

		// Layout
		Divider: lipgloss.NewStyle().
			Foreground(ColorBorderDim),

		// Logo/Header
		LogoText: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTextBright).
			Background(ColorPrimary),

		LogoSubtle: lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			PaddingLeft(1),

		// Breadcrumbs
		CrumbActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTextBright).
			Background(ColorSecondary),

		CrumbInactive: lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Background(ColorSurface),

		// Status indicators
		StatusKey: lipgloss.NewStyle().
			Foreground(ColorTextMuted),

		StatusValue: lipgloss.NewStyle().
			Foreground(ColorAccent),

		// Menu/Help bar
		MenuKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent),

		MenuDesc: lipgloss.NewStyle().
			Foreground(ColorText),

		MenuSeparator: lipgloss.NewStyle().
			Foreground(ColorBorderDim),

		// Tabs
		Tab: lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(ColorTextMuted),

		ActiveTab: lipgloss.NewStyle().
			Padding(0, 2).
			Bold(true).
			Foreground(ColorPrimary).
			Background(ColorHighlight),

		// Status bar
		StatusBarKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTextBright).
			Background(ColorPrimary).
			Padding(0, 1),

		StatusBarValue: lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Background(ColorSurface).
			Padding(0, 1),

		// List items
		ListItem: lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(ColorText),

		ListItemSelected: lipgloss.NewStyle().
			PaddingLeft(2).
			Bold(true).
			Foreground(ColorTextBright).
			Background(ColorHighlight),

		ListItemDesc: lipgloss.NewStyle().
			Foreground(ColorTextMuted),

		// Tree
		TreeBranch: lipgloss.NewStyle().
			Foreground(ColorBorder),

		TreeLeaf: lipgloss.NewStyle().
			Foreground(ColorAccent),

		// Panels
		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2),

		PanelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary),

		PanelBorder: lipgloss.NewStyle().
			Foreground(ColorBorder),

		// Table
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSecondary).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorBorderDim),

		TableRow: lipgloss.NewStyle().
			Foreground(ColorText),

		TableRowAlt: lipgloss.NewStyle().
			Foreground(ColorText).
			Background(ColorSurface),

		// Help
		HelpKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent),

		HelpDesc: lipgloss.NewStyle().
			Foreground(ColorTextMuted),

		// Spinner
		Spinner: lipgloss.NewStyle().
			Foreground(ColorAccent),

		// Step indicators
		StepPending: lipgloss.NewStyle().
			Foreground(ColorTextDim),

		StepRunning: lipgloss.NewStyle().
			Foreground(ColorAccent),

		StepDone: lipgloss.NewStyle().
			Foreground(ColorSuccess),

		StepFail: lipgloss.NewStyle().
			Foreground(ColorError),
	}
}
