package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// sized returns a model after a window size so its layout tree and geometry are
// computed, with layout persistence redirected to a temp dir.
func sized(t *testing.T, w, h int) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = out.(Model)
	// Drain the explorer's async root scan so the tree has visible rows. Init
	// batches several startup commands (explorer scan, git status 0320), so
	// unwrap tea.BatchMsg and apply each command's message once.
	queue := []tea.Cmd{m.Init()}
	for len(queue) > 0 {
		cmd := queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		switch msg := cmd().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			out, _ := m.Update(msg)
			m = out.(Model)
		}
	}
	return m
}

func press(x, y int) tea.MouseMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func motion(x, y int) tea.MouseMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func release(x, y int) tea.MouseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func step(m Model, msg tea.Msg) Model {
	out, _ := m.Update(msg)
	mm := out.(Model)
	// Wheel events coalesce until a flush (#238); tests don't run commands, so
	// apply the batch here to keep each step synchronous.
	if len(mm.pendingWheel) > 0 {
		out, _ = mm.Update(wheelFlushMsg{})
		mm = out.(Model)
	}
	return mm
}

func TestMouseClickFocusesExplorer(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus() // move focus to the editor
	if m.panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("setup: focus should be editor")
	}
	r := m.lay.Panes[ctxExplorer]
	// press the first content cell (inside border, padding, and title row).
	m = step(m, press(r.X+paneContentX, r.Y+paneContentY))
	if m.panes.FocusedInstance().Kind() != pane.KindExplorer {
		t.Fatalf("click did not focus explorer: focus=%v", m.panes.Focused())
	}
}

func TestMouseHoverHighlightsExplorerRow(t *testing.T) {
	m := sized(t, 100, 40)
	r := m.lay.Panes[ctxExplorer]
	// move (no button) over the second content row.
	hover := tea.MouseMotionMsg{X: r.X + paneContentX, Y: r.Y + paneContentY + 1, Button: tea.MouseNone}
	m = step(m, hover)
	if got := m.explorer().HoverRow(); got != 1 {
		t.Fatalf("hover row = %d want 1", got)
	}
	// moving off the pane clears it.
	off := tea.MouseMotionMsg{X: r.X + r.W + 5, Y: r.Y + 1, Button: tea.MouseNone}
	m = step(m, off)
	if got := m.explorer().HoverRow(); got != -1 {
		t.Fatalf("hover row = %d want -1 after leaving pane", got)
	}
}

