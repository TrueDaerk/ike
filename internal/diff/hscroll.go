package diff

// hscroll.go — the diff viewer's half of the shared horizontal-scroll
// indicator (#2377). The viewer never wraps: every row is one visual line
// clipped at its column edge (#1700), so a scrolled diff used to be
// indistinguishable from an unscrolled one showing different text. Each
// rendered segment now marks its own edges — "‹" where the row continues left,
// "›" where it continues right — so both columns of a side-by-side diff answer
// the question independently, on the row the eye is already on.
//
// The marks replace edge cells instead of inserting columns, so the click-to-
// column mapping in selection.go (x - gutter + hoff) is untouched.

import (
	"charm.land/lipgloss/v2"

	"ike/internal/hscroll"
	"ike/internal/theme"
)

// SetHScrollMarks toggles the edge marks (ui.h_scroll_marks) and re-renders;
// the pane registry applies it from the config on every (re)configure.
func (m *Model) SetHScrollMarks(on bool) {
	if m.hMarks == on {
		return
	}
	m.hMarks = on
	m.render()
}

// stampHScroll overlays the edge marks on one rendered segment. runes is the
// tab-expanded row the segment was cut from, so its length is the row's width
// in display columns — the same unit hoff and width speak. A gap segment (the
// empty counterpart of a one-sided row) carries no content and stays blank.
func (m Model) stampHScroll(seg string, runes []rune, gap bool, width int) string {
	if !m.hMarks || gap {
		return seg
	}
	left, right := hscroll.Cut(m.hoff, width, len(runes))
	pal := m.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	return hscroll.Stamp(seg, width, left, right,
		lipgloss.NewStyle().Foreground(pal.ScrollbarThumb))
}
