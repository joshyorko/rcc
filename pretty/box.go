package pretty

import (
	"os"
	"strings"

	"github.com/joshyorko/rcc/common"
)

// BoxStyle defines the characters used for drawing boxes with various line styles.
// Supports single-line, double-line, rounded corners, and ASCII fallback styles.
type BoxStyle struct {
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	Horizontal  string
	Vertical    string
	LeftT       string // Left T-junction (├)
	RightT      string // Right T-junction (┤)
	TopT        string // Top T-junction (┬)
	BottomT     string // Bottom T-junction (┴)
	Cross       string // Cross junction (┼)
}

var (
	// BoxSingle uses single-line box drawing characters (Unicode)
	BoxSingle = BoxStyle{
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "└",
		BottomRight: "┘",
		Horizontal:  "─",
		Vertical:    "│",
		LeftT:       "├",
		RightT:      "┤",
		TopT:        "┬",
		BottomT:     "┴",
		Cross:       "┼",
	}

	// BoxDouble uses double-line box drawing characters (Unicode)
	BoxDouble = BoxStyle{
		TopLeft:     "╔",
		TopRight:    "╗",
		BottomLeft:  "╚",
		BottomRight: "╝",
		Horizontal:  "═",
		Vertical:    "║",
		LeftT:       "╠",
		RightT:      "╣",
		TopT:        "╦",
		BottomT:     "╩",
		Cross:       "╬",
	}

	// BoxRounded uses rounded corner box drawing characters (Unicode)
	BoxRounded = BoxStyle{
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "╰",
		BottomRight: "╯",
		Horizontal:  "─",
		Vertical:    "│",
		LeftT:       "├",
		RightT:      "┤",
		TopT:        "┬",
		BottomT:     "┴",
		Cross:       "┼",
	}

	// BoxASCII uses ASCII characters for maximum compatibility
	BoxASCII = BoxStyle{
		TopLeft:     "+",
		TopRight:    "+",
		BottomLeft:  "+",
		BottomRight: "+",
		Horizontal:  "-",
		Vertical:    "|",
		LeftT:       "+",
		RightT:      "+",
		TopT:        "+",
		BottomT:     "+",
		Cross:       "+",
	}
)

// ActiveBoxStyle returns the appropriate BoxStyle based on terminal capabilities.
// Returns BoxRounded (modern look) when Iconic is true (Unicode supported).
// Returns BoxASCII when TERM is "dumb" or Unicode is not supported.
func ActiveBoxStyle() BoxStyle {
	// Check TERM environment variable first
	term := os.Getenv("TERM")
	if term == "dumb" || term == "" {
		return BoxASCII
	}

	// Use rounded corners for modern look when Unicode is supported
	if Iconic {
		return BoxRounded
	}

	// Default to ASCII for compatibility
	return BoxASCII
}

// DrawHLine draws a horizontal line starting at position (x, y) with the specified width.
// x is the column (1-indexed), y is the row (1-indexed), width is the number of characters.
// Uses the provided character for drawing the line.
func DrawHLine(x, y, width int, char string) {
	if !Interactive || width <= 0 {
		return
	}

	MoveTo(y, x)
	line := strings.Repeat(char, width)
	common.Stdout("%s", line)
}

// DrawVLine draws a vertical line starting at position (x, y) with the specified height.
// x is the column (1-indexed), y is the row (1-indexed), height is the number of rows.
// Uses the provided character for drawing the line.
func DrawVLine(x, y, height int, char string) {
	if !Interactive || height <= 0 {
		return
	}

	for i := 0; i < height; i++ {
		MoveTo(y+i, x)
		common.Stdout("%s", char)
	}
}

// DrawBox draws a complete bordered box at the specified position and size.
// x is the column (1-indexed), y is the row (1-indexed).
// width and height are the outer dimensions including the border.
func DrawBox(x, y, width, height int, style BoxStyle) {
	if !Interactive || width < 2 || height < 2 {
		return
	}

	// Draw top border
	MoveTo(y, x)
	common.Stdout("%s%s%s",
		style.TopLeft,
		strings.Repeat(style.Horizontal, width-2),
		style.TopRight)

	// Draw side borders
	for i := 1; i < height-1; i++ {
		MoveTo(y+i, x)
		common.Stdout("%s", style.Vertical)
		MoveTo(y+i, x+width-1)
		common.Stdout("%s", style.Vertical)
	}

	// Draw bottom border
	MoveTo(y+height-1, x)
	common.Stdout("%s%s%s",
		style.BottomLeft,
		strings.Repeat(style.Horizontal, width-2),
		style.BottomRight)
}

// DrawBoxWithTitle draws a box with a centered title in the top border.
// The title is surrounded by spaces and integrated into the top border line.
// If the title is too long for the box width, it will be truncated.
func DrawBoxWithTitle(x, y, width, height int, title string, style BoxStyle) {
	if !Interactive || width < 2 || height < 2 {
		return
	}

	// Calculate available space for title (exclude corners and padding)
	availableWidth := width - 4 // 2 corners + 2 spaces padding
	if availableWidth < 1 {
		// Box too narrow for title, draw normal box
		DrawBox(x, y, width, height, style)
		return
	}

	// Truncate title if necessary
	displayTitle := title
	if len(title) > availableWidth {
		displayTitle = title[:availableWidth]
	}

	// Calculate padding for centering
	titleLen := len(displayTitle)
	leftPad := (availableWidth - titleLen) / 2
	rightPad := availableWidth - titleLen - leftPad

	// Draw top border with title
	MoveTo(y, x)
	common.Stdout("%s%s %s %s%s",
		style.TopLeft,
		strings.Repeat(style.Horizontal, leftPad),
		displayTitle,
		strings.Repeat(style.Horizontal, rightPad),
		style.TopRight)

	// Draw side borders
	for i := 1; i < height-1; i++ {
		MoveTo(y+i, x)
		common.Stdout("%s", style.Vertical)
		MoveTo(y+i, x+width-1)
		common.Stdout("%s", style.Vertical)
	}

	// Draw bottom border
	MoveTo(y+height-1, x)
	common.Stdout("%s%s%s",
		style.BottomLeft,
		strings.Repeat(style.Horizontal, width-2),
		style.BottomRight)
}
