package pretty

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/robocorp/rcc/common"
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
	Interactive = stdin && stdout && stderr

	localSetup(Interactive)

	common.Trace("Interactive mode enabled: %v; colors enabled: %v; icons enabled: %v", Interactive, !Disabled, Iconic)
	if Interactive && !Disabled && !Colorless {
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
