package diff

// search.go gives the diff viewer the in-pane search every other pane has
// (#2409): "/" (and the shared find chord) opens a one-line prompt on the
// pane's last row, the same shape the editor's find command line has, and the
// matches are walked with n / N.
//
// The search runs over the *diff rows* rather than the rendered lines: a row
// carries the raw left and right text, so a match is found regardless of the
// layout (side-by-side or unified), the horizontal scroll offset, or the
// styling wrapped around it. n / N keep their hunk meaning while no search is
// active — a diff without a query is still navigated by change, which is what
// the pane is for.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/ui"
)

// diffSearch is the open search: the query being typed or applied, the row
// indices it matches, and where in them the view currently sits.
type diffSearch struct {
	query string
	qpos  int // rune cursor inside query while input is open
	input bool

	matches []int // row indices, ascending
	cur     int   // index into matches, -1 before the first step
	miss    bool  // the applied query matched nothing
}

// Searching reports whether the search prompt holds the keyboard (tests).
func (m *Model) Searching() bool { return m.search != nil && m.search.input }

// SearchQuery returns the applied (or typed) query, "" with no search open.
func (m *Model) SearchQuery() string {
	if m.search == nil {
		return ""
	}
	return m.search.query
}

// SearchMatches reports how many rows the current query matches (tests).
func (m *Model) SearchMatches() int {
	if m.search == nil {
		return 0
	}
	return len(m.search.matches)
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
		m.search = &diffSearch{cur: -1}
	}
	m.search.input = true
	m.search.qpos = len([]rune(m.search.query))
}

// closeSearch drops the search entirely; n / N return to hunk navigation.
func (m *Model) closeSearch() { m.search = nil }

// searchKey feeds one key to the open prompt. enter applies the query and
// jumps to the first match at or after the current view; esc abandons the
// search and leaves the view where it was.
func (m *Model) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	s := m.search
	switch msg.String() {
	case "esc":
		m.closeSearch()
		return nil
	case "enter":
		s.input = false
		m.applySearch()
		return nil
	}
	text, cur, handled, changed := ui.EditKey(msg, s.query, s.qpos)
	if !handled {
		return nil
	}
	s.query, s.qpos = text, cur
	if changed {
		// Incremental like the editor's "/": the matches follow every
		// keystroke, so the counter in the prompt is honest while typing.
		m.recomputeMatches()
	}
	return nil
}

// applySearch jumps to the first match at or after the top of the view,
// wrapping to the first match when the view already sits past the last one.
func (m *Model) applySearch() {
	s := m.search
	m.recomputeMatches()
	if len(s.matches) == 0 {
		s.miss = s.query != ""
		return
	}
	s.miss = false
	s.cur = 0
	for i, row := range s.matches {
		if m.rowStart(row) >= m.top {
			s.cur = i
			break
		}
	}
	m.scrollToMatch()
}

// stepMatch walks the matches by delta, wrapping at both ends.
func (m *Model) stepMatch(delta int) {
	s := m.search
	if len(s.matches) == 0 {
		s.miss = s.query != ""
		return
	}
	s.miss = false
	n := len(s.matches)
	s.cur = ((s.cur+delta)%n + n) % n
	m.scrollToMatch()
}

// scrollToMatch brings the current match a third down the viewport, like the
// hunk steps do.
func (m *Model) scrollToMatch() {
	s := m.search
	if s.cur < 0 || s.cur >= len(s.matches) {
		return
	}
	m.scrollTo(m.rowStart(s.matches[s.cur]) - m.h/3)
}

// rowStart is a row's first visual line, 0 before the first render.
func (m *Model) rowStart(row int) int {
	if row < 0 || row >= len(m.rowStarts) {
		return 0
	}
	return m.rowStarts[row]
}

// recomputeMatches rebuilds the match list for the current query, keeping the
// current match on the nearest surviving row so incremental typing does not
// throw the view around.
func (m *Model) recomputeMatches() {
	s := m.search
	prev := -1
	if s.cur >= 0 && s.cur < len(s.matches) {
		prev = s.matches[s.cur]
	}
	s.matches = nil
	if s.query != "" {
		for i, r := range m.res.Rows {
			if searchHit(s.query, r.Left) || searchHit(s.query, r.Right) {
				s.matches = append(s.matches, i)
			}
		}
	}
	s.cur = -1
	for i, row := range s.matches {
		if row >= prev {
			s.cur = i
			break
		}
	}
	if s.cur < 0 && len(s.matches) > 0 {
		s.cur = 0
	}
	s.miss = s.query != "" && len(s.matches) == 0
}

// searchHit is the pane's match rule: smartcase substring, like the editor's
// "/" (#257) — an all-lowercase pattern folds case, any uppercase rune makes
// it exact.
func searchHit(pattern, text string) bool {
	if pattern == "" {
		return false
	}
	if pattern == strings.ToLower(pattern) {
		return strings.Contains(strings.ToLower(text), pattern)
	}
	return strings.Contains(text, pattern)
}

// searchLine renders the prompt row: the slash prefix, the query with its
// text cursor while typing, and the match counter — the shape the explorer's
// speed search and the response viewer's prompt already wear.
func (m Model) searchLine() string {
	s := m.search
	dim := lipgloss.NewStyle().Faint(true)
	line := "/"
	if s.input {
		line += ui.CursorView(s.query, s.qpos)
	} else {
		line += s.query
	}
	switch {
	case s.miss:
		line += dim.Render("  no matches")
	case len(s.matches) > 0:
		line += dim.Render("  " + strconv.Itoa(s.cur+1) + "/" + strconv.Itoa(len(s.matches)))
	}
	return line
}
