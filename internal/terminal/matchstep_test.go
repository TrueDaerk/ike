package terminal

// matchstep_test.go covers #2410 for the terminal: cmd+g steps whichever
// search the pane has open — the scrollback field or copy mode's — without
// closing it. The pipe-backed model (#1370) gives deterministic content.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stepChord is the match-step chord as a key event; back flips the direction.
func stepChord(back bool) tea.KeyPressMsg {
	mod := tea.ModMeta
	if back {
		mod |= tea.ModShift
	}
	return tea.KeyPressMsg{Code: 'g', Mod: mod}
}

// TestScrollbackSearchMatchStepChord: the field owns the keyboard, so it
// answers the chord itself and keeps the query.
func TestScrollbackSearchMatchStepChord(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(5)
	if !m.StartSearch() {
		t.Fatal("StartSearch must open the scrollback field")
	}
	for _, r := range "line1" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	at := m.search.cur
	m.Update(stepChord(false))
	if m.search.cur == at {
		t.Fatalf("cmd+g must step the match, cur stayed at %d", at)
	}
	if !m.Searching() || m.search.query != "line1" {
		t.Fatalf("the field must survive the step: open=%v query=%q",
			m.Searching(), m.search.query)
	}
}

// TestScrollbackSearchMatchStepWraps: past the last match the walk comes back
// around, and the field's counter says so.
func TestScrollbackSearchMatchStepWraps(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(5)
	m.StartSearch()
	for _, r := range "foo" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	st := m.NextMatch() // the only match: any step wraps onto it
	if !st.Handled || st.Total != 1 || !st.Wrapped {
		t.Fatalf("the wrap = %+v", st)
	}
	if got := m.searchLine(); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the search line must mark the wrap: %q", got)
	}
}

func TestScrollbackSearchMatchStepWithNoMatches(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(5)
	m.StartSearch()
	for _, r := range "zzzz" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if st := m.NextMatch(); !st.Handled || st.Total != 0 {
		t.Fatalf("NextMatch over nothing = %+v", st)
	}
}

func TestTerminalMatchStepWithoutASearch(t *testing.T) {
	m := copyModel(t)
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with nothing open = %+v, want not handled", st)
	}
}

// TestCopyModeMatchStepChord: in copy mode n / N step the accepted search,
// but they are query text while the "/" line is up — which is the whole point
// of the chord (#2410).
func TestCopyModeMatchStepChord(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(5)
	if !m.StartCopyMode() {
		t.Fatal("StartCopyMode must open on a pipe session")
	}
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "line1" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	at := m.copy.cur.line
	m.Update(stepChord(false))
	if m.copy.cur.line == at {
		t.Fatalf("cmd+g must step the copy-mode search, line stayed at %d", at)
	}
	if !m.Copying() || m.copy.last != "line1" {
		t.Fatalf("copy mode must survive the step: copying=%v last=%q",
			m.Copying(), m.copy.last)
	}
	// Backwards returns to where the walk started.
	m.Update(stepChord(true))
	if m.copy.cur.line != at {
		t.Fatalf("cmd+shift+g must step back, line = %d want %d", m.copy.cur.line, at)
	}
}

// TestCopyModeMatchStepWithTheQueryLineOpen: the chord reaches the accepted
// search even while a new query is being typed.
func TestCopyModeMatchStepWithTheQueryLineOpen(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(5)
	m.StartCopyMode()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "line1" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// A second query line, half typed: n would be text here.
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	at := m.copy.cur.line
	m.Update(stepChord(false))
	if m.copy.cur.line == at {
		t.Fatal("the chord must step the accepted search with the query line open")
	}
	if !m.copy.input || m.copy.query != "l" {
		t.Fatalf("the query line must survive: input=%v query=%q", m.copy.input, m.copy.query)
	}
}

// TestCopyModeMatchStepWrapMarksTheStatusRow.
func TestCopyModeMatchStepWrapMarksTheStatusRow(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(5)
	m.StartCopyMode()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "foo" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	st := m.NextMatch() // the only match: the step wraps onto it
	if !st.Handled || st.Total != 1 || !st.Wrapped {
		t.Fatalf("the wrap = %+v", st)
	}
	if got := m.copyStatusLine(); !strings.Contains(got, "(wrapped)") {
		t.Fatalf("the copy-mode status row must mark the wrap: %q", got)
	}
}

// TestCopyModeMatchStepWithoutAnAcceptedQuery: nothing accepted, nothing ours.
func TestCopyModeMatchStepWithoutAnAcceptedQuery(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	if st := m.NextMatch(); st.Handled {
		t.Fatalf("NextMatch with no accepted query = %+v, want not handled", st)
	}
}
