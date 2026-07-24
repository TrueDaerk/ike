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
