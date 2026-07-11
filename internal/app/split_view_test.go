package app

import (
	"strings"
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// split_view_test.go covers editor.splitViewRight/Down (#147): a second live
// shared view of the focused editor's document.

func TestSplitViewSharesDocumentAndCopiesPosition(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("line\n", 40)
	a := writeTemp(t, dir, "a.txt", content)
	m := openApp(t, a)
	srcKey := m.panes.Focused()
	src := m.panes.Get(srcKey).Editor()
	src.SetCursor(20, 2)
	src.SetScroll(10, 0)

	m = dispatch(t, m, SplitViewMsg{Zone: layout.ZoneRight})

	newKey := m.panes.Focused()
	if newKey == srcKey {
		t.Fatal("split view must focus the new pane")
	}
	ed := m.panes.Get(newKey).Editor()
	if ed.Path() != a || !ed.SharesBufferWith(src) {
		t.Fatalf("new pane must be a shared view of %q", a)
	}
	if line, col := ed.CursorPos(); line != 20 || col != 2 {
		t.Fatalf("cursor must copy from the source view, got %d,%d", line, col)
	}
	if top, _ := ed.ScrollOffset(); top != 10 {
		t.Fatalf("scroll must copy from the source view, got top %d", top)
	}
}

func TestSplitViewEditMirrorsAcrossViews(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\n")
	m := openApp(t, a)
	src := m.panes.Get(m.panes.Focused()).Editor()

	m = dispatch(t, m, SplitViewMsg{Zone: layout.ZoneBottom})
	ed := m.panes.Get(m.panes.Focused()).Editor()
	// One document: an edit through either view is visible in both.
	ed.SetCursor(0, 0)
	if src.Text() != ed.Text() {
		t.Fatal("views must read the same document")
	}
	if !ed.SharesBufferWith(src) {
		t.Fatal("views must alias one buffer")
	}
}

func TestSplitViewNoFileIsNoop(t *testing.T) {
	m := newSized() // fresh scratch editor, no file loaded
	before := len(m.panes.Keys())
	focused := m.panes.Focused()
	if inst := m.panes.Get(focused); inst.Kind() == pane.KindEditor && inst.Editor().HasFile() {
		t.Fatal("setup: expected a file-less editor")
	}

	m = dispatch(t, m, SplitViewMsg{Zone: layout.ZoneRight})

	if got := len(m.panes.Keys()); got != before {
		t.Fatalf("scratch editor must not split (panes %d -> %d)", before, got)
	}
}

func TestSplitViewCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"editor.splitViewRight", "editor.splitViewDown"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must be registered", id)
		}
	}
}
