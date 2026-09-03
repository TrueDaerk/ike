package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/bookmarks"
)

// bookmarks_overview_test.go covers the project-wide bookmarks overview
// (#2251): the grouped listing with previews and descriptions, the speed
// search, and the three row actions (jump, edit description, delete).

// overviewBody renders the open overview's body at a comfortable width.
func overviewBody(m Model) string { return m.bookmarkOverviewBody(80) }

// key feeds one printable rune to the model.
func typeRune(t *testing.T, m Model, r rune) Model {
	t.Helper()
	return dispatch(t, m, tea.KeyPressMsg{Text: string(r), Code: r})
}

// TestBookmarkOverviewListsGroupedRows: every bookmark shows under its file
// with the bookmarked line's text and its description.
func TestBookmarkOverviewListsGroupedRows(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\nbeta\ngamma\n")
	b := writeTemp(t, dir, "b.txt", "delta\nepsilon\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 1)
	m.bmarks.SetNote(bpKey(a), 1, "second line here")
	m.bmarks.SetMnemonic(bpKey(b), 1, '3')

	m = dispatch(t, m, BookmarkOverviewMsg{})
	if !m.bookmarkOverviewOpen() {
		t.Fatal("the overview must open")
	}
	body := overviewBody(m)
	for _, want := range []string{"a.txt", "b.txt", "beta", "epsilon", "second line here", "3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview body misses %q:\n%s", want, body)
		}
	}
	if len(m.bmOverview.flat) != 2 {
		t.Fatalf("overview holds %d rows, want 2", len(m.bmOverview.flat))
	}
}

// TestBookmarkOverviewEmptyStoreStaysClosed: with no bookmarks the command
// only explains itself.
func TestBookmarkOverviewEmptyStoreStaysClosed(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\n")
	m := openApp(t, a)

	m = dispatch(t, m, BookmarkOverviewMsg{})
	if m.bookmarkOverviewOpen() {
		t.Fatal("an empty store must not open the overview")
	}
}

// TestBookmarkOverviewSpeedSearchFilters: typing narrows the list to the
// matching rows — path, preview and description all count — and esc clears
// the query before it closes the overview.
func TestBookmarkOverviewSpeedSearchFilters(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\nbeta\n")
	b := writeTemp(t, dir, "b.txt", "delta\nepsilon\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 0)
	m.bmarks.Add(bpKey(b), 1)
	m.bmarks.SetNote(bpKey(b), 1, "needle")

	m = dispatch(t, m, BookmarkOverviewMsg{})
	for _, r := range "needle" {
		m = typeRune(t, m, r)
	}
	if n := len(m.bmOverview.flat); n != 1 {
		t.Fatalf("filtered rows = %d, want 1:\n%s", n, overviewBody(m))
	}
	if got := m.bmOverview.flat[0]; got.Path != bpKey(b) {
		t.Fatalf("filtered row = %+v, want the b.txt bookmark", got)
	}

	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.bookmarkOverviewOpen() {
		t.Fatal("the first esc must only clear the filter")
	}
	if n := len(m.bmOverview.flat); n != 2 {
		t.Fatalf("rows after clearing the filter = %d, want 2", n)
	}
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.bookmarkOverviewOpen() {
		t.Fatal("the second esc must close the overview")
	}
}

// TestBookmarkOverviewEnterJumps: enter opens the selected bookmark's file at
// its line through the standard open funnel and closes the overview.
func TestBookmarkOverviewEnterJumps(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\n")
	b := writeTemp(t, dir, "b.txt", "delta\nepsilon\nzeta\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 0)
	m.bmarks.Add(bpKey(b), 2)

	m = dispatch(t, m, BookmarkOverviewMsg{})
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.bookmarkOverviewOpen() {
		t.Fatal("enter must close the overview")
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if l, _ := ed.CursorPos(); !strings.HasSuffix(ed.Path(), "b.txt") || l != 2 {
		t.Fatalf("jump landed on %s:%d, want b.txt:2", ed.Path(), l)
	}
}

// TestBookmarkOverviewDeleteRemoves: the delete key drops the selected
// bookmark from the store and from the list, and persists.
func TestBookmarkOverviewDeleteRemoves(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\nbeta\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 0)
	m.bmarks.Add(bpKey(a), 1)
	m.saveBookmarks()

	m = dispatch(t, m, BookmarkOverviewMsg{})
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.bmarks.Has(bpKey(a), 0) {
		t.Fatal("delete must drop the selected bookmark")
	}
	if !m.bookmarkOverviewOpen() || len(m.bmOverview.flat) != 1 {
		t.Fatalf("the overview must stay open with the remaining row (%+v)", m.bmOverview)
	}
	if n := bookmarks.Load().Count(); n != 1 {
		t.Fatalf("persisted bookmarks = %d, want 1", n)
	}

	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyDelete})
	if m.bookmarkOverviewOpen() {
		t.Fatal("removing the last bookmark must close the overview")
	}
	if n := bookmarks.Load().Count(); n != 0 {
		t.Fatalf("persisted bookmarks = %d, want 0", n)
	}
}

