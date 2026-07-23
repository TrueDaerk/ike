package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/layout"
	"ike/internal/pane"
)

// editorDragModel is a sized model with one open file and the editor pane's rect.
func editorDragModel(t *testing.T) (Model, layout.Rect, string) {
	t.Helper()
	m := sized(t, 100, 40)
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("hello world\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	for key, rect := range m.lay.Panes {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			return m, rect, key
		}
	}
	t.Fatal("setup: no editor pane rect")
	return m, layout.Rect{}, ""
}

// TestEditorDragSelects guards #977: press-drag in an editor pane routes
// motion events to the editor and produces a charwise visual selection.
func TestEditorDragSelects(t *testing.T) {
	m, r, key := editorDragModel(t)
	x := r.X + paneContentX
	y := r.Y + paneContentY
	m = step(m, tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
	if m.drag == nil || m.drag.kind != dragEditSelect {
		t.Fatal("editor press must start an edit-select drag")
	}
	m = step(m, tea.MouseMotionMsg{X: x + 6, Y: y + 1, Button: tea.MouseLeft})
	ed := m.activeWS().Panes.Get(key).Editor()
	if got := ed.ModeName(); got != editor.Visual {
		t.Fatalf("mode=%v want Visual", got)
	}
	m = step(m, tea.MouseReleaseMsg{X: x + 6, Y: y + 1, Button: tea.MouseLeft})
	if m.drag != nil {
		t.Fatal("release must end the drag")
	}
	if got := ed.ModeName(); got != editor.Visual {
		t.Fatalf("selection must survive the release (mode=%v)", got)
	}
}
