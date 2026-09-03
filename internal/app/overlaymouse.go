package app

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// overlaymouse.go holds the hit test the centered floating overlays share
// (#2463): every one of them dismisses on a click outside, routes a left press
// to its own Click in overlay-local coordinates, scrolls on the wheel when it
// has a list, and swallows everything else so no mouse event leaks to the
// panes below.

// centeredOverlay is what handleMouse needs of a centered overlay. The wheel
// is not part of it: an overlay that scrolls says so with a Wheel(int) method,
// which overlayMouse picks up, and one without simply ignores the wheel.
type centeredOverlay interface {
	IsOpen() bool
	View() string
	Close()
	Click(x, y int) tea.Cmd
}

// overlayMouse hit-tests one open overlay and reports whether it consumed the
// event. A closed overlay consumes nothing, so callers chain the overlays in
// render order, topmost first.
func (m Model) overlayMouse(msg mouseEvent, ov centeredOverlay) (tea.Cmd, bool) {
	if !ov.IsOpen() {
		return nil, false
	}
	v := ov.View()
	if clickOutside(msg, v, m.width, m.height) {
		ov.Close()
		return nil, true
	}
	switch {
	case msg.action == mousePress && msg.Button == tea.MouseLeft:
		bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
		return ov.Click(msg.X-bx, msg.Y-by), true
	case msg.action == mouseWheel && msg.Button == tea.MouseWheelUp:
		if w, ok := ov.(interface{ Wheel(delta int) }); ok {
			w.Wheel(-wheelLines * msg.ticks())
		}
	case msg.action == mouseWheel && msg.Button == tea.MouseWheelDown:
		if w, ok := ov.(interface{ Wheel(delta int) }); ok {
			w.Wheel(wheelLines * msg.ticks())
		}
	}
	return nil, true
}
