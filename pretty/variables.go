package pretty

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/joshyorko/rcc/common"
)

var (
	Colorless   bool
	Iconic      bool
	Disabled    bool
	Interactive bool
	White       string
	Grey        string
	Black       string
	Red         string
	Green       string
	Blue        string
	Yellow      string
	Magenta     string
	Cyan        string
	Reset       string
	Sparkles    string
	Rocket      string
	Home        string
	Clear       string
	Bold        string
	Faint       string
	Italic      string
	Underline   string
)

func Setup() {
	stdin := isatty.IsTerminal(os.Stdin.Fd())
	stdout := isatty.IsTerminal(os.Stdout.Fd())
	stderr := isatty.IsTerminal(os.Stderr.Fd())

	// Check NO_COLOR environment variable - if set, disable colors
	if os.Getenv("NO_COLOR") != "" {
		Colorless = true
	}

	// Handle missing TERM environment variable by defaulting to non-color mode
	if os.Getenv("TERM") == "" {
		Colorless = true
	}

	// Handle stdin pipe + stdout TTY scenario: allow visual output but disable interactive prompts
	// Interactive requires all three to be TTY for safe prompt handling
	Interactive = stdin && stdout && stderr

	// Allow visual output (colors) if stdout is a TTY, even if stdin is piped
	visualOutput := stdout && !Colorless

	localSetup(Interactive)

	common.Trace("Interactive mode enabled: %v; colors enabled: %v; icons enabled: %v", Interactive, !Disabled, Iconic)
	if visualOutput && !Disabled {
		White = csi("97m")
		Grey = csi("90m")
		Black = csi("30m")
		Red = csi("91m")
		Green = csi("92m")
		Yellow = csi("93m")
		Blue = csi("94m")
		Magenta = csi("95m")
		Cyan = csi("96m")
		Reset = csi("0m")
		Home = csi("1;1H")
		Clear = csi("0J")
		Bold = csi("1m")
		Faint = csi("2m")
		Italic = csi("3m")
		Underline = csi("4m")
	}
	if Iconic && !Colorless {
		Sparkles = "\u2728 "
		Rocket = "\U0001F680 "
	}
}

// Color Conventions:
// - Green: Success messages
// - Yellow: Warnings
// - Red: Errors
// - Bold: Section headers

// Success outputs a success message in Green with a newline.
func Success(message string) {
	common.Stdout("%s%s%s\n", Green, message, Reset)
}

// WarnMessage outputs a warning message in Yellow with a newline.
// Note: This is a simpler alternative to the Warning() function in functions.go
// which uses common.Log() and supports format strings with variadic arguments.
func WarnMessage(message string) {
	common.Stdout("%s%s%s\n", Yellow, message, Reset)
}

// Error outputs an error message in Red with a newline.
func Error(message string) {
	common.Stdout("%s%s%s\n", Red, message, Reset)
}

// Header outputs a header text in Bold with a newline.
func Header(text string) {
	common.Stdout("%s%s%s\n", Bold, text, Reset)
}
