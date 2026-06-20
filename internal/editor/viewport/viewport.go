// Package viewport owns scroll state and gutter sizing: the first visible line
// and column, the scrolloff margin, and the line-number gutter. It maps the
// cursor into the visible window (scrolling vertically and horizontally to keep
// it on screen) and renders gutter cells from the [editor] line-number settings.
// It holds no buffer; the editor passes line counts and the cursor in.
package viewport

import "strconv"

// Viewport is the scroll + gutter state for one editor pane.
type Viewport struct {
	Top  int // first visible line (0-based)
	Left int // first visible column (0-based, for horizontal scroll)

	width  int // total content width of the pane (gutter + text)
	height int // visible text rows

	ScrollOff       int  // keep this many lines above/below the cursor
	LineNumbers     bool // show the gutter
	RelativeNumbers bool // relative numbering (current line shows absolute)
}

// SetSize sets the pane content dimensions.
func (v *Viewport) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// Height returns the number of visible text rows.
func (v *Viewport) Height() int { return v.height }

// GutterWidth returns the width reserved for the line-number gutter given the
// total line count, or 0 when line numbers are disabled. It is the digit count
// of the largest line number plus a one-cell separator.
func (v *Viewport) GutterWidth(lineCount int) int {
	if !v.LineNumbers {
		return 0
	}
	digits := len(strconv.Itoa(maxInt(lineCount, 1)))
	if digits < 3 {
		digits = 3 // keep a stable minimum so the text edge doesn't jitter
	}
	return digits + 1
}

// TextWidth returns the columns available for buffer text after the gutter.
func (v *Viewport) TextWidth(lineCount int) int {
	w := v.width - v.GutterWidth(lineCount)
	if w < 1 {
		return 1
	}
	return w
}

// Scroll adjusts Top and Left so the cursor stays visible, honouring ScrollOff
// vertically and keeping the cursor column within the text width horizontally.
func (v *Viewport) Scroll(cursorLine, cursorCol, lineCount int) {
	if v.height <= 0 {
		return
	}
	off := v.ScrollOff
	// A scrolloff larger than half the window would pin the cursor; clamp it.
	if max := (v.height - 1) / 2; off > max {
		off = max
	}
	if cursorLine-off < v.Top {
		v.Top = cursorLine - off
	}
	if cursorLine+off > v.Top+v.height-1 {
		v.Top = cursorLine + off - v.height + 1
	}
	if v.Top > lineCount-1 {
		v.Top = lineCount - 1
	}
	if v.Top < 0 {
		v.Top = 0
	}

	tw := v.TextWidth(lineCount)
	if cursorCol < v.Left {
		v.Left = cursorCol
	}
	if cursorCol > v.Left+tw-1 {
		v.Left = cursorCol - tw + 1
	}
	if v.Left < 0 {
		v.Left = 0
	}
}

// Bottom returns one past the last visible line index, clamped to lineCount.
func (v *Viewport) Bottom(lineCount int) int {
	end := v.Top + v.height
	if v.height <= 0 || end > lineCount {
		end = lineCount
	}
	return end
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
