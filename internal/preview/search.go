package preview

// search.go gives the markdown preview the in-pane search every other viewer
// has (#2409): "/" (and the shared find chord) opens a one-line prompt on the
// pane's last row and n / N walk the matching lines.
//
// The state and the prompt's behaviour are the shared ui.LineSearch (#2461);
// what is the preview's own is the match rule and the landing. The match runs
// over the *plain text* of each rendered line — the styling glamour wrapped
// around it is stripped first, so a word split by a colour escape still
// matches what the reader sees. The preview is read-only and has no cursor,
// so a match is expressed as a scroll: the matching line comes to rest a
// third down the viewport, the same landing the diff viewer uses.

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

// OpenSearch implements the pane's Searchable capability (#2409): the shared
// find chord opens the same prompt "/" does.
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

// closeSearch drops the search entirely.
func (m *Model) closeSearch() { m.search = nil }

// searchKey feeds one key to the open prompt: typing re-matches live, enter
// applies the query, esc abandons the search and leaves the view where it
// was.
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
// wrapping to the first when the view already sits past the last one.
func (m *Model) applySearch() {
	m.recomputeMatches()
	if m.search.Apply(m.top) {
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
// the matches while the "/" prompt keeps the keyboard, so the query stays
// editable between steps. With no search open the chord is not ours.
func (m *Model) NextMatch() ui.MatchStep { return m.stepSearch(1) }

// PrevMatch steps backwards; see NextMatch.
func (m *Model) PrevMatch() ui.MatchStep { return m.stepSearch(-1) }

func (m *Model) stepSearch(delta int) ui.MatchStep {
	if m.search == nil {
		return ui.NoStep
	}
	return m.stepMatch(delta)
}

// scrollToMatch brings the current match a third down the viewport.
func (m *Model) scrollToMatch() {
	line, ok := m.search.Current()
	if !ok {
		return
	}
	m.scrollTo(line - m.h/3)
}

// recomputeMatches rebuilds the match list for the current query with the
// pane's match rule: smartcase substring (ui.SmartCaseContains) over the
// stripped line text.
func (m *Model) recomputeMatches() {
	s := m.search
	s.Recompute(len(m.lines), func(i int) bool {
		return ui.SmartCaseContains(s.Text, ansi.Strip(m.lines[i]))
	})
}

// searchLine renders the prompt row — the shared shape (#2461): the slash
// prefix, the query with its text cursor while typing, and the match counter.
func (m Model) searchLine() string { return m.search.Line() }
