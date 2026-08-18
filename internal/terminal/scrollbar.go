package terminal

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/scrollbar"
	"ike/internal/theme"
)

// scrollbar.go — the terminal pane's scrollback scrollbar (#1368): the shared
// track/thumb visual language (internal/scrollbar, #1367) on the pane's
// rightmost column, mapping the virtual buffer [scrollback ++ screen] so the
// thumb shows where the view sits in the history. The bar appears while
// scrolled back, and on the live view once scrollback exists — but never over
// a mouse-reporting or alt-screen child at the live view: their UI owns the
// grid (and their mouse events pass through untouched; ScrollbarHit is false,
// so the app forwards clicks as before). Mouse: a thumb press starts a drag
// (routed by the app as dragTermScroll); a track press jumps proportionally.
// The wheel keeps its existing routing (#226/#669) and is unaffected.

// scrollbarGeometry resolves the bar's layout: the track length in rows, the
// virtual line count it maps, and the thumb's start/length. ok is false when
// no scrollbar should render.
func (m Model) scrollbarGeometry() (track, total, thumbStart, thumbLen int, ok bool) {
	if m.sess == nil || m.h <= 0 || m.w < 2 {
		return 0, 0, 0, 0, false
	}
	sb := m.sess.ScrollbackLen()
	if sb <= 0 {
		return 0, 0, 0, 0, false
	}
	// A finished session has no child UI to protect (#1951): its scrollbar
	// shows even when the exited program left the alt screen behind.
	if m.scroll == 0 && !m.dead() && (m.sess.WantsMouse() || m.sess.AltScreen()) {
		return 0, 0, 0, 0, false
	}
	track = m.h
	total = sb + m.h
	thumbStart, thumbLen = scrollbar.Thumb(track, total, m.h, sb-m.scroll)
	return track, total, thumbStart, thumbLen, true
}

// ScrollbarHit reports whether a pane-local press lands on the scrollbar: the
// rightmost column while the bar is visible. The app checks this before the
// press routing, so the bar outranks selection anchoring there — and a
// mouse-reporting child keeps every click, since the bar hides for it.
func (m Model) ScrollbarHit(x, y int) bool {
	track, _, _, _, ok := m.scrollbarGeometry()
	return ok && x == m.w-1 && y >= 0 && y < track
}

// ScrollbarPress handles a left press at track row y. On the thumb it records
// the grab offset and returns true — the app then tracks a dragTermScroll
// gesture feeding ScrollbarDrag. On the track above/below the thumb it jumps
// the view to the proportional position in the scrollback and returns false.
func (m *Model) ScrollbarPress(y int) (drag bool) {
	track, total, start, length, ok := m.scrollbarGeometry()
	if !ok {
		return false
	}
	if y >= start && y < start+length {
		m.sbGrab = y - start
		return true
	}
	sb := m.sess.ScrollbackLen()
	m.scroll = sb - scrollbar.Jump(y, track, total, m.h, sb-m.scroll)
	return false
}

// ScrollbarDrag continues a thumb drag: the thumb's top follows the pointer
// minus the recorded grab offset, mapped back to a scrollback offset.
func (m *Model) ScrollbarDrag(y int) {
	track, total, _, _, ok := m.scrollbarGeometry()
	if !ok {
		return
	}
	sb := m.sess.ScrollbackLen()
	m.scroll = sb - scrollbar.Drag(y, m.sbGrab, track, total, m.h, sb-m.scroll)
}

// overlayScrollbar draws the bar in the pane's rightmost column — the
// reserved gutter (#1500): the child grid is one column narrower (gridW), so
// the bar never covers content. Each affected row is clipped/padded to the
// grid width, then the track or thumb cell is appended; rows are padded up to
// the track so the bar reads full-height even over a short render.
func (m Model) overlayScrollbar(view string) string {
	track, _, start, length, ok := m.scrollbarGeometry()
	if !ok {
		return view
	}
	var pal *theme.Palette
	if m.pal != nil {
		pal = m.pal
	} else {
		pal = theme.DefaultPalette()
	}
	trackCell := lipgloss.NewStyle().Foreground(pal.ScrollbarTrack).Render("│")
	thumbCell := lipgloss.NewStyle().Background(pal.ScrollbarThumb).Render(" ")
	rows := strings.Split(view, "\n")
	for len(rows) < track {
		rows = append(rows, "")
	}
	for y := 0; y < track; y++ {
		cell := trackCell
		if y >= start && y < start+length {
			cell = thumbCell
		}
		row := ansi.Truncate(rows[y], m.w-1, "")
		if pad := m.w - 1 - ansi.StringWidth(row); pad > 0 {
			row += strings.Repeat(" ", pad)
		}
		rows[y] = row + cell
	}
	return strings.Join(rows, "\n")
}
