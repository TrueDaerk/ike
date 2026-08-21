package app

import (
	"fmt"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/palette"
)

// palettewheel_test.go covers the mouse-wheel routing into the open palette
// (#2041): before it the overlay only saw presses, so no palette list — least
// of all the recent dialog's projects column — could be scrolled by wheel.

// TestPaletteWheelScrollsTheOpenPalette feeds a wheel burst over the palette
// box and expects its rendered list to scroll; a wheel outside the box leaves
// it untouched.
func TestPaletteWheelScrollsTheOpenPalette(t *testing.T) {
	m := sized(t, 120, 40)
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		writeTemp(t, dir, fmt.Sprintf("file%02d.txt", i), "x")
	}
	// The ';' open-path picker over a well-filled directory is the simplest
	// palette list long enough to scroll.
	m.palette.OpenLockedWith(palette.Context{Root: dir}, palette.OpenPathPrefix, dir+string(filepath.Separator))
	if !m.palette.IsOpen() {
		t.Fatal("setup: the palette must be open")
	}
	before := m.palette.View()
	w, h := lipgloss.Width(before), lipgloss.Height(before)
	bx, by := (m.width-w)/2, (m.height-h)/2
	cx, cy := bx+w/2, by+h/2

	m, _ = raw(t, m, tea.MouseWheelMsg{X: cx, Y: cy, Button: tea.MouseWheelDown})
	m, _ = raw(t, m, wheelFlushMsg{})
	scrolled := m.palette.View()
	if scrolled == before {
		t.Fatalf("a wheel over the palette must scroll its list:\n%s", scrolled)
	}

	// Outside the box the wheel is not the palette's business.
	m, _ = raw(t, m, tea.MouseWheelMsg{X: 0, Y: 0, Button: tea.MouseWheelDown})
	m, _ = raw(t, m, wheelFlushMsg{})
	if got := m.palette.View(); got != scrolled {
		t.Fatalf("a wheel outside the box must not move the palette:\n%s", got)
	}
}
