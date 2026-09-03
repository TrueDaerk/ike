package diff

// search.go gives the diff viewer the in-pane search every other pane has
// (#2409): "/" (and the shared find chord) opens a one-line prompt on the
// pane's last row, the same shape the editor's find command line has, and the
// matches are walked with n / N.
//
// The state and the prompt's behaviour are the shared ui.LineSearch (#2461);
// what is the diff viewer's own is the match rule and the landing. The search
// runs over the *diff rows* rather than the rendered lines: a row carries the
// raw left and right text, so a match is found regardless of the layout
// (side-by-side or unified), the horizontal scroll offset, or the styling
// wrapped around it. n / N keep their hunk meaning while no search is active
// — a diff without a query is still navigated by change, which is what the
// pane is for.

import (
	tea "charm.land/bubbletea/v2"

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

// SearchMatches reports how many rows the current query matches (tests).
func (m *Model) SearchMatches() int {
	if m.search == nil {
		return 0
	}
	return len(m.search.Matches)
}

// OpenSearch implements the pane's Searchable capability (#2409): the shared
// find chord opens the same prompt "/" does. It always opens — an empty diff
// still gets its prompt, and answers with "no matches".
func (m *Model) OpenSearch() bool {
	m.openSearch()
	return true
}

// openSearch puts the cursor in the prompt, seeded with the last query so
// refining a search is an edit rather than a retype.
func (m *Model) openSearch() {
	if m.search == nil {
		m.search = &ui.LineSearch{Cur: -1}
	}
	m.search.Start()
}

// closeSearch drops the search entirely; n / N return to hunk navigation.
func (m *Model) closeSearch() { m.search = nil }

// searchKey feeds one key to the open prompt. Typing re-matches live; enter
// applies the query and jumps to the first match at or after the current
// view; esc abandons the search and leaves the view where it was.
func (m *Model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	_, changed, action := m.search.Key(msg)
	switch action {
	case ui.SearchCancel:
		m.closeSearch()
	case ui.SearchAccept:
		m.applySearch()
	default:
		if changed {
			m.recomputeMatches()
		}
	}
	return nil
}

// applySearch jumps to the first match at or after the top of the view,
// wrapping to the first match when the view already sits past the last one.
func (m *Model) applySearch() {
	m.recomputeMatches()
	// The matches are rows and the view top a visual line: the row the top
	// sits on is the landing rule's position.
	top := len(m.res.Rows)
	for r := range m.res.Rows {
		if m.rowStart(r) >= m.top {
			top = r
			break
		}
	}
	if m.search.Apply(top) {
		m.scrollToMatch()
	}
}

// stepMatch walks the matches by delta, wrapping at both ends and reporting
// where it landed so the shared match-step chord can say so (#2410).
func (m *Model) stepMatch(delta int) ui.MatchStep {
	st := m.search.Step(delta)
	m.scrollToMatch()
	return st
}

// NextMatch implements the pane's match-step capability (#2410): cmd+g walks
// the matches while the "/" prompt keeps the keyboard, so a query can be
// refined and stepped through without enter applying it first. With no search
// open the chord is not ours, and the root model falls back to what it meant
// before.
func (m *Model) NextMatch() ui.MatchStep { return m.stepSearch(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepSearch(-1) }

func (m *Model) stepSearch(delta int) ui.MatchStep {
	if m.search == nil {
		return ui.NoStep
	}
	return m.stepMatch(delta)
}

// scrollToMatch brings the current match a third down the viewport, like the
// hunk steps do.
func (m *Model) scrollToMatch() {
	row, ok := m.search.Current()
	if !ok {
		return
	}
	m.scrollTo(m.rowStart(row) - m.h/3)
}

// rowStart is a row's first visual line, 0 before the first render.
func (m *Model) rowStart(row int) int {
	if row < 0 || row >= len(m.rowStarts) {
		return 0
	}
	return m.rowStarts[row]
}

// recomputeMatches rebuilds the match list for the current query with the
// pane's match rule: smartcase substring (ui.SmartCaseContains) over a row's
// left or right text.
func (m *Model) recomputeMatches() {
	s := m.search
	s.Recompute(len(m.res.Rows), func(i int) bool {
		r := m.res.Rows[i]
		return ui.SmartCaseContains(s.Text, r.Left) || ui.SmartCaseContains(s.Text, r.Right)
	})
}

// searchLine renders the prompt row — the shared shape (#2461): the slash
// prefix, the query with its text cursor while typing, and the match counter.
func (m Model) searchLine() string { return m.search.Line() }
