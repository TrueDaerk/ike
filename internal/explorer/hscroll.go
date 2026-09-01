package explorer

// hscroll.go — the tree's half of the shared horizontal-scroll indicator
// (#2377). The explorer already had the coarse half of the answer: a
// horizontal scrollbar row at the pane's bottom edge whenever the content
// overflows. What it lacked was the per-row half — which of the rows in front
// of you are cut off, and on which side — so a deep subtree scrolled sideways
// read as a list of truncated names with no cue about the truncation. The
// marks answer that on each row, in the same glyphs the editor and the diff
// viewer use.

import (
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// hMarkStyle paints the marks in the scrollbar thumb's tone: the slot that
// already means "scroll state" in this pane, so the marks re-theme with the
// bars instead of carrying a colour of their own.
func (m Model) hMarkStyle() lipgloss.Style {
	pal := m.theme()
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	return lipgloss.NewStyle().Foreground(pal.ScrollbarThumb)
}
