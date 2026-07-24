package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/marks"
	"ike/internal/palette"
)

// keyMsg builds a printable key press for driving an editor directly.
func keyMsg(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Text: string(r), Code: r} }

// bookmarks_test.go covers the bookmarks picker and the app half of the vim
// marks (#1151): listing, jumping (local, and global through the open
// funnel), the aux removal, and the palette-only registry command.

// TestBookmarksModeListsLocalAndGlobal: rows show "'x  path:line" with a
// preview detail, carry jump msgs, and expose the aux removal.
func TestBookmarksModeListsLocalAndGlobal(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n  two\nthree\n")
	b := writeTemp(t, dir, "b.txt", "bee line\n")
	m := openApp(t, a)
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	ed.SetCursor(1, 2)
	*ed, _ = ed.Update(keyMsg('m'))
	*ed, _ = ed.Update(keyMsg('c'))
	m.gmarks.Set('B', b, 0, 3)

	mode := &bookmarksMode{}
	mode.Set(ed, m.gmarks)
	items := mode.Results("", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(items), items)
	}
	// Rows sort by title: the global 'B row before the local 'c row.
	if !strings.HasPrefix(items[0].Title, "'B  ") || !strings.Contains(items[0].Title, "b.txt:1") {
		t.Fatalf("global row title = %q", items[0].Title)
	}
	if items[0].Detail != "bee line" {
		t.Fatalf("global row preview = %q", items[0].Detail)
	}
	jump, ok := items[0].Msg.(BookmarkJumpMsg)
	if !ok || jump.Local || jump.Letter != 'B' || jump.Line != 0 || jump.Col != 3 {
		t.Fatalf("global row msg = %#v", items[0].Msg)
	}
	if aux, ok := items[0].Aux.(BookmarkRemoveMsg); !ok || aux.Local || aux.Letter != 'B' {
		t.Fatalf("global row aux = %#v", items[0].Aux)
	}
	if !strings.HasPrefix(items[1].Title, "'c  ") || !strings.Contains(items[1].Title, "a.txt:2") {
		t.Fatalf("local row title = %q", items[1].Title)
	}
	if items[1].Detail != "two" {
		t.Fatalf("local row preview = %q", items[1].Detail)
	}
	if jump := items[1].Msg.(BookmarkJumpMsg); !jump.Local || jump.Letter != 'c' || jump.Line != 1 {
		t.Fatalf("local row msg = %#v", items[1].Msg)
	}
}

// TestShowBookmarksOpensPicker: with marks present the palette opens locked;
// without any it stays closed (toast instead).
func TestShowBookmarksOpensPicker(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)
	m = dispatch(t, m, ShowBookmarksMsg{})
	if m.palette.IsOpen() {
		t.Fatal("no marks: the picker must not open")
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	*ed, _ = ed.Update(keyMsg('m'))
	*ed, _ = ed.Update(keyMsg('a'))
	m = dispatch(t, m, ShowBookmarksMsg{})
	if !m.palette.IsOpen() {
		t.Fatal("picker must open with a mark present")
	}
	m.palette.Close()
}

// TestBookmarkJumpLocal: a local row's activation jumps the focused editor.
func TestBookmarkJumpLocal(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\nthree\n")
	m := openApp(t, a)
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	ed.SetCursor(2, 1)
	*ed, _ = ed.Update(keyMsg('m'))
	*ed, _ = ed.Update(keyMsg('a'))
	ed.SetCursor(0, 0)
	m = dispatch(t, m, BookmarkJumpMsg{Local: true, Letter: 'a', Line: 2, Col: 1})
	if l, c := m.activeWS().Panes.FocusedInstance().Editor().CursorPos(); l != 2 || c != 1 {
		t.Fatalf("local jump = %d:%d, want 2:1", l, c)
	}
}

// TestGlobalMarkJumpOpensFile: the editor's GlobalMarkJumpMsg opens the
// mark's file through the standard funnel and lands on the position; the
// quote (non-exact) form settles on the first non-blank.
func TestGlobalMarkJumpOpensFile(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	b := writeTemp(t, dir, "b.txt", "zero\n  indented target\n")
	m := openApp(t, a)
	m.gmarks.Set('B', b, 1, 5)

	m = dispatch(t, m, editor.GlobalMarkJumpMsg{Letter: 'B', Exact: true})
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if !strings.HasSuffix(ed.Path(), "b.txt") {
		t.Fatalf("focused path = %q, want b.txt", ed.Path())
	}
	if l, c := ed.CursorPos(); l != 1 || c != 5 {
		t.Fatalf("exact jump = %d:%d, want 1:5", l, c)
	}

	ed.SetCursor(0, 0)
	m = dispatch(t, m, editor.GlobalMarkJumpMsg{Letter: 'B', Exact: false})
	ed = m.activeWS().Panes.FocusedInstance().Editor()
	if l, c := ed.CursorPos(); l != 1 || c != 2 {
		t.Fatalf("line jump = %d:%d, want 1:2 (first non-blank)", l, c)
	}
}

// TestGlobalMarkJumpUnsetToasts: an unset letter never navigates.
func TestGlobalMarkJumpUnsetToasts(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)
	m = dispatch(t, m, editor.GlobalMarkJumpMsg{Letter: 'Q', Exact: true})
	if p := m.activeWS().Panes.FocusedInstance().Editor().Path(); !strings.HasSuffix(p, "a.txt") {
		t.Fatalf("focus moved to %q on an unset mark", p)
	}
}

// TestBookmarkRemove: the aux action drops local and global marks and
// re-lists.
func TestBookmarkRemove(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	*ed, _ = ed.Update(keyMsg('m'))
	*ed, _ = ed.Update(keyMsg('a'))
	m.gmarks.Set('B', a, 0, 0)

	m = dispatch(t, m, BookmarkRemoveMsg{Local: true, Letter: 'a'})
	if n := len(m.activeWS().Panes.FocusedInstance().Editor().LocalMarks()); n != 0 {
		t.Fatalf("local marks after remove = %d, want 0", n)
	}
	m = dispatch(t, m, BookmarkRemoveMsg{Letter: 'B'})
	if _, ok := m.gmarks.Get('B'); ok {
		t.Fatal("global mark survived its removal")
	}
	if items := m.bookmarks.Results("", palette.Context{}); len(items) != 0 {
		t.Fatalf("picker rows after removals = %+v, want none", items)
	}
}

// TestGlobalMarkPersistedViaEditorKeys: m{A} in the editor writes through the
// injected hook into the persistent store.
func TestGlobalMarkPersistedViaEditorKeys(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\n")
	m := openApp(t, a)
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	ed.SetCursor(1, 1)
	*ed, _ = ed.Update(keyMsg('m'))
	*ed, _ = ed.Update(keyMsg('G'))
	mk, ok := m.gmarks.Get('G')
	if !ok || mk.Line != 1 || mk.Col != 1 || !strings.HasSuffix(mk.Path, "a.txt") {
		t.Fatalf("stored mark = %+v %v, want a.txt:1:1", mk, ok)
	}
	// A fresh store over the same state dir sees it (restart survival).
	fresh := &marks.Store{}
	if _, ok := fresh.Get('G'); !ok {
		t.Fatal("mark must persist to the state store")
	}
}

// TestBookmarksCommandRegistered: nav.bookmarks exists (palette-only, no
// default chord — keymap budget #711).
func TestBookmarksCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("nav.bookmarks"); !ok {
		t.Fatal("nav.bookmarks must be registered")
	}
}
