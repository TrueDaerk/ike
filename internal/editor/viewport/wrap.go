package viewport

import "strings"

// wrap.go is the visual-row mapping layer for soft wrap (#64): pure functions
// that split one buffer line into screen rows by display-cell budget, plus the
// wrapped variant of cursor-follow scrolling. Like the rest of the package it
// holds no buffer — the editor passes rune slices and row counts in.

// WrapSegments splits a line's runes into visual rows of at most width display
// cells and returns the start column of each row. Tabs budget tabWidth cells; a
// tab that would straddle the right edge starts the next row (unless it sits at
// a row start, where it renders clamped like the unwrapped renderer). The
// result always holds at least one segment, [0], so an empty line is one row.
func WrapSegments(runes []rune, width, tabWidth int) []int {
	if width < 1 {
		width = 1
	}
	segs := []int{0}
	cells := 0
	for col, r := range runes {
		w := 1
		if r == '\t' {
			w = tabWidth
		}
		if cells+w > width && cells > 0 {
			segs = append(segs, col)
			cells = 0
		}
		cells += w
	}
	return segs
}

// SegmentIndex returns the index of the segment containing col. A col at or
// past the line end (the insert-mode end-of-line cursor) maps to the last
// segment.
func SegmentIndex(segs []int, col int) int {
	for i := len(segs) - 1; i > 0; i-- {
		if col >= segs[i] {
			return i
		}
	}
	return 0
}

// SegmentEnd returns one past the last column of segment i: the next segment's
// start, or lineLen for the final segment.
func SegmentEnd(segs []int, i, lineLen int) int {
	if i+1 < len(segs) {
		return segs[i+1]
	}
	return lineLen
}

// ScrollWrapped is the soft-wrap variant of Scroll: it adjusts Top so the
// cursor's visual row stays within the window, honouring ScrollOff in visual
// rows. rows reports how many screen rows a buffer line occupies (0 for a line
// hidden inside a collapsed fold, 1 for a fold header). Horizontal scroll is
// meaningless under wrap, so Left resets to 0. A single line taller than the
// window pins Top on that line (its overflow rows stay off screen, like vim's
// "@@@" lines).
func (v *Viewport) ScrollWrapped(cursorLine, cursorSeg, lineCount int, rows func(int) int) {
	v.Left = 0
	if v.height <= 0 {
		return
	}
	off := v.ScrollOff
	if max := (v.height - 1) / 2; off > max {
		off = max
	}
	if v.Top > cursorLine {
		v.Top = cursorLine
	}
	if v.Top > lineCount-1 {
		v.Top = lineCount - 1
	}
	if v.Top < 0 {
		v.Top = 0
	}
	// The cursor's visual row relative to Top.
	vr := cursorSeg
	for l := v.Top; l < cursorLine; l++ {
		vr += rows(l)
	}
	// Pull Top up while the scrolloff margin above the cursor is short.
	for vr < off && v.Top > 0 {
		v.Top--
		vr += rows(v.Top)
	}
	// Push Top down while the cursor row hangs below the window.
	for vr > v.height-1-off && v.Top < cursorLine {
		vr -= rows(v.Top)
		v.Top++
	}
}

// GutterContinuation renders the gutter cell for a wrap-continuation row: blank
// where the line number would sit, with a wrap indicator before the separator.
func (v *Viewport) GutterContinuation(lineCount int) string {
	w := v.GutterWidth(lineCount)
	if w == 0 {
		return ""
	}
	if w < 2 {
		return strings.Repeat(" ", w)
	}
	// rightPad counts bytes, and "↪" is multi-byte; pad by display width here.
	return strings.Repeat(" ", w-2) + "↪ "
}
