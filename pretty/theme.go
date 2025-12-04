package pretty

import "github.com/charmbracelet/lipgloss"

// Theme defines the color palette and styles for the dashboard
// Inspired by Tokyo Night color scheme for a modern, polished look
type Theme struct {
	// Core brand colors
	Primary   lipgloss.AdaptiveColor
	Secondary lipgloss.AdaptiveColor
	Accent    lipgloss.AdaptiveColor

	// Semantic colors
	Success lipgloss.AdaptiveColor
	Warning lipgloss.AdaptiveColor
	Error   lipgloss.AdaptiveColor
	Info    lipgloss.AdaptiveColor

	// Text hierarchy
	TextBright lipgloss.AdaptiveColor
	Text       lipgloss.AdaptiveColor
	TextMuted  lipgloss.AdaptiveColor
	TextDim    lipgloss.AdaptiveColor

	// UI elements
	Border     lipgloss.AdaptiveColor
	BorderDim  lipgloss.AdaptiveColor
	Background lipgloss.AdaptiveColor
	Surface    lipgloss.AdaptiveColor
	Highlight  lipgloss.AdaptiveColor
}

// DefaultTheme returns the standard RCC theme with vibrant Tokyo Night-inspired colors
func DefaultTheme() Theme {
	return Theme{
		// Core brand - Vibrant blue tones
		Primary:   lipgloss.AdaptiveColor{Dark: "#82aaff", Light: "#2e7de9"}, // Brighter blue
		Secondary: lipgloss.AdaptiveColor{Dark: "#c792ea", Light: "#7847bd"}, // Brighter purple
		Accent:    lipgloss.AdaptiveColor{Dark: "#89ddff", Light: "#007197"}, // Brighter cyan

		// Semantic - More saturated
		Success: lipgloss.AdaptiveColor{Dark: "#c3e88d", Light: "#587539"}, // Brighter green
		Warning: lipgloss.AdaptiveColor{Dark: "#ffcb6b", Light: "#8c6c3e"}, // Brighter orange
		Error:   lipgloss.AdaptiveColor{Dark: "#ff5370", Light: "#f52a65"}, // Brighter red
		Info:    lipgloss.AdaptiveColor{Dark: "#89ddff", Light: "#0891b2"}, // Brighter cyan

		// Text - Better contrast
		TextBright: lipgloss.AdaptiveColor{Dark: "#eeffff", Light: "#343b58"}, // Near white
		Text:       lipgloss.AdaptiveColor{Dark: "#bfc7d5", Light: "#4c505e"}, // Lighter gray
		TextMuted:  lipgloss.AdaptiveColor{Dark: "#697098", Light: "#8990a3"}, // More visible muted
		TextDim:    lipgloss.AdaptiveColor{Dark: "#4e5579", Light: "#b4b5b9"}, // Slightly brighter dim

		// UI - Better visibility
		Border:     lipgloss.AdaptiveColor{Dark: "#5c6370", Light: "#c4c8da"}, // More visible border
		BorderDim:  lipgloss.AdaptiveColor{Dark: "#3e4452", Light: "#dfe1e8"},
		Background: lipgloss.AdaptiveColor{Dark: "#1a1b26", Light: "#f5f5f5"},
		Surface:    lipgloss.AdaptiveColor{Dark: "#292d3e", Light: "#e9e9ec"}, // Slightly lighter surface
		Highlight:  lipgloss.AdaptiveColor{Dark: "#3a3f58", Light: "#e1e2e7"},
	}
}

