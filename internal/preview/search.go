package preview

// search.go gives the markdown preview the in-pane search every other viewer
// has (#2409): "/" (and the shared find chord) opens a one-line prompt on the
// pane's last row and n / N walk the matching lines.
//
// The match runs over the *plain text* of each rendered line — the styling
// glamour wrapped around it is stripped first, so a word split by a colour
// escape still matches what the reader sees. The preview is read-only and has
// no cursor, so a match is expressed as a scroll: the matching line comes to
// rest a third down the viewport, the same landing the diff viewer uses.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// previewSearch is the open search: the query being typed or applied, the
// rendered line indices it matches, and where in them the view sits.
type previewSearch struct {
	query string
	qpos  int // rune cursor inside query while input is open
	input bool

	matches []int // rendered line indices, ascending
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

// SearchMatches reports how many lines the current query matches (tests).
func (m *Model) SearchMatches() int {
	if m.search == nil {
		return 0
	}
	return len(m.search.matches)
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
		m.search = &previewSearch{cur: -1}
	}
	m.search.input = true
	m.search.qpos = len([]rune(m.search.query))
}

// closeSearch drops the search entirely.
func (m *Model) closeSearch() { m.search = nil }

// searchKey feeds one key to the open prompt: enter applies the query, esc
// abandons the search and leaves the view where it was.
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
		m.recomputeMatches()
	}
	return nil
}

// applySearch jumps to the first match at or after the top of the view,
// wrapping to the first when the view already sits past the last one.
func (m *Model) applySearch() {
	s := m.search
	m.recomputeMatches()
	if len(s.matches) == 0 {
		s.miss = s.query != ""
		return
	}
	s.miss = false
	s.cur = 0
	for i, line := range s.matches {
		if line >= m.top {
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

// scrollToMatch brings the current match a third down the viewport.
func (m *Model) scrollToMatch() {
	s := m.search
	if s.cur < 0 || s.cur >= len(s.matches) {
		return
	}
	m.scrollTo(s.matches[s.cur] - m.h/3)
}

// recomputeMatches rebuilds the match list for the current query, keeping the
// current match on the nearest surviving line so incremental typing does not
// throw the view around.
func (m *Model) recomputeMatches() {
	s := m.search
	prev := -1
	if s.cur >= 0 && s.cur < len(s.matches) {
		prev = s.matches[s.cur]
	}
	s.matches = nil
	if s.query != "" {
		for i, line := range m.lines {
			if searchHit(s.query, ansi.Strip(line)) {
				s.matches = append(s.matches, i)
			}
		}
	}
	s.cur = -1
	for i, line := range s.matches {
		if line >= prev {
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
// text cursor while typing, and the match counter.
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
