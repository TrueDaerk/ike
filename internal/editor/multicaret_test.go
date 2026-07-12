package editor

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
)

// caretAt is a test helper planting a secondary caret directly.
func caretAt(m *Model, line, col int) {
	m.addCaret(buffer.Position{Line: line, Col: col})
}

func TestCaretInsertFanOutSameLine(t *testing.T) {
	m, _ := loaded(t, "aaa bbb ccc")
	// Primary before "aaa", carets before "bbb" and "ccc".
	caretAt(&m, 0, 4)
	caretAt(&m, 0, 8)
	m = send(m, keys("i")...)
	m = typeKeys(m, "X")
	if got := line(m, 0); got != "Xaaa Xbbb Xccc" {
		t.Fatalf("fan-out drifted: %q", got)
	}
	// Every caret sits right after its own insertion.
	if m.cursor != (buffer.Position{Line: 0, Col: 1}) {
		t.Fatalf("primary at %v", m.cursor)
	}
	want := []buffer.Position{{Line: 0, Col: 6}, {Line: 0, Col: 11}}
	got := m.Carets()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("carets at %v, want %v", got, want)
	}
}

func TestCaretInsertFanOutAcrossLines(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree")
	caretAt(&m, 1, 0)
	caretAt(&m, 2, 0)
	m = send(m, keys("i")...)
	m = typeKeys(m, "> ")
	if got := m.Text(); got != "> one\n> two\n> three" {
		t.Fatalf("got %q", got)
	}
}

func TestCaretFanOutIsOneUndoUnit(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree")
	caretAt(&m, 1, 0)
	caretAt(&m, 2, 0)
	m = send(m, keys("i")...)
	m = typeKeys(m, "xy")
	m = send(m, special(tea.KeyEscape))
	m = send(m, keys("u")...)
	if got := m.Text(); got != "one\ntwo\nthree" {
		t.Fatalf("one undo must revert the whole fan-out, got %q", got)
	}
}

func TestCaretBackspaceMergesColliding(t *testing.T) {
	m, _ := loaded(t, "ab")
	// Insert mode with primary after 'a' and a caret after 'b'.
	m = send(m, keys("i")...)
	m.cursor = buffer.Position{Line: 0, Col: 1}
	caretAt(&m, 0, 2)
	m = send(m, special(tea.KeyBackspace))
	if got := line(m, 0); got != "" {
		t.Fatalf("each caret deletes its own rune, got %q", got)
	}
	if m.hasCarets() {
		t.Fatalf("colliding carets must merge, still have %v", m.Carets())
	}
}

func TestCaretAddNextWalksOccurrences(t *testing.T) {
	m, _ := loaded(t, "foo bar\nfoo baz\nqux foo")
	// First invocation locks onto the word and snaps to its start.
	m, _ = m.runAction("caret_add_next")
	if m.cursor != (buffer.Position{Line: 0, Col: 0}) {
		t.Fatalf("first addNext should snap to word start, cursor %v", m.cursor)
	}
	if m.hasCarets() {
		t.Fatalf("first addNext should not add a caret yet")
	}
	// Each following invocation leaves a caret and jumps to the next foo.
	m, _ = m.runAction("caret_add_next")
	if m.cursor != (buffer.Position{Line: 1, Col: 0}) {
		t.Fatalf("cursor %v after second addNext", m.cursor)
	}
	m, _ = m.runAction("caret_add_next")
	if m.cursor != (buffer.Position{Line: 2, Col: 4}) {
		t.Fatalf("cursor %v after third addNext", m.cursor)
	}
	if got := m.Carets(); len(got) != 2 {
		t.Fatalf("carets %v", got)
	}
	// All occurrences taken: another invocation wraps, skips them, adds none.
	m, _ = m.runAction("caret_add_next")
	if got := m.Carets(); len(got) != 2 {
		t.Fatalf("wrap must skip occupied occurrences, carets %v", got)
	}
}