// Styles container for pre-computed styles
type Styles struct {
	Theme Theme

	// Layout
	Box         lipgloss.Style
	BoxActive   lipgloss.Style
	BoxInactive lipgloss.Style
	Header      lipgloss.Style
	Footer      lipgloss.Style
	Content     lipgloss.Style

	// Text styles
	Title       lipgloss.Style
	Subtitle    lipgloss.Style
	Text        lipgloss.Style
	Subtext     lipgloss.Style
	Label       lipgloss.Style
	Success     lipgloss.Style
	Warning     lipgloss.Style
	Error       lipgloss.Style
	Accent      lipgloss.Style
	Info        lipgloss.Style

	// Component styles
	Progress     lipgloss.Style
	Spinner      lipgloss.Style
	LogBox       lipgloss.Style
	LogEntry     lipgloss.Style
	StatusBar    lipgloss.Style

	// Step styles
	StepPending  lipgloss.Style
	StepRunning  lipgloss.Style
	StepDone     lipgloss.Style
	StepFail     lipgloss.Style
	StepSkipped  lipgloss.Style

	// Special
	Highlight    lipgloss.Style
	Badge        lipgloss.Style
	BadgeSuccess lipgloss.Style
	BadgeError   lipgloss.Style
	KeyHint      lipgloss.Style
}

// NewStyles creates a new Styles struct from a Theme
func NewStyles(t Theme) Styles {
	s := Styles{Theme: t}

	// Layout styles - More spacious
	s.Box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Padding(1, 2)

	s.BoxActive = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2)

	s.BoxInactive = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderDim).
		Padding(1, 2)

	s.Header = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.TextBright).
		Padding(0, 1)

	s.Footer = lipgloss.NewStyle().
		Foreground(t.TextMuted).
		Padding(0, 1)

	s.Content = lipgloss.NewStyle().
		Foreground(t.Text).
		Padding(0, 2)

	// Text styles
	s.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Primary)

	s.Subtitle = lipgloss.NewStyle().
		Foreground(t.Secondary)

	s.Text = lipgloss.NewStyle().
		Foreground(t.Text)

	s.Subtext = lipgloss.NewStyle().
		Foreground(t.TextMuted)

	s.Label = lipgloss.NewStyle().
		Foreground(t.TextDim).
		Italic(true)

	s.Success = lipgloss.NewStyle().Foreground(t.Success)
	s.Warning = lipgloss.NewStyle().Foreground(t.Warning)
	s.Error = lipgloss.NewStyle().Foreground(t.Error)
	s.Accent = lipgloss.NewStyle().Foreground(t.Accent)
	s.Info = lipgloss.NewStyle().Foreground(t.Info)

	// Component styles
	s.Progress = lipgloss.NewStyle().Foreground(t.Primary)

	s.Spinner = lipgloss.NewStyle().Foreground(t.Accent)

	s.LogBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderDim).
		Foreground(t.TextMuted).
		Padding(0, 1)

	s.LogEntry = lipgloss.NewStyle().
		Foreground(t.TextMuted)

	s.StatusBar = lipgloss.NewStyle().
		Foreground(t.TextMuted).
		Background(t.Surface).
		Padding(0, 1)

	// Step styles with icons consideration
	s.StepPending = lipgloss.NewStyle().
		Foreground(t.TextDim)

	s.StepRunning = lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true)

	s.StepDone = lipgloss.NewStyle().
		Foreground(t.Success)

	s.StepFail = lipgloss.NewStyle().
		Foreground(t.Error).
		Bold(true)

	s.StepSkipped = lipgloss.NewStyle().
		Foreground(t.TextDim).
		Italic(true)

	// Special styles
	s.Highlight = lipgloss.NewStyle().
		Background(t.Highlight).
		Foreground(t.TextBright)

	s.Badge = lipgloss.NewStyle().
		Padding(0, 1).
		Background(t.Surface).
		Foreground(t.Text)

	s.BadgeSuccess = lipgloss.NewStyle().
		Padding(0, 1).
		Background(t.Success).
		Foreground(lipgloss.Color("#1a1b26"))

	s.BadgeError = lipgloss.NewStyle().
		Padding(0, 1).
		Background(t.Error).
		Foreground(lipgloss.Color("#1a1b26"))

	s.KeyHint = lipgloss.NewStyle().
		Foreground(t.TextDim).
		Background(t.Surface).
		Padding(0, 1)

	return s
}

// Semantic style helpers for common patterns

// StepStyle returns the appropriate style for a step status
func (s Styles) StepStyle(status StepStatus) lipgloss.Style {
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
		return s.StepSkipped
	default:
		return s.Text
	}
}

// StepIcon returns the appropriate icon for a step status
func (s Styles) StepIcon(status StepStatus, spinnerFrame string) string {
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