func TestOpenFileMarksActiveInExplorer(t *testing.T) {
	m := sized(t, 100, 40)
	abs, err := filepath.Abs("app.go") // a real, visible root-level file (cwd is this pkg)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: abs})
	m = out.(Model)
	if got := m.explorer().Active(); got != abs {
		t.Fatalf("explorer active = %q want %q", got, abs)
	}
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
	m = step(m, press(2, m.lay.Panes[ctxExplorer].Y))
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
	m = step(m, press(2, m.lay.Panes[ctxExplorer].Y)) // grab explorer title
	edRect := m.lay.Panes[ctxEditor]
	m = step(m, motion(edRect.X+edRect.W-2, edRect.Y+edRect.H/2)) // hover editor right half
	view := m.render()
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

func TestMoveDragShowsGhostBox(t *testing.T) {
	m := sized(t, 100, 40)
	m = step(m, press(2, m.lay.Panes[ctxExplorer].Y))
	edRect := m.lay.Panes[ctxEditor]
	// hover the right half of the editor → ghost on its right half.
	hx, hy := edRect.X+edRect.W-2, edRect.Y+edRect.H/2
	m = step(m, motion(hx, hy))

	box, gx, gy, ok := m.moveGhost()
	if !ok {
		t.Fatal("expected a ghost box over a valid drop target")
	}
	if !strings.Contains(box, "EXPLORER") {
		t.Fatalf("ghost box should label the dragged pane:\n%s", box)
	}
	// Right zone → ghost sits in the right half of the editor pane.
	if gx <= edRect.X+edRect.W/2-1 {
		t.Fatalf("ghost x=%d not in editor's right half (pane x=%d w=%d)", gx, edRect.X, edRect.W)
	}
	if gy != edRect.Y {
		t.Fatalf("ghost y=%d, want pane top %d", gy, edRect.Y)
	}
	// No ghost when the cursor is over the source pane's interior (center): a
	// self-drop there is a no-op, not a spawn.
	exRect := m.lay.Panes[ctxExplorer]
	m = step(m, motion(exRect.X+exRect.W/2, exRect.Y+exRect.H/2))
	if _, _, _, ok := m.moveGhost(); ok {
		t.Fatal("no ghost expected while hovering the source pane interior")
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
	m = step(m, tea.KeyPressMsg{Text: "?", Code: '?'})
	if !m.shell.IsOpen() {
		t.Fatal("shell should be open")
	}
	before := m.tree
	m = step(m, press(2, m.lay.Panes[ctxExplorer].Y))
	m = step(m, release(50, 20))
	if m.drag != nil {
		t.Fatal("drag should not start while shell open")
	}
	if m.tree != before {
		t.Fatal("tree should be untouched while shell open")
	}
}

func TestActiveFollowsFocusedEditor(t *testing.T) {
	m := sized(t, 100, 40)
	a, err := filepath.Abs("app.go")
	if err != nil {
		t.Fatal(err)
	}
	b, err := filepath.Abs("session.go")
	if err != nil {
		b = a // fallback; any second real file works
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: a})
	m = out.(Model)
	out, _ = m.Update(explorer.OpenFileMsg{Path: b, NewPane: true})
	m = out.(Model)
	if got := m.explorer().Active(); got != b {
		t.Fatalf("active = %q want %q after opening in split", got, b)
	}
	if !m.explorer().IsOpen(a) || !m.explorer().IsOpen(b) {
		t.Fatal("both files should be in the explorer's open set")
	}
	// Focus back on the first editor: the accent must follow.
	m.setFocus(m.editorKeyForPath(a))
	if got := m.explorer().Active(); got != a {
		t.Fatalf("active = %q want %q after refocusing", got, a)
	}
}

// TestTitleClickDoesNotSplit guards #304: a press+release on a pane's title
// band without leaving it is a click — it focuses the pane and must not spawn
// a split (the title band doubles as the top edgeZone).
func TestTitleClickDoesNotSplit(t *testing.T) {
	m := sized(t, 100, 40)
	before := len(m.lay.Panes)
	edRect := m.lay.Panes[ctxEditor]
	m = step(m, press(edRect.X+2, edRect.Y))
	m = step(m, release(edRect.X+2, edRect.Y))
	if got := len(m.lay.Panes); got != before {
		t.Fatalf("title click must not split: %d panes, want %d", got, before)
	}
	if m.panes.Focused() != ctxEditor {
		t.Fatalf("title click should focus the pane, focused=%q", m.panes.Focused())
	}
	// The second title row (tab bar) behaves the same.
	m = step(m, press(edRect.X+2, edRect.Y+1))
	m = step(m, release(edRect.X+3, edRect.Y+1))
	if got := len(m.lay.Panes); got != before {
		t.Fatalf("tab-row click must not split: %d panes, want %d", got, before)
	}
}

// TestTitleDragOutOfBandStillSplits: dragging from the title band to the
// source pane's bottom edge keeps spawning the self-split.
func TestTitleDragOutOfBandStillSplits(t *testing.T) {
	m := sized(t, 100, 40)
	before := len(m.lay.Panes)
	edRect := m.lay.Panes[ctxEditor]
	m = step(m, press(edRect.X+edRect.W/2, edRect.Y))
	m = step(m, release(edRect.X+edRect.W/2, edRect.Y+edRect.H-1))
	if got := len(m.lay.Panes); got != before+1 {
		t.Fatalf("bottom-edge drop should split: %d panes, want %d", got, before+1)
	}
}