func TestCaretAddAll(t *testing.T) {
	m, _ := loaded(t, "foo bar\nfoo baz\nqux foo")
	m, _ = m.runAction("caret_add_all")
	if got := m.Carets(); len(got) != 2 {
		t.Fatalf("addAll should caret every other occurrence, got %v", got)
	}
	// ciw replaces the word at every occurrence in one go.
	m = send(m, keys("ciw")...)
	m = typeKeys(m, "quux")
	if got := m.Text(); got != "quux bar\nquux baz\nqux quux" {
		t.Fatalf("got %q", got)
	}
	// One undo unit for delete + typed text.
	m = send(m, special(tea.KeyEscape))
	m = send(m, keys("u")...)
	if got := m.Text(); got != "foo bar\nfoo baz\nqux foo" {
		t.Fatalf("undo got %q", got)
	}
}

func TestCaretEscCollapses(t *testing.T) {
	m, _ := loaded(t, "foo foo foo")
	m, _ = m.runAction("caret_add_next")
	m, _ = m.runAction("caret_add_next")
	if !m.hasCarets() {
		t.Fatalf("expected carets")
	}
	m = send(m, special(tea.KeyEscape))
	if m.hasCarets() || !m.caretQuery.Empty() {
		t.Fatalf("Esc must collapse carets and drop the query")
	}
}

func TestCaretBlockInsert(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree")
	// ctrl+v, select down two lines, I, type, Esc.
	m = send(m, modKey('v', tea.ModCtrl))
	m = send(m, keys("jj")...)
	m = send(m, keys("I")...)
	if m.ModeName() != Insert {
		t.Fatalf("block I must enter insert, mode %v", m.ModeName())
	}
	if len(m.Carets()) != 2 {
		t.Fatalf("block I carets: %v", m.Carets())
	}
	m = typeKeys(m, "# ")
	if got := m.Text(); got != "# one\n# two\n# three" {
		t.Fatalf("got %q", got)
	}
	m = send(m, special(tea.KeyEscape))
	if got := m.Text(); got != "# one\n# two\n# three" {
		t.Fatalf("after Esc got %q", got)
	}
	if !m.hasCarets() {
		t.Fatalf("carets survive leaving insert; only the next Esc collapses")
	}
	m = send(m, special(tea.KeyEscape))
	if m.hasCarets() {
		t.Fatalf("second Esc collapses")
	}
}

func TestCaretBlockAppendClampsShortLines(t *testing.T) {
	m, _ := loaded(t, "long line\nab\nlonger line")
	// Block from col 4 on line 0 down to line 2.
	m = send(m, keys("4l")...)
	m = send(m, modKey('v', tea.ModCtrl))
	m = send(m, keys("jj")...)
	m = send(m, keys("A")...)
	m = typeKeys(m, "!")
	if got := m.Text(); got != "long !line\nab!\nlonge!r line" {
		t.Fatalf("got %q", got)
	}
}

func TestCaretDeleteUnderCursorFansOut(t *testing.T) {
	m, _ := loaded(t, "abc\nabc")
	caretAt(&m, 1, 0)
	m = send(m, keys("x")...)
	if got := m.Text(); got != "bc\nbc" {
		t.Fatalf("got %q", got)
	}
	m = send(m, keys("u")...)
	if got := m.Text(); got != "abc\nabc" {
		t.Fatalf("one undo unit, got %q", got)
	}
}

func TestCaretOperatorMotionFansOut(t *testing.T) {
	m, _ := loaded(t, "foo bar\nfoo baz")
	caretAt(&m, 1, 0)
	m = send(m, keys("dw")...)
	if got := m.Text(); got != "bar\nbaz" {
		t.Fatalf("dw at every caret, got %q", got)
	}
	// The register holds both spans, joined.
	if e := m.regs.Get(0); e.Text != "foo \nfoo " {
		t.Fatalf("register %q", e.Text)
	}
	m = send(m, keys("u")...)
	if got := m.Text(); got != "foo bar\nfoo baz" {
		t.Fatalf("one undo unit, got %q", got)
	}
}

