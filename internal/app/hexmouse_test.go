package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
)

// TestHexClickMovesCursor: a left click on a byte of an unfocused hex pane
// focuses it and moves the byte cursor there, activating the clicked column.
func TestHexClickMovesCursor(t *testing.T) {
	m, _ := hexEditOpen(t, false)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst.Kind() != pane.KindHex {
		t.Fatalf("focused pane is %v, want a hex viewer", inst.Kind())
	}
	hv := inst.Hex()
	r, ok := m.lay.Panes[key]
	if !ok {
		t.Fatalf("no layout rect for %q", key)
	}
	x, y := r.X+paneContentX, r.Y+m.contentYOff(key)
	m.setFocus("explorer")

	// offset gutter (8 digits) + 2 spaces, then "xx " per byte.
	m = step(m, tea.MouseClickMsg{X: x + 10 + 3*2, Y: y, Button: tea.MouseLeft})
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("the click did not focus the hex pane: %q", m.activeWS().Panes.Focused())
	}
	if hv.Cursor() != 2 || hv.ColumnLabel() != "HEX" {
		t.Fatalf("hex click: cursor %d column %s, want 2 HEX", hv.Cursor(), hv.ColumnLabel())
	}

	per := hv.PerRow()
	m = step(m, tea.MouseClickMsg{X: x + 10 + 3*per + per/8 + 5, Y: y, Button: tea.MouseLeft})
	if hv.Cursor() != 5 || hv.ColumnLabel() != "TEXT" {
		t.Fatalf("text click: cursor %d column %s, want 5 TEXT", hv.Cursor(), hv.ColumnLabel())
	}
}
