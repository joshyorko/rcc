package pretty

import "github.com/charmbracelet/lipgloss"

// Theme defines the color palette and styles for the dashboard
type Theme struct {
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Error     lipgloss.Color
	Text      lipgloss.Color
	Subtext   lipgloss.Color
	Border    lipgloss.Color
	Background lipgloss.Color
}

// DefaultTheme returns the standard RCC theme
func DefaultTheme() Theme {
	return Theme{
		Primary:    lipgloss.Color("39"),  // Blue
		Secondary:  lipgloss.Color("63"),  // Purple/Blue
		Success:    lipgloss.Color("42"),  // Green
		Warning:    lipgloss.Color("214"), // Orange
		Error:      lipgloss.Color("196"), // Red
		Text:       lipgloss.Color("255"), // White
		Subtext:    lipgloss.Color("245"), // Gray
		Border:     lipgloss.Color("63"),  // Purple/Blue
		Background: lipgloss.Color("0"),   // Black
	}
}

// Styles container for pre-computed styles
type Styles struct {
	Theme Theme

	// Base styles
	Base   lipgloss.Style
	Header lipgloss.Style
	Footer lipgloss.Style
	Border lipgloss.Style

	// Text styles
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Text     lipgloss.Style
	Subtext  lipgloss.Style
	Success  lipgloss.Style
	Warning  lipgloss.Style
	Error    lipgloss.Style

	// Component styles
	Progress    lipgloss.Style
	Spinner     lipgloss.Style
	LogBox      lipgloss.Style
	StepPending lipgloss.Style
	StepRunning lipgloss.Style
	StepDone    lipgloss.Style
	StepFail    lipgloss.Style
}

// NewStyles creates a new Styles struct from a Theme
func NewStyles(t Theme) Styles {
	s := Styles{Theme: t}

	s.Base = lipgloss.NewStyle().Foreground(t.Text)
	s.Header = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	s.Footer = lipgloss.NewStyle().Foreground(t.Subtext)
	s.Border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border)

	s.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	s.Subtitle = lipgloss.NewStyle().Foreground(t.Secondary)
	s.Text = lipgloss.NewStyle().Foreground(t.Text)
	s.Subtext = lipgloss.NewStyle().Foreground(t.Subtext)

	s.Success = lipgloss.NewStyle().Foreground(t.Success)
	s.Warning = lipgloss.NewStyle().Foreground(t.Warning)
	s.Error = lipgloss.NewStyle().Foreground(t.Error)

	s.Progress = lipgloss.NewStyle().Foreground(t.Primary)
	s.Spinner = lipgloss.NewStyle().Foreground(t.Primary)
	s.LogBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Subtext).
		Padding(0, 1)

	s.StepPending = lipgloss.NewStyle().Foreground(t.Subtext)
	s.StepRunning = lipgloss.NewStyle().Foreground(t.Primary)
	s.StepDone = lipgloss.NewStyle().Foreground(t.Success)
	s.StepFail = lipgloss.NewStyle().Foreground(t.Error)

	return s
}
