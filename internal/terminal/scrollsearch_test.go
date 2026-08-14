package terminal

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// searchModel spawns a shell, fills the scrollback with seq output and
// returns a scrolled model — the state whose `/` opens the search (#1169).
func searchModel(t *testing.T) *Model {
	t.Helper()
	c := &collector{}
	s := startSh(t, c)
	for _, r := range "seq 1 100\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "scrollback filled", func() bool {
		return s.ScrollbackLen() > 50 && findRow(s, "100") >= 0
	})
	m := &Model{sess: s, w: 80, h: 24}
	m.ScrollBy(10)
	return m
}

func press(m *Model, code rune, text string) { m.Update(tea.KeyPressMsg{Code: code, Text: text}) }

func typeQuery(m *Model, q string) {
	for _, r := range q {
		press(m, r, string(r))
	}
}

// TestSearchOpensOnlyFromScrollback guards the capture rule: `/` opens the
// search only while scrolled into scrollback; at the live view it is shell
// input (paths!), and alt-screen / mouse-reporting children keep their own /.
func TestSearchOpensOnlyFromScrollback(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	if !m.Searching() {
		t.Fatal("/ while scrolled must open the search")
	}
	press(m, tea.KeyEscape, "")

	m.scroll = 0
	press(m, '/', "/")
	if m.Searching() {
		t.Fatal("/ at the live view must stay shell input")
	}
	if !m.occupied {
		t.Fatal("the live-view / must have been forwarded to the shell")
	}
}

// TestSearchAltScreenPassthrough: with the child on the alternate screen the
// scrolled state never captures `/` (vim owns its own search).
func TestSearchAltScreenPassthrough(t *testing.T) {
	m := searchModel(t)
	for _, r := range "printf '\\033[?1049h'\r" {
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "alt screen", func() bool { return m.sess.AltScreen() })
	m.scroll = 10
	press(m, '/', "/")
	if m.Searching() {
		t.Fatal("/ must pass through to an alt-screen child")
	}
}

// TestSearchMouseModePassthrough: a mouse-reporting child keeps `/` too.
func TestSearchMouseModePassthrough(t *testing.T) {
	m := searchModel(t)
	for _, r := range "printf '\\033[?1000h'\r" {
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "mouse mode", func() bool { return m.sess.WantsMouse() })
	m.scroll = 10
	press(m, '/', "/")
	if m.Searching() {
		t.Fatal("/ must pass through to a mouse-reporting child")
	}
}

// TestSearchJumpsUpward guards the incremental jump: typing scrolls to the
// nearest match at or above the anchored view, and the match line becomes
// visible; esc restores the pre-search offset, enter keeps the position.
func TestSearchJumpsUpward(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	typeQuery(m, "37")
	want := findRow(m.sess, "37")
	if want < 0 {
		t.Fatal("setup: no line 37 in the history")
	}
	if m.search.cur != want {
		t.Fatalf("current match = line %d, want %d", m.search.cur, want)
	}
	sb := m.sess.ScrollbackLen()
	top := sb - m.Scroll()
	if want < top || want > top+m.h-1 {
		t.Fatalf("match line %d not visible in window [%d,%d]", want, top, top+m.h-1)
	}
	// esc: back to the offset the search opened on.
	press(m, tea.KeyEscape, "")
	if m.Searching() || m.Scroll() != 10 {
		t.Fatalf("esc must close and restore scroll 10, got %d (open=%v)", m.Scroll(), m.Searching())
	}
	// enter keeps the jumped position.
	press(m, '/', "/")
	typeQuery(m, "37")
	at := m.Scroll()
	press(m, tea.KeyEnter, "")
	if m.Searching() || m.Scroll() != at {
		t.Fatalf("enter must close keeping scroll %d, got %d", at, m.Scroll())
	}
}

