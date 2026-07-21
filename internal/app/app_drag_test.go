package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// sized returns a model after a window size so its layout tree and geometry are
// computed, with layout persistence redirected to a temp dir.
func sized(t *testing.T, w, h int) Model {
	return sizedWith(t, registry.New(), w, h)
}

// sizedWith is sized with an explicit registry — pass registry.Global() when a
// test needs the real registered commands (e.g. keymap dispatch, #805).
func sizedWith(t *testing.T, reg *registry.Registry, w, h int) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(reg, host.MapConfig{})
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
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("setup: focus should be editor")
	}
	r := m.lay.Panes[ctxExplorer]
	// press the first content cell (inside border, padding, and title row).
	m = step(m, press(r.X+paneContentX, r.Y+paneContentY))
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindExplorer {
		t.Fatalf("click did not focus explorer: focus=%v", m.activeWS().Panes.Focused())
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
	s, ok := m.activeWS().Tree.(*layout.Split)
	if !ok || s.Orient != layout.Horizontal {
		t.Fatalf("expected horizontal split, got %#v", m.activeWS().Tree)
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
	if m.activeWS().Tree != nil {
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
	before := m.activeWS().Tree
	m = step(m, press(2, m.lay.Panes[ctxExplorer].Y))
	m = step(m, release(50, 20))
	if m.drag != nil {
		t.Fatal("drag should not start while shell open")
	}
	if m.activeWS().Tree != before {
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
	if m.activeWS().Panes.Focused() != ctxEditor {
		t.Fatalf("title click should focus the pane, focused=%q", m.activeWS().Panes.Focused())
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
	// One cell above the workspace border — the outermost row docks (#811).
	m = step(m, release(edRect.X+edRect.W/2, edRect.Y+edRect.H-2))
	if got := len(m.lay.Panes); got != before+1 {
		t.Fatalf("bottom-edge drop should split: %d panes, want %d", got, before+1)
	}
}

// TestTitleClickShowsNoMoveFeedback: a press on a title bar without any pointer
// travel must not flash the move overlay (#559) — no status hint, no source
// marker, no ghost — and a release in place changes nothing.
func TestTitleClickShowsNoMoveFeedback(t *testing.T) {
	m := sized(t, 100, 40)
	before := len(m.lay.Panes)
	x, y := 2, m.lay.Panes[ctxExplorer].Y
	m = step(m, press(x, y))
	view := m.render()
	if strings.Contains(view, "MOVE EXPLORER") {
		t.Fatal("status line shows move hint on a plain press")
	}
	if strings.Contains(view, "⤴") {
		t.Fatal("source pane shows move marker on a plain press")
	}
	if _, _, _, ok := m.moveGhost(); ok {
		t.Fatal("ghost box rendered on a plain press")
	}
	m = step(m, release(x, y))
	if m.drag != nil {
		t.Fatal("drag state should clear on release")
	}
	if got := len(m.lay.Panes); got != before {
		t.Fatalf("plain click changed layout: %d panes, want %d", got, before)
	}
}

// TestTitleDragBelowThresholdIsClick: travel of fewer columns than the engage
// threshold on the same row stays a click (#559).
func TestTitleDragBelowThresholdIsClick(t *testing.T) {
	m := sized(t, 100, 40)
	before := len(m.lay.Panes)
	x, y := 2, m.lay.Panes[ctxExplorer].Y
	m = step(m, press(x, y))
	m = step(m, motion(x+moveEngageCols-1, y))
	if strings.Contains(m.render(), "MOVE EXPLORER") {
		t.Fatal("move hint shown below the engage threshold")
	}
	m = step(m, release(x+moveEngageCols-1, y))
	if got := len(m.lay.Panes); got != before {
		t.Fatalf("sub-threshold drag changed layout: %d panes, want %d", got, before)
	}
}

// TestTitleDragPastThresholdEngages: one row of vertical travel — or the column
// threshold sideways — engages the move and its feedback (#559).
func TestTitleDragPastThresholdEngages(t *testing.T) {
	m := sized(t, 100, 40)
	x, y := 2, m.lay.Panes[ctxExplorer].Y
	m = step(m, press(x, y))
	m = step(m, motion(x+moveEngageCols, y)) // sideways past the threshold
	if !strings.Contains(m.render(), "MOVE EXPLORER") {
		t.Fatal("move hint missing after horizontal travel past the threshold")
	}
	m = step(m, release(x, y)) // back at the press cell: engaged, but a no-op drop
	m = step(m, press(x, y))
	m = step(m, motion(x, y+1)) // one row down engages immediately
	if !strings.Contains(m.render(), "MOVE EXPLORER") {
		t.Fatal("move hint missing after one row of vertical travel")
	}
}

// TestDragDockToOuterEdge guards #811: releasing a whole-pane drag on the
// workspace's outermost strip docks the pane full-span against that edge.
func TestDragDockToOuterEdge(t *testing.T) {
	m := sized(t, 100, 40)
	body := m.bodyRect()

	// Drag the explorer to the outer bottom edge: full-width dock.
	m = step(m, press(2, m.lay.Panes[ctxExplorer].Y))
	m = step(m, release(body.X+body.W/2, body.Y+body.H-1))
	r := m.lay.Panes[ctxExplorer]
	if r.W != body.W {
		t.Fatalf("bottom dock: explorer width = %d, want full body width %d", r.W, body.W)
	}
	if r.Y+r.H != body.Y+body.H {
		t.Fatalf("bottom dock: explorer not at the bottom (Y=%d H=%d)", r.Y, r.H)
	}

	// Drag it to the outer right edge: full-height dock. The pane's top
	// border now sits on a split boundary where the resize band wins (#761),
	// so grab the title text row just inside it.
	m = step(m, press(r.X+2, r.Y+1))
	m = step(m, release(body.X+body.W-1, body.Y+body.H/2))
	r = m.lay.Panes[ctxExplorer]
	if r.H != body.H {
		t.Fatalf("right dock: explorer height = %d, want full body height %d", r.H, body.H)
	}
	if r.X+r.W != body.X+body.W {
		t.Fatalf("right dock: explorer not at the right edge (X=%d W=%d)", r.X, r.W)
	}
}

// TestDragDockShowsFullSpanPreview: hovering the outer strip previews the
// full-span target (ghost + status hint) before release.
func TestDragDockShowsFullSpanPreview(t *testing.T) {
	m := sized(t, 100, 40)
	body := m.bodyRect()
	m = step(m, press(2, m.lay.Panes[ctxExplorer].Y))
	m = step(m, motion(body.X+body.W/2, body.Y+body.H-1))

	box, gx, gy, ok := m.moveGhost()
	if !ok {
		t.Fatal("expected a dock ghost over the outer strip")
	}
	if gx != body.X || lipgloss.Width(box) != body.W {
		t.Fatalf("dock ghost at x=%d w=%d, want full-width at %d w=%d", gx, lipgloss.Width(box), body.X, body.W)
	}
	_ = gy
	if view := m.render(); !strings.Contains(view, "dock bottom (full width)") {
		t.Fatalf("status hint missing the dock label:\n%s", view)
	}
}