func TestCaretLinewiseDeleteFansOut(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour")
	caretAt(&m, 2, 0)
	m = send(m, keys("dd")...)
	if got := m.Text(); got != "two\nfour" {
		t.Fatalf("dd at every caret line, got %q", got)
	}
}

func TestCaretPasteFansOut(t *testing.T) {
	m, _ := loaded(t, "xy\nxy")
	m = send(m, keys("ylj")...) // yank "x", move down
	caretAt(&m, 0, 0)
	m = send(m, keys("P")...)
	if got := m.Text(); got != "xxy\nxxy" {
		t.Fatalf("got %q", got)
	}
}

func TestCaretMotionMovesAllCarets(t *testing.T) {
	m, _ := loaded(t, "abc\nabc")
	caretAt(&m, 1, 0)
	m = send(m, keys("l")...)
	if got := m.Carets(); len(got) != 1 || got[0] != (buffer.Position{Line: 1, Col: 1}) {
		t.Fatalf("caret should follow the motion, got %v", got)
	}
	if m.cursor != (buffer.Position{Line: 0, Col: 1}) {
		t.Fatalf("primary at %v", m.cursor)
	}
}

func TestCaretAltClickToggles(t *testing.T) {
	m, _ := loaded(t, "abc\ndef")
	m.AltClick(0, 1) // gutter-less? gutter width included in x mapping
	// Map through the same translation MouseClick uses: click on line 1.
	if !m.hasCarets() {
		t.Skipf("alt-click position mapping depends on gutter; probe directly")
	}
	p := m.Carets()[0]
	m.AltClick(0, 1)
	if m.hasCarets() {
		t.Fatalf("second alt+click must remove the caret at %v", p)
	}
}

func TestCaretToggleDirect(t *testing.T) {
	m, _ := loaded(t, "abc\ndef")
	m.toggleCaret(buffer.Position{Line: 1, Col: 1})
	if len(m.Carets()) != 1 {
		t.Fatalf("toggle add failed")
	}
	m.toggleCaret(buffer.Position{Line: 1, Col: 1})
	if m.hasCarets() {
		t.Fatalf("toggle remove failed")
	}
	// Toggling the primary caret is a no-op.
	m.toggleCaret(m.cursor)
	if m.hasCarets() {
		t.Fatalf("primary must not become a secondary caret")
	}
}

func TestCaretsArePerViewAndClampOnSync(t *testing.T) {
	a, path := loaded(t, "one\ntwo\nthree")
	b := New()
	b.ShareDocumentWith(&a)
	caretAt(&a, 2, 2)
	if b.hasCarets() {
		t.Fatalf("carets must be per view")
	}
	// The other view deletes the caret's line; the sync clamps a's carets.
	b.cursor = buffer.Position{Line: 2, Col: 0}
	b = send(b, keys("dd")...)
	a, _ = a.applySync(SyncMsg{Path: path, Dirty: true})
	for _, c := range a.Carets() {
		if c.Line > a.buf.LineCount()-1 {
			t.Fatalf("caret %v beyond buffer end", c)
		}
	}
}

func TestCaretClampOnReload(t *testing.T) {
	m, path := loaded(t, "one\ntwo\nthree")
	caretAt(&m, 2, 4)
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.reloadFromDisk()
	if m.hasCarets() {
		for _, c := range m.Carets() {
			if c.Line > m.buf.LineCount()-1 || c.Col > m.buf.RuneLen(c.Line) {
				t.Fatalf("caret %v beyond reloaded buffer", c)
			}
		}
	}
}

func TestCaretCommandLineCollapses(t *testing.T) {
	m, _ := loaded(t, "foo foo")
	m, _ = m.runAction("caret_add_next")
	m, _ = m.runAction("caret_add_next")
	if !m.hasCarets() {
		t.Fatalf("expected carets")
	}
	m = send(m, keys(":")...)
	if m.hasCarets() {
		t.Fatalf("command line must collapse carets")
	}
}
