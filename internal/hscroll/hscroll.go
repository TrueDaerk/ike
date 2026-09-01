// Package hscroll holds the shared horizontal-scroll indicator used by every
// pane that scrolls sideways (#2377): the editor, the diff viewer, the
// explorer tree and the playground's result buffer. Horizontal scrolling used
// to be invisible — a shifted view looked exactly like an unshifted one with
// different text — so each of those views now marks the edges of its window
// wherever content is cut off.
//
// The marks are overlays, never insertions: they take the cell that is already
// on the edge instead of adding a column, so cursor positions, mouse hit zones
// and column reporting are untouched by them. That is the same contract the
// vertical scrollbar honours when it claims a pane's rightmost column.
package hscroll

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
)

// LeftGlyph marks a window whose content continues to the left (the view is
// scrolled), RightGlyph one whose content continues to the right. Both are one
// display cell wide so they can replace a single content cell.
const (
	LeftGlyph  = "‹"
	RightGlyph = "›"
)

// Cut reports which edges of a horizontal window hide content: left when the
// window starts past column 0, right when the row reaches past its end. All
// three arguments are display columns — the unit the callers' own scroll math
// already works in, never rune counts — so tabs, double-width runes and
// conceal stand-ins are accounted for by whoever measured the row.
func Cut(offset, width, rowWidth int) (left, right bool) {
	return offset > 0, width > 0 && rowWidth > offset+width
}

// Stamp overlays the edge marks on one rendered row occupying width display
// cells. The row keeps its width: the marks replace the edge cells rather than
// shifting them. A row shorter than width is padded only when the right mark
// needs the cell, so short lines stay free of trailing blanks.
func Stamp(row string, width int, left, right bool, st lipgloss.Style) string {
	if width <= 0 || (!left && !right) {
		return row
	}
	if left {
		row = st.Render(LeftGlyph) + ansi.Cut(row, 1, width)
	}
	if right {
		head := ansi.Cut(row, 0, width-1)
		if pad := width - 1 - ansi.StringWidth(head); pad > 0 {
			head += strings.Repeat(" ", pad)
		}
		row = head + st.Render(RightGlyph)
	}
	return row
}
