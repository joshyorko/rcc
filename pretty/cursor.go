package pretty

import (
	"os"

	"github.com/joshyorko/rcc/common"
	"golang.org/x/term"
)

// Cursor control functions using CSI (Control Sequence Introducer) escape sequences.
// All functions check Interactive flag before outputting escape sequences.
// Output is directed to stdout for consistency with other terminal control operations.

// SaveCursor saves the current cursor position (CSI s or ESC 7)
func SaveCursor() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("s"))
}

// RestoreCursor restores the previously saved cursor position (CSI u or ESC 8)
func RestoreCursor() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("u"))
}

// MoveTo moves cursor to specified row and column (CSI {row};{col}H)
// Row and column are 1-indexed (top-left is 1,1)
func MoveTo(row, col int) {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("%d;%dH", row, col))
}

// MoveUp moves cursor up by n lines (CSI {n}A)
func MoveUp(n int) {
	if !Interactive {
		return
	}
	if n <= 0 {
		return
	}
	common.Stdout("%s", csif("%dA", n))
}

// MoveDown moves cursor down by n lines (CSI {n}B)
func MoveDown(n int) {
	if !Interactive {
		return
	}
	if n <= 0 {
		return
	}
	common.Stdout("%s", csif("%dB", n))
}

// MoveRight moves cursor right by n columns (CSI {n}C)
func MoveRight(n int) {
	if !Interactive {
		return
	}
	if n <= 0 {
		return
	}
	common.Stdout("%s", csif("%dC", n))
}

// MoveLeft moves cursor left by n columns (CSI {n}D)
func MoveLeft(n int) {
	if !Interactive {
		return
	}
	if n <= 0 {
		return
	}
	common.Stdout("%s", csif("%dD", n))
}

// SetScrollRegion sets the scrolling region to lines [top, bottom] (CSI {top};{bottom}r - DECSTBM)
// Lines outside this region won't scroll. Both parameters are 1-indexed.
func SetScrollRegion(top, bottom int) {
	if !Interactive {
		return
	}
	if top <= 0 || bottom <= 0 || top >= bottom {
		return
	}
	common.Stdout("%s", csif("%d;%dr", top, bottom))
}

// ClearScrollRegion resets scroll region to full screen (CSI r)
func ClearScrollRegion() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("r"))
}

// ScrollUp scrolls the scroll region up by n lines (CSI {n}S)
// New lines appear at the bottom
func ScrollUp(n int) {
	if !Interactive {
		return
	}
	if n <= 0 {
		return
	}
	common.Stdout("%s", csif("%dS", n))
}

// ScrollDown scrolls the scroll region down by n lines (CSI {n}T)
// New lines appear at the top
func ScrollDown(n int) {
	if !Interactive {
		return
	}
	if n <= 0 {
		return
	}
	common.Stdout("%s", csif("%dT", n))
}

// ClearLine clears the entire current line (CSI 2K)
// Cursor position is not changed
func ClearLine() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("2K"))
}

// ClearToEnd clears from cursor to end of line (CSI 0K)
func ClearToEnd() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("0K"))
}

// ClearToStart clears from cursor to start of line (CSI 1K)
func ClearToStart() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("1K"))
}

// ClearScreen clears the entire screen (CSI 2J)
// Cursor position is not changed
func ClearScreen() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("2J"))
}

// HideCursor makes the cursor invisible (CSI ?25l)
func HideCursor() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("?25l"))
}

// ShowCursor makes the cursor visible (CSI ?25h)
func ShowCursor() {
	if !Interactive {
		return
	}
	common.Stdout("%s", csif("?25h"))
}

// TerminalHeight returns the terminal height in rows
// Uses golang.org/x/term.GetSize() with fallback to 24 rows if detection fails
func TerminalHeight() int {
	_, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || height <= 0 {
		common.Trace("Failed to get terminal height, using fallback: %v", err)
		return 24
	}
	common.Trace("Terminal height detected: %d", height)
	return height
}

// TerminalWidth returns the terminal width in columns
// Uses golang.org/x/term.GetSize() with fallback to 80 columns if detection fails
func TerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		common.Trace("Failed to get terminal width, using fallback: %v", err)
		return 80
	}
	common.Trace("Terminal width detected: %d", width)
	return width
}
