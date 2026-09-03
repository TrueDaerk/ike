package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/bookmarks"
	"ike/internal/explorer"
	"ike/internal/palette"
)

// bookmarks_store_test.go covers the project bookmarks (#55) as the app wires
// them: the toggle command, the mnemonic and note prompts, next/previous
// stepping, the gutter glyph, the picker rows and the rename hook.

// TestBookmarkToggleCommandPersists: bookmark.toggle marks the cursor line,
// a second toggle clears it, and the state reaches disk.
func TestBookmarkToggleCommandPersists(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\nthree\n")
	m := openApp(t, a)
	m.activeWS().Panes.FocusedInstance().Editor().SetCursor(1, 0)

	m = dispatch(t, m, BookmarkToggleMsg{})
	key := bpKey(a)
	if !m.bmarks.Has(key, 1) {
		t.Fatalf("toggle did not bookmark %s:2 (%+v)", key, m.bmarks.All())
	}
	if n := bookmarks.Load().Count(); n != 1 {
		t.Fatalf("persisted bookmarks = %d, want 1", n)
	}

	m = dispatch(t, m, BookmarkToggleMsg{})
	if m.bmarks.Has(key, 1) {
		t.Fatal("the second toggle must remove the bookmark")
	}
	if n := bookmarks.Load().Count(); n != 0 {
		t.Fatalf("persisted bookmarks after removal = %d, want 0", n)
	}
}

// TestBookmarkGutterShowsMnemonic: the editor's gutter draws the mnemonic
// digit for a numbered bookmark and the flag for an anonymous one.
func TestBookmarkGutterShowsMnemonic(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\nthree\n")
	m := openApp(t, a)
	key := bpKey(a)
	m.bmarks.Add(key, 0)
	m.bmarks.SetMnemonic(key, 1, '4')

	view := m.activeWS().Panes.FocusedInstance().Editor().View()
	if !strings.Contains(view, "⚑") {
		t.Fatalf("anonymous bookmark has no gutter flag:\n%s", view)
	}
	if !strings.Contains(view, "4") {
		t.Fatalf("mnemonic bookmark has no digit in the gutter:\n%s", view)
	}
}

// TestBookmarkMnemonicPromptAssignsAndRemoves: the digit prompt assigns the
// mnemonic to the cursor line; pressing the same digit there again drops the
// bookmark (JetBrains' toggle-with-mnemonic).
func TestBookmarkMnemonicPromptAssignsAndRemoves(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\n")
	m := openApp(t, a)
	m.activeWS().Panes.FocusedInstance().Editor().SetCursor(1, 0)
	key := bpKey(a)

	m = dispatch(t, m, BookmarkMnemonicMsg{})
	if !m.bookmarkPromptOpen() {
		t.Fatal("the mnemonic prompt must open")
	}
	m = dispatch(t, m, tea.KeyPressMsg{Text: "2", Code: '2'})
	if m.bookmarkPromptOpen() {
		t.Fatal("a digit must close the prompt")
	}
	b, ok := m.bmarks.ByMnemonic('2')
	if !ok || b.Path != key || b.Line != 1 {
		t.Fatalf("mnemonic 2 = %+v %v, want %s:1", b, ok, key)
	}

	m = dispatch(t, m, BookmarkMnemonicMsg{})
	m = dispatch(t, m, tea.KeyPressMsg{Text: "2", Code: '2'})
	if m.bmarks.Has(key, 1) {
		t.Fatal("the same digit on the same line must remove the bookmark")
	}
}

// TestBookmarkMnemonicPromptEscapes: esc leaves the store untouched, and a
// non-digit key keeps the prompt open.
func TestBookmarkMnemonicPromptEscapes(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)

	m = dispatch(t, m, BookmarkMnemonicMsg{})
	m = dispatch(t, m, tea.KeyPressMsg{Text: "x", Code: 'x'})
	if !m.bookmarkPromptOpen() {
		t.Fatal("a non-digit must leave the prompt open")
	}
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.bookmarkPromptOpen() || m.bmarks.Count() != 0 {
		t.Fatalf("esc must cancel without a bookmark (%d stored)", m.bmarks.Count())
	}
}

// TestBookmarkJumpMnemonicOpensFile: bookmark.jumpMnemonic's digit navigates
// to the bookmark's file and line through the standard open funnel.
func TestBookmarkJumpMnemonicOpensFile(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	b := writeTemp(t, dir, "b.txt", "zero\none\ntwo\n")
	m := openApp(t, a)
	m.bmarks.SetMnemonic(bpKey(b), 2, '7')

	m = dispatch(t, m, BookmarkMnemonicMsg{Jump: true})
	m = dispatch(t, m, tea.KeyPressMsg{Text: "7", Code: '7'})
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if !strings.HasSuffix(ed.Path(), "b.txt") {
		t.Fatalf("focused path = %q, want b.txt", ed.Path())
	}
	if l, _ := ed.CursorPos(); l != 2 {
		t.Fatalf("jump landed on line %d, want 2", l)
	}
}

