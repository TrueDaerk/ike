package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// TestEditorShiftClickExtendsSelection guards #2608: the Shift modifier of a
// left press reaches the editor's click path — shift+click extends (here:
// starts) a selection, while the next plain click drops it again.
func TestEditorShiftClickExtendsSelection(t *testing.T) {
	m, r, key := editorDragModel(t)
	x := r.X + paneContentX
	y := r.Y + paneContentY
	m = step(m, tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	ed := m.activeWS().Panes.Get(key).Editor()
	if got := ed.ModeName(); got != editor.Normal {
		t.Fatalf("mode after plain click=%v want Normal", got)
	}
	m = step(m, tea.MouseClickMsg{X: x + 6, Y: y, Button: tea.MouseLeft, Mod: tea.ModShift})
	if got := ed.ModeName(); got != editor.Visual {
		t.Fatalf("mode after shift+click=%v want Visual", got)
	}
	// The press still arms the selection drag, so dragging on keeps working.
	if m.drag == nil || m.drag.kind != dragEditSelect {
		t.Fatal("shift+click must still arm the edit-select drag")
	}
	m = step(m, tea.MouseReleaseMsg{X: x + 6, Y: y, Button: tea.MouseLeft})
	m = step(m, tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})
	if got := ed.ModeName(); got != editor.Normal {
		t.Fatalf("mode after the plain click=%v want Normal", got)
	}
}