// TestBookmarkOverviewEditsDescription: ctrl+e opens the note prompt on the
// selected bookmark — not on the cursor line — and saving lands back in the
// overview with the new description on the row.
func TestBookmarkOverviewEditsDescription(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\n")
	b := writeTemp(t, dir, "b.txt", "delta\nepsilon\n")
	m := openApp(t, a)
	m.bmarks.Add(bpKey(a), 0)
	m.bmarks.Add(bpKey(b), 1)

	m = dispatch(t, m, BookmarkOverviewMsg{})
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = dispatch(t, m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if !m.bookmarkPromptOpen() || m.bmPrompt.key != bpKey(b) || m.bmPrompt.line != 1 {
		t.Fatalf("ctrl+e must open the note prompt on the selected bookmark, got %+v", m.bmPrompt)
	}
	for _, r := range "why" {
		m = typeRune(t, m, r)
	}
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if bm, ok := m.bmarks.At(bpKey(b), 1); !ok || bm.Note != "why" {
		t.Fatalf("edited bookmark = %+v %v, want the note \"why\"", bm, ok)
	}
	if !m.bookmarkOverviewOpen() {
		t.Fatal("saving must return to the overview")
	}
	if body := overviewBody(m); !strings.Contains(body, "why") {
		t.Fatalf("the overview must show the new description:\n%s", body)
	}
}

// TestBookmarkOverviewEditCancelReturns: esc in the prompt opened from the
// overview leaves the description untouched and reopens the list.
func TestBookmarkOverviewEditCancelReturns(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\n")
	m := openApp(t, a)
	m.bmarks.SetNote(bpKey(a), 0, "keep")

	m = dispatch(t, m, BookmarkOverviewMsg{})
	m = dispatch(t, m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if m.bmPrompt == nil || m.bmPrompt.input.Text != "keep" {
		t.Fatalf("the prompt must prefill the description, got %+v", m.bmPrompt)
	}
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.bookmarkOverviewOpen() {
		t.Fatal("cancelling must return to the overview")
	}
	if bm, _ := m.bmarks.At(bpKey(a), 0); bm.Note != "keep" {
		t.Fatalf("cancelled edit changed the description to %q", bm.Note)
	}
}

// TestBookmarkAnnotateCreatesDescribedBookmark: the note prompt on an
// unbookmarked line places the bookmark together with its description — the
// "describe while placing" flavour.
func TestBookmarkAnnotateCreatesDescribedBookmark(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "alpha\nbeta\n")
	m := openApp(t, a)
	m.activeWS().Panes.FocusedInstance().Editor().SetCursor(1, 0)

	m = dispatch(t, m, BookmarkNoteMsg{})
	for _, r := range "todo" {
		m = typeRune(t, m, r)
	}
	m = dispatch(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	bm, ok := m.bmarks.At(bpKey(a), 1)
	if !ok || bm.Note != "todo" {
		t.Fatalf("annotating an unbookmarked line must place it: %+v %v", bm, ok)
	}
	if n := bookmarks.Load().Count(); n != 1 {
		t.Fatalf("persisted bookmarks = %d, want 1", n)
	}
}

// TestBookmarkOverviewCommandRegistered: the overview is reachable from the
// palette.
func TestBookmarkOverviewCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("bookmark.overview"); !ok {
		t.Fatal("bookmark.overview must be registered")
	}
}