// TestBookmarkNotePromptAnnotates: the note prompt stores the typed text and
// prefills it on the next open.
func TestBookmarkNotePromptAnnotates(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\n")
	m := openApp(t, a)
	key := bpKey(a)

	m = dispatch(t, m, BookmarkNoteMsg{})
	for _, r := range "why" {
		m = dispatch(t, m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	b, ok := m.bmarks.At(key, 0)
	if !ok || b.Note != "why" {
		t.Fatalf("annotated bookmark = %+v %v, want the note \"why\"", b, ok)
	}

	m = dispatch(t, m, BookmarkNoteMsg{})
	if m.bmPrompt == nil || m.bmPrompt.input.Text != "why" {
		t.Fatalf("prompt must prefill the note, got %+v", m.bmPrompt)
	}
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if b, _ := m.bmarks.At(key, 0); b.Note != "" {
		t.Fatalf("an empty note must clear the annotation, got %q", b.Note)
	}
	if !m.bmarks.Has(key, 0) {
		t.Fatal("clearing the note must keep the bookmark")
	}
}

// TestBookmarkStepWrapsAcrossFiles: next/previous walk the project's
// bookmarks in path/line order and wrap at both ends.
func TestBookmarkStepWrapsAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\nthree\n")
	b := writeTemp(t, dir, "b.txt", "one\ntwo\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 2)
	m.bmarks.Add(bpKey(b), 1)
	m.activeWS().Panes.FocusedInstance().Editor().SetCursor(0, 0)

	m = dispatch(t, m, BookmarkStepMsg{Delta: 1})
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if l, _ := ed.CursorPos(); !strings.HasSuffix(ed.Path(), "a.txt") || l != 2 {
		t.Fatalf("next = %s:%d, want a.txt:2", ed.Path(), l)
	}
	m = dispatch(t, m, BookmarkStepMsg{Delta: 1})
	ed = m.activeWS().Panes.FocusedInstance().Editor()
	if l, _ := ed.CursorPos(); !strings.HasSuffix(ed.Path(), "b.txt") || l != 1 {
		t.Fatalf("next across files = %s:%d, want b.txt:1", ed.Path(), l)
	}
	m = dispatch(t, m, BookmarkStepMsg{Delta: 1})
	ed = m.activeWS().Panes.FocusedInstance().Editor()
	if l, _ := ed.CursorPos(); !strings.HasSuffix(ed.Path(), "a.txt") || l != 2 {
		t.Fatalf("next must wrap to %s:%d, want a.txt:2", ed.Path(), l)
	}
	m = dispatch(t, m, BookmarkStepMsg{Delta: -1})
	ed = m.activeWS().Panes.FocusedInstance().Editor()
	if l, _ := ed.CursorPos(); !strings.HasSuffix(ed.Path(), "b.txt") || l != 1 {
		t.Fatalf("previous must wrap to %s:%d, want b.txt:1", ed.Path(), l)
	}
}

// TestBookmarksPickerListsProjectBookmarks: a mnemonic bookmark shows its
// digit and note, and its aux action removes it from the store.
func TestBookmarksPickerListsProjectBookmarks(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\n")
	m := openApp(t, a)
	key := bpKey(a)
	m.bmarks.SetMnemonic(key, 1, '5')
	m.bmarks.SetNote(key, 1, "the interesting line")

	m = dispatch(t, m, ShowBookmarksMsg{})
	if !m.palette.IsOpen() {
		t.Fatal("the picker must open for a project bookmark")
	}
	items := m.bookmarks.Results("", palette.Context{})
	if len(items) != 1 {
		t.Fatalf("rows = %+v, want one", items)
	}
	if !strings.HasPrefix(items[0].Title, "⚑5  ") || !strings.Contains(items[0].Title, "a.txt:2") {
		t.Fatalf("row title = %q", items[0].Title)
	}
	if items[0].Detail != "the interesting line" {
		t.Fatalf("row detail = %q, want the note", items[0].Detail)
	}
	aux, ok := items[0].Aux.(BookmarkRemoveMsg)
	if !ok || !aux.Project || aux.Path != key || aux.Line != 1 {
		t.Fatalf("row aux = %#v", items[0].Aux)
	}
	m.palette.Close()

	m = dispatch(t, m, aux)
	if m.bmarks.Count() != 0 {
		t.Fatalf("aux removal left %+v", m.bmarks.All())
	}
}

// TestBookmarksFollowRename: an explorer rename re-keys the store instead of
// leaving the bookmark on the old path.
func TestBookmarksFollowRename(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	renamed := writeTemp(t, dir, "renamed.txt", "one\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 0)

	m = dispatch(t, m, explorer.FileMovedMsg{Old: a, New: renamed})
	if m.bmarks.Has(bpKey(a), 0) || !m.bmarks.Has(bpKey(renamed), 0) {
		t.Fatalf("bookmark did not follow the rename: %+v", m.bmarks.All())
	}
}

// TestBookmarkEditShiftFollowsInsertion: typing above a bookmark moves it
// down with its line, through the editor's adjuster hook.
func TestBookmarkEditShiftFollowsInsertion(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\nthree\n")
	m := openApp(t, a)
	key := bpKey(a)
	m.bmarks.Add(key, 2)

	ed := m.activeWS().Panes.FocusedInstance().Editor()
	ed.SetCursor(0, 0)
	*ed, _ = ed.Update(keyMsg('o')) // open a line below the cursor
	*ed, _ = ed.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.bmarks.Has(key, 3) {
		t.Fatalf("bookmark did not shift with the insertion: %+v", m.bmarks.All())
	}
}

// TestBookmarkCommandsRegistered: the whole command family reaches the
// registry (palette and menu entries resolve through it).
func TestBookmarkCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{
		"bookmark.toggle", "bookmark.toggleMnemonic", "bookmark.jumpMnemonic",
		"bookmark.annotate", "bookmark.next", "bookmark.previous",
	} {
		if _, ok := m.reg.Command(id); !ok {
			t.Errorf("%s must be registered", id)
		}
	}
}
