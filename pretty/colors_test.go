package pretty

import (
	"os"
	"testing"
)

func TestDetectColorMode(t *testing.T) {
	// Save original env
	origNoColor := os.Getenv("NO_COLOR")
	origColorterm := os.Getenv("COLORTERM")
	origTerm := os.Getenv("TERM")
	defer func() {
		os.Setenv("NO_COLOR", origNoColor)
		os.Setenv("COLORTERM", origColorterm)
		os.Setenv("TERM", origTerm)
		// Reset detection for next test
		colorModeDetected = false
	}()

	tests := []struct {
		name      string
		noColor   string
		colorterm string
		term      string
		expected  ColorMode
	}{
		{
			name:     "NO_COLOR set disables colors",
			noColor:  "1",
			expected: ColorModeNone,
		},
		{
			name:      "COLORTERM=truecolor enables TrueColor",
			colorterm: "truecolor",
			term:      "xterm-256color",
			expected:  ColorModeTrueColor,
		},
		{
			name:      "COLORTERM=24bit enables TrueColor",
			colorterm: "24bit",
			term:      "xterm-256color",
			expected:  ColorModeTrueColor,
		},
		{
			name:     "TERM=xterm-256color enables 256 colors",
			term:     "xterm-256color",
			expected: ColorMode256,
		},
		{
			name:     "TERM=dumb disables colors",
			term:     "dumb",
			expected: ColorModeNone,
		},
		{
			name:     "Empty TERM disables colors",
			term:     "",
			expected: ColorModeNone,
		},
		{
			name:     "TERM=xterm enables basic colors",
			term:     "xterm",
			expected: ColorModeBasic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset detection for each test
			colorModeDetected = false

			// Set environment
			if tt.noColor != "" {
				os.Setenv("NO_COLOR", tt.noColor)
			} else {
				os.Unsetenv("NO_COLOR")
			}

			if tt.colorterm != "" {
				os.Setenv("COLORTERM", tt.colorterm)
			} else {
				os.Unsetenv("COLORTERM")
			}

			if tt.term != "" {
				os.Setenv("TERM", tt.term)
			} else {
				os.Unsetenv("TERM")
			}

			// Test
			result := DetectColorMode()
			if result != tt.expected {
				t.Errorf("DetectColorMode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSeverityColor(t *testing.T) {
	// Save original state
	origColorless := Colorless
	origDisabled := Disabled
	defer func() {
		Colorless = origColorless
		Disabled = origDisabled
	}()

	// Enable colors for testing
	Colorless = false
	Disabled = false

	tests := []struct {
		level    string
		expected string
	}{
		{"trace", Faint},
		{"debug", Grey},
		{"info", White},
		{"warning", Yellow},
		{"warn", Yellow},
		{"error", Red},
		{"critical", csif("91;1m")},
		{"fatal", csif("91;1m")},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			result := SeverityColor(tt.level)
			if result != tt.expected {
				t.Errorf("SeverityColor(%q) = %q, want %q", tt.level, result, tt.expected)
			}
		})
	}

	// Test colorless mode
	Colorless = true
	result := SeverityColor("error")
	if result != "" {
		t.Errorf("SeverityColor() in colorless mode should return empty string, got %q", result)
	}
}

func TestStatusColor(t *testing.T) {
	// Save original state
	origColorless := Colorless
	origDisabled := Disabled
	defer func() {
		Colorless = origColorless
		Disabled = origDisabled
	}()

	// Enable colors for testing
	Colorless = false
	Disabled = false

	tests := []struct {
		status   string
		expected string
	}{
		{"pending", Grey},
		{"running", Cyan},
		{"in-progress", Cyan},
		{"in_progress", Cyan},
		{"complete", Green},
		{"completed", Green},
		{"success", Green},
		{"done", Green},
		{"failed", Red},
		{"failure", Red},
		{"error", Red},
		{"skipped", Faint},
		{"skip", Faint},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := StatusColor(tt.status)
			if result != tt.expected {
				t.Errorf("StatusColor(%q) = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}

	// Test disabled mode
	Disabled = true
	result := StatusColor("running")
	if result != "" {
		t.Errorf("StatusColor() in disabled mode should return empty string, got %q", result)
	}
}

func TestColor256(t *testing.T) {
	// Save original state
	origColorless := Colorless
	origDisabled := Disabled
	defer func() {
		Colorless = origColorless
		Disabled = origDisabled
		colorModeDetected = false
	}()

	// Enable colors and set 256-color mode
	Colorless = false
	Disabled = false
	colorModeDetected = true
	detectedColorMode = ColorMode256

	tests := []struct {
		name     string
		input    int
		expected string
	}{
		{"valid color 0", 0, csif("38;5;0m")},
		{"valid color 255", 255, csif("38;5;255m")},
		{"valid color 123", 123, csif("38;5;123m")},
		{"invalid negative", -1, ""},
		{"invalid too large", 256, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Color256(tt.input)
			if result != tt.expected {
				t.Errorf("Color256(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}

	// Test with basic color mode (should return empty)
	detectedColorMode = ColorModeBasic
	result := Color256(100)
	if result != "" {
		t.Errorf("Color256() in basic mode should return empty string, got %q", result)
	}
}

func TestRGB(t *testing.T) {
	// Save original state
	origColorless := Colorless
	origDisabled := Disabled
	defer func() {
		Colorless = origColorless
		Disabled = origDisabled
		colorModeDetected = false
	}()

	// Enable colors and set TrueColor mode
	Colorless = false
	Disabled = false
	colorModeDetected = true
	detectedColorMode = ColorModeTrueColor

	tests := []struct {
		name     string
		r, g, b  int
		expected string
	}{
		{"valid black", 0, 0, 0, csif("38;2;0;0;0m")},
		{"valid white", 255, 255, 255, csif("38;2;255;255;255m")},
		{"valid color", 128, 64, 192, csif("38;2;128;64;192m")},
		{"invalid r negative", -1, 0, 0, ""},
		{"invalid r too large", 256, 0, 0, ""},
		{"invalid g negative", 0, -1, 0, ""},
		{"invalid g too large", 0, 256, 0, ""},
		{"invalid b negative", 0, 0, -1, ""},
		{"invalid b too large", 0, 0, 256, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RGB(tt.r, tt.g, tt.b)
			if result != tt.expected {
				t.Errorf("RGB(%d, %d, %d) = %q, want %q", tt.r, tt.g, tt.b, result, tt.expected)
			}
		})
	}

	// Test with 256-color mode (should return empty)
	detectedColorMode = ColorMode256
	result := RGB(100, 100, 100)
	if result != "" {
		t.Errorf("RGB() in 256-color mode should return empty string, got %q", result)
	}
}

func TestBGColor256(t *testing.T) {
	// Save original state
	origColorless := Colorless
	origDisabled := Disabled
	defer func() {
		Colorless = origColorless
		Disabled = origDisabled
		colorModeDetected = false
	}()

	// Enable colors and set 256-color mode
	Colorless = false
	Disabled = false
	colorModeDetected = true
	detectedColorMode = ColorMode256

	result := BGColor256(100)
	expected := csif("48;5;100m")
	if result != expected {
		t.Errorf("BGColor256(100) = %q, want %q", result, expected)
	}
}

func TestBGRGB(t *testing.T) {
	// Save original state
	origColorless := Colorless
	origDisabled := Disabled
	defer func() {
		Colorless = origColorless
		Disabled = origDisabled
		colorModeDetected = false
	}()

	// Enable colors and set TrueColor mode
	Colorless = false
	Disabled = false
	colorModeDetected = true
	detectedColorMode = ColorModeTrueColor

	result := BGRGB(128, 64, 192)
	expected := csif("48;2;128;64;192m")
	if result != expected {
		t.Errorf("BGRGB(128, 64, 192) = %q, want %q", result, expected)
	}
}