// TestSearchStepAndWrap: ctrl+p/up step to older matches, ctrl+n/down to
// newer ones, both wrapping around the match set.
func TestSearchStepAndWrap(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	typeQuery(m, "99") // matches exactly the "99" line
	matches := m.searchMatches()
	if len(matches) != 1 {
		t.Fatalf("setup: %d matches for 99, want 1", len(matches))
	}
	typeQuery(m, "")
	press(m, tea.KeyBackspace, "")
	press(m, tea.KeyBackspace, "")
	typeQuery(m, "9") // 9, 19, ..., 89, 9x rows: many matches
	matches = m.searchMatches()
	if len(matches) < 3 {
		t.Fatalf("setup: want several matches, got %d", len(matches))
	}
	cur := m.search.cur
	idx := -1
	for i, v := range matches {
		if v == cur {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("current match %d not in match set", cur)
	}
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	wantPrev := matches[(idx-1+len(matches))%len(matches)]
	if m.search.cur != wantPrev {
		t.Fatalf("ctrl+p: cur = %d, want %d", m.search.cur, wantPrev)
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.search.cur != cur {
		t.Fatalf("ctrl+n must step back down to %d, got %d", cur, m.search.cur)
	}
	// Wrap upward: stepping to older past the first match lands on the last.
	m.search.cur = matches[0]
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.search.cur != matches[len(matches)-1] {
		t.Fatalf("up from the oldest match must wrap to the newest, got %d", m.search.cur)
	}
	// Wrap downward: newest -> oldest.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.search.cur != matches[0] {
		t.Fatalf("down from the newest match must wrap to the oldest, got %d", m.search.cur)
	}
}

// TestSearchViewFieldAndCounter: the pane's bottom row carries the query
// field with the match counter while the search is open.
func TestSearchViewFieldAndCounter(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	typeQuery(m, "42")
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	last := rows[len(rows)-1]
	if !strings.HasPrefix(last, "/42") {
		t.Fatalf("bottom row must show the query field, got %q", last)
	}
	if !strings.Contains(last, "1/1") {
		t.Fatalf("bottom row must show the 1/1 counter, got %q", last)
	}
	typeQuery(m, "zz")
	rows = strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[len(rows)-1], "no matches") {
		t.Fatalf("a missing query must show the miss, got %q", rows[len(rows)-1])
	}
	// Every key is consumed while open: nothing reached the shell.
	if m.occupied {
		t.Fatal("search keys must not be forwarded to the shell")
	}
}

// TestStartSearchFromLiveView guards the cmd+f entry point (#1504): the
// explicit chord opens the search from the live view (no prior scrollback
// entry needed), typing jumps to a match, and esc returns to the live view.
func TestStartSearchFromLiveView(t *testing.T) {
	m := searchModel(t)
	m.scroll = 0
	if !m.StartSearch() {
		t.Fatal("StartSearch must open from the live view")
	}
	if !m.Searching() {
		t.Fatal("search field must be open after StartSearch")
	}
	typeQuery(m, "17")
	if m.scroll == 0 {
		t.Fatal("a match above the live view must scroll into scrollback")
	}
	press(m, tea.KeyEscape, "")
	if m.Searching() {
		t.Fatal("esc must close the search")
	}
	if m.scroll != 0 {
		t.Fatalf("esc must return to the live view, scroll = %d", m.scroll)
	}
}

// TestStartSearchIdempotentAndGuarded: a second StartSearch while the field
// is open stays handled without resetting the query, and alt-screen /
// mouse-reporting children keep the chord (StartSearch reports false).
func TestStartSearchIdempotentAndGuarded(t *testing.T) {
	m := searchModel(t)
	if !m.StartSearch() {
		t.Fatal("StartSearch must open while scrolled")
	}
	typeQuery(m, "42")
	if !m.StartSearch() {
		t.Fatal("StartSearch on an open field must stay handled")
	}
	if m.search.query != "42" {
		t.Fatalf("reopen must not reset the query, got %q", m.search.query)
	}
	press(m, tea.KeyEscape, "")

	for _, r := range "printf '\\033[?1049h'\r" {
		m.sess.SendKey(keyFor(r))
	}
	waitFor(t, "alt screen", func() bool { return m.sess.AltScreen() })
	if m.StartSearch() {
		t.Fatal("StartSearch must report false under an alt-screen child")
	}
}

