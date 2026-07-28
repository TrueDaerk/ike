package app

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/pane"
	"ike/internal/registry"
)

// fgSGR is the truecolor foreground escape a colour renders as, which is how a
// border colour shows up in a rendered pane box.
func fgSGR(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// focusedEditorPane focuses the startup editor and returns the model, the pane
// key and that pane's rendered box.
func focusedEditorPane(t *testing.T, m Model) (Model, string, string) {
	t.Helper()
	key := m.activeEditorKey()
	if key == "" {
		t.Fatal("startup layout must focus an editor")
	}
	m.activeWS().Panes.SetFocused(key)
	return m, key, m.renderPane(key, m.lay.Panes[key])
}

// TestPaneBorderTracksEditorMode guards the loud half of the mode signal
// (#1353): the focused editor pane's border takes the input mode's colour once
// the mode leaves Normal, so insert mode is visible from across the screen.
// Normal mode keeps BorderFocus — the resting look is unchanged.
func TestPaneBorderTracksEditorMode(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	m, _, box := focusedEditorPane(t, m)
	if !strings.Contains(box, fgSGR(m.pal().BorderFocus)) {
		t.Fatal("focused editor pane in normal mode must keep the focus border colour")
	}

	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	m, _, box = focusedEditorPane(t, m)
	if md := m.activeWS().Panes.FocusedInstance().Editor().ModeName(); md != editor.Insert {
		t.Fatalf("editor mode = %v, want Insert", md)
	}
	if !strings.Contains(box, fgSGR(editor.ModeColor(editor.Insert, m.pal()))) {
		t.Fatal("insert-mode editor pane must carry the mode border colour")
	}
	if strings.Contains(box, fgSGR(m.pal().BorderFocus)) {
		t.Fatal("insert-mode border must replace the focus border colour, not sit beside it")
	}

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m, _, box = focusedEditorPane(t, m)
	if !strings.Contains(box, fgSGR(m.pal().BorderFocus)) {
		t.Fatal("leaving insert mode must restore the focus border colour")
	}
}

// TestUnfocusedPaneIgnoresEditorMode guards that the mode colour is a signal
// about where the user is typing: an unfocused pane keeps the plain border even
// when its editor sits in insert mode.
func TestUnfocusedPaneIgnoresEditorMode(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	m, key, _ := focusedEditorPane(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	m.activeWS().Panes.SetFocused(pane.ExplorerKey)
	box := m.renderPane(key, m.lay.Panes[key])
	if strings.Contains(box, fgSGR(editor.ModeColor(editor.Insert, m.pal()))) {
		t.Fatal("an unfocused pane must not paint its editor's mode colour")
	}
	if !strings.Contains(box, fgSGR(m.pal().Border)) {
		t.Fatal("an unfocused pane must keep the plain border colour")
	}
}

// TestPaneEditorMode covers the resolver: only an editor pane showing an actual
// editor reports a mode, so terminal and tool panes keep their plain chrome.
func TestPaneEditorMode(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	m, key, _ := focusedEditorPane(t, m)
	if md, ok := paneEditorMode(m.activeWS().Panes.Get(key)); !ok || md != editor.Normal {
		t.Fatalf("editor pane: mode=%v ok=%v, want Normal/true", md, ok)
	}
	if _, ok := paneEditorMode(m.activeWS().Panes.Get(pane.ExplorerKey)); ok {
		t.Fatal("explorer pane must report no editor mode")
	}
	if _, ok := paneEditorMode(nil); ok {
		t.Fatal("a missing instance must report no editor mode")
	}
}

// TestDragColourOutranksModeColour keeps the move-drag feedback authoritative:
// while a pane is being dragged its source colour wins over the mode colour, so
// the drag stays readable no matter which mode the editor is in.
func TestDragColourOutranksModeColour(t *testing.T) {
	m := sizedWith(t, registry.Global(), 100, 40)
	m, key, _ := focusedEditorPane(t, m)
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	r := m.lay.Panes[key]
	m.drag = &dragState{kind: dragMove, srcPane: key, startX: r.X, startY: r.Y, curX: r.X + 20, curY: r.Y}
	box := m.renderPane(key, r)
	if !strings.Contains(box, fgSGR(m.pal().MoveSource)) {
		t.Fatal("a dragged pane must paint the move-source colour")
	}
	if strings.Contains(box, fgSGR(editor.ModeColor(editor.Insert, m.pal()))) {
		t.Fatal("the mode colour must not survive on a dragged pane")
	}
}
