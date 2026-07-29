package httppane

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/scrollbar"
)

// scrollbar.go — the response viewer's vertical scrollbar (#1367): the shared
// track/thumb visual language from the editor (#1022) and explorer (#1036),
// overlaying the pane's rightmost column whenever the composed view has more
// display rows than the body viewport. Mouse: a left press on the thumb
// starts a drag (routed by the app as dragHTTPScroll); a press on the track
// jumps the viewport proportionally. Folding (#1330) composes for free: the
// bar maps the display projection (m.visible), so collapsed rows are not part
// of the track's world.
//
// Coordinates: the pane renders a title row above the body, so the
// content-local y the app hands in is 1-based relative to the track; the
// methods translate.

// scrollbarGeometry resolves the bar's layout: the track length in rows, the
// total display-row count it maps, and the thumb's start/length. ok is false
// when no scrollbar should render (unsized pane or no overflow).
func (m *Model) scrollbarGeometry() (track, total, thumbStart, thumbLen int, ok bool) {
	track = m.bodyHeight()
	total = len(m.visible)
	if m.height <= 0 || track <= 0 || total <= track || m.width < 2 {
		return 0, 0, 0, 0, false
	}
	thumbStart, thumbLen = scrollbar.Thumb(track, total, track, m.top)
	return track, total, thumbStart, thumbLen, true
}

// ScrollbarHit reports whether a content-local press lands on the scrollbar:
// the pane's rightmost column while the bar is visible, within the track. The
// app checks this before the selection press so the bar outranks text there.
func (m *Model) ScrollbarHit(x, y int) bool {
	track, _, _, _, ok := m.scrollbarGeometry()
	return ok && x == m.width-1 && y >= 1 && y < 1+track
}

// ScrollbarPress handles a left press at content-local row y. On the thumb it
// records the grab offset and returns true — the app then tracks a
// dragHTTPScroll gesture feeding ScrollbarDrag. On the track above/below the
// thumb it jumps the viewport to the proportional position and returns false.
func (m *Model) ScrollbarPress(y int) (drag bool) {
	track, total, start, length, ok := m.scrollbarGeometry()
	if !ok {
		return false
	}
	y-- // title row above the track
	if y >= start && y < start+length {
		m.sbGrab = y - start
		return true
	}
	m.top = scrollbar.Jump(y, track, total, track, m.top)
	return false
}

// ScrollbarDrag continues a thumb drag: the thumb's top follows the pointer
// minus the recorded grab offset, mapped back to a scroll offset.
func (m *Model) ScrollbarDrag(y int) {
	track, total, _, _, ok := m.scrollbarGeometry()
	if !ok {
		return
	}
	m.top = scrollbar.Drag(y-1, m.sbGrab, track, total, track, m.top)
}

// overlayScrollbar draws the bar over the rightmost cell of the rendered body
// rows: each affected row is clipped/padded to the pane width minus one, then
// the track or thumb cell is appended.
func (m *Model) overlayScrollbar(rows []string) []string {
	track, _, start, length, ok := m.scrollbarGeometry()
	if !ok {
		return rows
	}
	pal := m.theme()
	trackCell := lipgloss.NewStyle().Foreground(pal.ScrollbarTrack).Render("│")
	thumbCell := lipgloss.NewStyle().Background(pal.ScrollbarThumb).Render(" ")
	w := m.width
	for y := 0; y < len(rows) && y < track; y++ {
		cell := trackCell
		if y >= start && y < start+length {
			cell = thumbCell
		}
		row := ansi.Truncate(rows[y], w-1, "")
		if pad := w - 1 - ansi.StringWidth(row); pad > 0 {
			row += strings.Repeat(" ", pad)
		}
		rows[y] = row + cell
	}
	return rows
}
