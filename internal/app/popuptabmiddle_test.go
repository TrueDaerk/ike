package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// popuptabmiddle_test.go: the popup terminal layer's tab bars answer to
// middle-click like the editor's does (#2259) — before it, closing a tab
// there needed its ✕ zone or the keyboard.

// popupTabX scans the popup box's title row for the first screen column that
// resolves to tab idx outside its ✕ zone.
func popupTabX(t *testing.T, m Model, idx int) (x, y int) {
	t.Helper()
	px, py, _, _ := m.popupTermRect()
	wl, _ := m.popupSplitWidths()
	for sx := px; sx < px+wl; sx++ {
		i, closeHit, ok := m.popupBoxTabAt(m.popup.inst, sx-(px+paneContentX), wl)
		if ok && i == idx && !closeHit {
			return sx, py + 1
		}
	}
	t.Fatalf("no title-row column resolves to tab %d", idx)
	return 0, 0
}

// TestPopupTabMiddleClickCloses: a middle press on a non-active tab segment
// closes that tab and leaves the rest of the layer alone.
func TestPopupTabMiddleClickCloses(t *testing.T) {
	m := openTestPopup(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper})
	m = out.(Model)
	if m.popup.inst.TabCount() != 2 {
		t.Fatalf("setup: want two popup tabs, got %d", m.popup.inst.TabCount())
	}
	if m.popup.inst.ActiveTab() != 1 {
		t.Fatalf("setup: the new tab should be active, got %d", m.popup.inst.ActiveTab())
	}
	x, y := popupTabX(t, m, 0)
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if got := m.popup.inst.TabCount(); got != 1 {
		t.Fatalf("middle click left %d tabs, want 1", got)
	}
	if !m.popup.open {
		t.Fatal("closing one tab must not close the popup")
	}
}

// TestPopupTabMiddleClickOffSegmentIsInert: a middle press on the title row
// that misses every segment neither closes a tab nor starts a move drag.
func TestPopupTabMiddleClickOffSegmentIsInert(t *testing.T) {
	m := openTestPopup(t)
	out, _ := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModSuper})
	m = out.(Model)
	px, py, pw, _ := m.popupTermRect()
	m = step(m, tea.MouseClickMsg{X: px + pw - 2, Y: py + 1, Button: tea.MouseMiddle})
	if got := m.popup.inst.TabCount(); got != 2 {
		t.Fatalf("an off-segment middle click closed a tab: %d left", got)
	}
	if m.floatMove != nil {
		t.Fatal("a middle click must not start the box move drag")
	}
}