// TestSearchCursorNavigation guards the shared-helper adoption (#1882):
// left/right/home/end move the cursor, typing and backspace act at the
// cursor position rather than always appending/trimming at the end.
func TestSearchCursorNavigation(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	typeQuery(m, "abc")
	if m.search.pos != 3 {
		t.Fatalf("pos after typing = %d, want 3", m.search.pos)
	}
	press(m, tea.KeyLeft, "")
	press(m, tea.KeyLeft, "")
	if m.search.pos != 1 {
		t.Fatalf("pos after two lefts = %d, want 1", m.search.pos)
	}
	press(m, 'x', "x")
	if m.search.query != "axbc" {
		t.Fatalf("insert at cursor = %q, want axbc", m.search.query)
	}
	press(m, tea.KeyHome, "")
	if m.search.pos != 0 {
		t.Fatalf("home must move to 0, got %d", m.search.pos)
	}
	press(m, tea.KeyEnd, "")
	if m.search.pos != 4 {
		t.Fatalf("end must move to len, got %d", m.search.pos)
	}
	press(m, tea.KeyBackspace, "")
	if m.search.query != "axb" {
		t.Fatalf("backspace at end = %q, want axb", m.search.query)
	}
}

// TestSearchWordMotionsAndDelete guards alt+left/alt+right word motions and
// ctrl+w/alt+backspace word deletion, passed through to ui.EditKey (#1882).
func TestSearchWordMotionsAndDelete(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	typeQuery(m, "foo bar")
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if m.search.pos != 4 {
		t.Fatalf("alt+left must land at the start of the last word, pos = %d, want 4", m.search.pos)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	if m.search.pos != 0 {
		t.Fatalf("alt+left again must land at the start, pos = %d, want 0", m.search.pos)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})
	if m.search.pos != 3 {
		t.Fatalf("alt+right must land after the first word, pos = %d, want 3", m.search.pos)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	m.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if m.search.query != "foo " {
		t.Fatalf("ctrl+w must delete the trailing word, got %q", m.search.query)
	}
}

// TestSearchPasteGoesToQuery guards the paste routing fix (#1882): a
// bracketed paste while the search is open must land in the query, not the
// shell underneath.
func TestSearchPasteGoesToQuery(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	m.PasteText("37")
	if m.occupied {
		t.Fatal("paste while the search is open must not reach the shell")
	}
	if m.search.query != "37" {
		t.Fatalf("paste must land in the search query, got %q", m.search.query)
	}
	want := findRow(m.sess, "37")
	if m.search.cur != want {
		t.Fatalf("paste must trigger the match jump, cur = %d, want %d", m.search.cur, want)
	}
}

// TestSearchPasteAtCursor guards that a paste inserts at the cursor position,
// not always at the end.
func TestSearchPasteAtCursor(t *testing.T) {
	m := searchModel(t)
	press(m, '/', "/")
	typeQuery(m, "27")
	press(m, tea.KeyLeft, "")
	m.PasteText("x")
	if m.search.query != "2x7" {
		t.Fatalf("paste at cursor = %q, want 2x7", m.search.query)
	}
	if m.search.pos != 2 {
		t.Fatalf("cursor after paste = %d, want 2", m.search.pos)
	}
}

// TestSearchPasteFallsThroughToShellWhenClosed guards that paste routing
// stays untouched once the search is closed — it must reach the shell again.
func TestSearchPasteFallsThroughToShellWhenClosed(t *testing.T) {
	m := searchModel(t)
	m.PasteText("hi")
	if !m.occupied {
		t.Fatal("paste without an open search must reach the shell")
	}
}
