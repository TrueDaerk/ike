package htmlpreview

// search.go gives the HTML preview the in-pane search every viewer has: "/"
// (and the shared find chord) opens a one-line prompt on the pane's last row
// and n / N walk the matching lines. The state and the prompt's behaviour are
// the shared ui.LineSearch (#2461); the match rule and the landing are the
// markdown preview's — smartcase over the plain text of each rendered line,
// a match landing a third down the viewport.

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// Searching reports whether the search prompt holds the keyboard (tests).
func (m *Model) Searching() bool { return m.search != nil && m.search.Open }

// SearchQuery returns the applied (or typed) query, "" with no search open.
func (m *Model) SearchQuery() string {
	if m.search == nil {
		return ""
	}
	return m.search.Text
}

// SearchMatches reports how many lines the current query matches (tests).
func (m *Model) SearchMatches() int {
	if m.search == nil {
		return 0
	}
	return len(m.search.Matches)
}

// OpenSearch implements the pane's Searchable capability: the shared find
// chord opens the same prompt "/" does.
func (m *Model) OpenSearch() bool {
	m.openSearch()
	return true
}

// NextMatch implements the pane's match-step capability (#2410): cmd+g walks
// the matches while the "/" prompt keeps the keyboard. With no search open
// the chord is not ours.
func (m *Model) NextMatch() ui.MatchStep { return m.stepSearch(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepSearch(-1) }

// LineInputOpen implements the pane's Searchable capability (#2634): the open
// search prompt owns the caret chords.
func (m *Model) LineInputOpen() bool { return m.Searching() }

// openSearch puts the cursor in the prompt, seeded with the last query.
func (m *Model) openSearch() {
	if m.search == nil {
		m.search = &ui.LineSearch{Cur: -1}
	}
	m.search.Start()
}

// closeSearch drops the search entirely.
func (m *Model) closeSearch() { m.search = nil }

// searchKey feeds one key to the open prompt: typing re-matches live, enter
// applies the query, esc abandons the search.
func (m *Model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	_, changed, action := m.search.Key(msg)
	switch action {
	case ui.SearchCancel:
		m.closeSearch()
	case ui.SearchAccept:
		m.recomputeMatches()
		if m.search.Apply(m.top) {
			m.scrollToMatch()
		}
	default:
		if changed {
			m.recomputeMatches()
		}
	}
	return nil
}

func (m *Model) stepSearch(delta int) ui.MatchStep {
	if m.search == nil {
		return ui.NoStep
	}
	return m.stepMatch(delta)
}

// stepMatch walks the matches by delta, wrapping at both ends.
func (m *Model) stepMatch(delta int) ui.MatchStep {
	st := m.search.Step(delta)
	m.scrollToMatch()
	return st
}

// scrollToMatch brings the current match a third down the viewport.
func (m *Model) scrollToMatch() {
	if line, ok := m.search.Current(); ok {
		m.scrollTo(line - m.h/3)
	}
}

// recomputeMatches rebuilds the match list with smartcase substring matching
// over the stripped line text.
func (m *Model) recomputeMatches() {
	s := m.search
	s.Recompute(len(m.doc.Lines), func(i int) bool {
		return ui.SmartCaseContains(s.Text, ansi.Strip(m.doc.Lines[i]))
	})
}

// searchLine renders the prompt row — the shared shape (#2461).
func (m Model) searchLine() string { return m.search.Line() }
