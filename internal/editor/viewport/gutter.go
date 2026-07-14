package viewport

import (
	"strconv"
	"strings"
)

// Gutter renders the line-number cell for line (0-based) given the cursor line
// and total line count. It returns "" when line numbers are disabled. With
// relative numbering the cursor line shows its absolute number left-aligned (vim
// "number relativenumber"); other lines show the distance to the cursor,
// right-aligned.
func (v *Viewport) Gutter(line, cursorLine, lineCount int) string {
	w := v.GutterWidth(lineCount)
	if w == 0 {
		return ""
	}
	num := w - 2 // reserve the leading sign column and trailing separator
	var s string
	switch {
	case v.RelativeNumbers && line == cursorLine:
		s = leftPad(strconv.Itoa(line+1), num)
	case v.RelativeNumbers:
		s = rightPad(strconv.Itoa(abs(line-cursorLine)), num)
	default:
		s = rightPad(strconv.Itoa(line+1), num)
	}
	// Leading blank is the sign column; editors overwrite it with a breakpoint
	// or debug-line glyph. Trailing blank separates the gutter from the text.
	return " " + s + " "
}

// leftPad pads s on the right to width w (left-aligned content).
func leftPad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// rightPad pads s on the left to width w (right-aligned content).
func rightPad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
