package pretty

import (
	"os"
	"strings"
)

// Advanced color support for RCC terminal UI
// Provides 256-color palette, 24-bit TrueColor, and semantic color functions
// Respects NO_COLOR, COLORTERM, and TERM environment variables

// ColorMode represents the level of color support available in the terminal
type ColorMode int

const (
	// ColorModeNone indicates no color support (NO_COLOR set or dumb terminal)
	ColorModeNone ColorMode = iota
	// ColorModeBasic indicates 16 basic ANSI colors
	ColorModeBasic
	// ColorMode256 indicates 256-color palette support
	ColorMode256
	// ColorModeTrueColor indicates 24-bit RGB (16.7 million colors) support
	ColorModeTrueColor
)

var (
	detectedColorMode ColorMode
	colorModeDetected bool
)

// DetectColorMode checks environment variables to determine terminal color capabilities.
// Checks in order: NO_COLOR, COLORTERM, TERM.
func DetectColorMode() ColorMode {
	if colorModeDetected {
		return detectedColorMode
	}

	// Check NO_COLOR first - if set to any value, disable colors
	if os.Getenv("NO_COLOR") != "" {
		detectedColorMode = ColorModeNone
		colorModeDetected = true
		return detectedColorMode
	}

	// Check COLORTERM for TrueColor support
	colorterm := os.Getenv("COLORTERM")
	if colorterm == "truecolor" || colorterm == "24bit" {
		detectedColorMode = ColorModeTrueColor
		colorModeDetected = true
		return detectedColorMode
	}

	// Check TERM for color capabilities
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		detectedColorMode = ColorModeNone
		colorModeDetected = true
		return detectedColorMode
	}

	// Check for 256-color support in TERM
	if strings.Contains(term, "256color") {
		detectedColorMode = ColorMode256
		colorModeDetected = true
		return detectedColorMode
	}

	// Default to basic 16-color support
	detectedColorMode = ColorModeBasic
	colorModeDetected = true
	return detectedColorMode
}

// SeverityColor returns the appropriate ANSI color code for a log severity level.
// Mappings: trace→dim, debug→gray, info→white, warning→yellow, error→red, critical→bright red+bold
func SeverityColor(level string) string {
	if Colorless || Disabled {
		return ""
	}

	switch strings.ToLower(level) {
	case "trace":
		return Faint // dim/faint text (CSI 2m)
	case "debug":
		return Grey // gray (CSI 90m)
	case "info":
		return White // white (CSI 97m)
	case "warning", "warn":
		return Yellow // yellow (CSI 93m)
	case "error":
		return Red // red (CSI 91m)
	case "critical", "fatal":
		return csif("91;1m") // bright red + bold (CSI 91;1m)
	default:
		return ""
	}
}

// StatusColor returns the appropriate ANSI color code for an operation status.
// Mappings: pending→gray, running→cyan, complete→green, failed→red, skipped→dim
func StatusColor(status string) string {
	if Colorless || Disabled {
		return ""
	}

	switch strings.ToLower(status) {
	case "pending":
		return Grey // gray (CSI 90m)
	case "running", "in-progress", "in_progress":
		return Cyan // cyan (CSI 96m)
	case "complete", "completed", "success", "done":
		return Green // green (CSI 92m)
	case "failed", "failure", "error":
		return Red // red (CSI 91m)
	case "skipped", "skip":
		return Faint // dim (CSI 2m)
	default:
		return ""
	}
}

// Color256 returns an ANSI escape code for 256-color foreground text.
// Returns empty string if the terminal doesn't support 256 colors.
// Valid range: 0-255
func Color256(n int) string {
	if Colorless || Disabled {
		return ""
	}

	mode := DetectColorMode()
	if mode < ColorMode256 {
		return ""
	}

	// Validate range
	if n < 0 || n > 255 {
		return ""
	}

	return csif("38;5;%dm", n)
}

// BGColor256 returns an ANSI escape code for 256-color background.
// Returns empty string if the terminal doesn't support 256 colors.
// Valid range: 0-255
func BGColor256(n int) string {
	if Colorless || Disabled {
		return ""
	}

	mode := DetectColorMode()
	if mode < ColorMode256 {
		return ""
	}

	// Validate range
	if n < 0 || n > 255 {
		return ""
	}

	return csif("48;5;%dm", n)
}

// RGB returns an ANSI escape code for 24-bit TrueColor foreground text.
// Returns empty string if the terminal doesn't support TrueColor.
// Valid range for each component: 0-255
func RGB(r, g, b int) string {
	if Colorless || Disabled {
		return ""
	}

	mode := DetectColorMode()
	if mode < ColorModeTrueColor {
		return ""
	}

	// Validate ranges
	if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 {
		return ""
	}

	return csif("38;2;%d;%d;%dm", r, g, b)
}

// BGRGB returns an ANSI escape code for 24-bit TrueColor background.
// Returns empty string if the terminal doesn't support TrueColor.
// Valid range for each component: 0-255
func BGRGB(r, g, b int) string {
	if Colorless || Disabled {
		return ""
	}

	mode := DetectColorMode()
	if mode < ColorModeTrueColor {
		return ""
	}

	// Validate ranges
	if r < 0 || r > 255 || g < 0 || g > 255 || b < 0 || b > 255 {
		return ""
	}

	return csif("48;2;%d;%d;%dm", r, g, b)
}
