package interactive

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

// Iconic controls whether to use Unicode icons or ASCII fallback
// This is set by the pretty package during Setup()
var Iconic = true
