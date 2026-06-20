package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/registry"
)

// sized returns a model after a window size so its layout tree and geometry are
// computed, with layout persistence redirected to a temp dir.
func sized(t *testing.T, w, h int) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return out.(Model)
}

func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}
func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
}
func release(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

func step(m Model, msg tea.Msg) Model {
	out, _ := m.Update(msg)
	return out.(Model)
}

func TestDragResizeWidensExplorer(t *testing.T) {
	m := sized(t, 100, 40)
	before := m.lay.Panes[ctxExplorer].W
	divX := m.lay.Dividers[0].Rect.X
	m = step(m, press(divX, 5))
	m = step(m, motion(60, 5))
	m = step(m, release(60, 5))
	after := m.lay.Panes[ctxExplorer].W
	if after <= before {
		t.Fatalf("explorer did not widen: before=%d after=%d", before, after)
	}
	if after < 55 || after > 61 {
		t.Fatalf("explorer width after drag to 60 = %d", after)
	}
}

func TestDragResizeClampsMinimum(t *testing.T) {
	m := sized(t, 100, 40)
	divX := m.lay.Dividers[0].Rect.X
	m = step(m, press(divX, 5))
	m = step(m, motion(0, 5)) // slam left
	m = step(m, release(0, 5))
	if w := m.lay.Panes[ctxExplorer].W; w < 4 {
		t.Fatalf("explorer collapsed below minimum: %d", w)
	}
}

func TestDragMoveSwapsPanes(t *testing.T) {
	m := sized(t, 100, 40)
	// press explorer title bar (top row), release on right half of editor.
	m = step(m, press(2, 0))
	edRect := m.lay.Panes[ctxEditor]
	m = step(m, release(edRect.X+edRect.W-2, edRect.Y+edRect.H/2))
	s, ok := m.tree.(*layout.Split)
	if !ok || s.Orient != layout.Horizontal {
		t.Fatalf("expected horizontal split, got %#v", m.tree)
	}
	if s.A.(*layout.Leaf).Pane != ctxEditor || s.B.(*layout.Leaf).Pane != ctxExplorer {
		t.Fatalf("panes not swapped: A=%+v B=%+v", s.A, s.B)
	}
}

func TestMoveDragShowsFeedback(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, press(2, 0)) // grab explorer title
	edRect := m.lay.Panes[ctxEditor]
	m = step(m, motion(edRect.X+edRect.W-2, edRect.Y+edRect.H/2)) // hover editor right half
	view := m.View()
	if !strings.Contains(view, "MOVE EXPLORER") {
		t.Fatalf("status line missing move hint:\n%s", view)
	}
	if !strings.Contains(view, "⤴") {
		t.Fatal("source pane missing move marker")
	}
	if !strings.Contains(view, "right ◨") {
		t.Fatal("drop target missing right-zone marker")
	}
}

func TestDragPersistsAndRestores(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	divX := m.lay.Dividers[0].Rect.X
	m = step(m, press(divX, 5))
	m = step(m, motion(60, 5))
	m = step(m, release(60, 5))

	if _, err := os.Stat(filepath.Join(dir, "layout.json")); err != nil {
		t.Fatalf("layout file not written: %v", err)
	}

	// A fresh model in the same state dir restores the widened layout.
	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m2 = out2.(Model)
	if got := m2.lay.Panes[ctxExplorer].W; got < 55 || got > 61 {
		t.Fatalf("restored explorer width = %d, want ~60", got)
	}
}

func TestStaleLayoutFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	// A saved layout referencing a pane that no longer exists must be ignored.
	stale := `{"orient":"h","ratio":0.3,"a":{"pane":"explorer"},"b":{"pane":"ghost"}}`
	if err := os.WriteFile(filepath.Join(dir, "layout.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{})
	if m.tree != nil {
		t.Fatal("stale layout should not load; tree must stay nil until default")
	}
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	if _, ok := m.lay.Panes[ctxExplorer]; !ok {
		t.Fatal("explorer pane missing after fallback")
	}
	if _, ok := m.lay.Panes[ctxEditor]; !ok {
		t.Fatal("editor pane missing after fallback")
	}
}

func TestMouseIgnoredWhenShellOpen(t *testing.T) {
	m := sized(t, 100, 40)
	// open help shell
	m = step(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !m.shell.IsOpen() {
		t.Fatal("shell should be open")
	}
	before := m.tree
	m = step(m, press(2, 0))
	m = step(m, release(50, 20))
	if m.drag != nil {
		t.Fatal("drag should not start while shell open")
	}
	if m.tree != before {
		t.Fatal("tree should be untouched while shell open")
	}
}
