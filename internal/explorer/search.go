package explorer

// search.go implements the explorer's type-to-select speed search (#1087):
// "/" with the tree focused opens a one-line search field on the pane's last
// row (the same region the scan-error banner uses, #1030 — never a modal box),
// and typing filters incrementally: the cursor jumps to the best visible row
// whose NAME contains the query, case-insensitively, with prefix matches
// ranked first. Matching is over the currently visible rows only — the search
// never auto-expands directories.
//
// While the search is open it owns the keyboard: the root model routes every
// key straight to the explorer (explorerCapturing, the same capture path the
// file-op prompt uses), so the single-letter file-op keys (a/A/d/R/r/c/C/o/.)
// cannot fire mid-query. enter accepts (cursor stays, search closes), esc
// cancels (cursor returns to where the search started), backspace edits,
// ctrl+n / down step to the next match and ctrl+p / up to the previous one,
// both wrapping. Every other key is consumed without effect — no silent
// passthrough while the field is visible.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// SearchMsg opens the speed-search field (explorer.search, "/").
type SearchMsg struct{}

func (SearchMsg) explorerMsg() {}

// searchState is the open speed search: the query typed so far and the cursor
// row at activation, restored on cancel. It lives behind a pointer so the
// value-receiver Update/View copies share it, mirroring prompt.
type searchState struct {
	query string
	prev  int // cursor row when the search opened; esc returns here
}

// Searching reports whether the speed search is open, so the root model can
// route raw keys straight to the explorer — the same capture the file-op
// prompt gets via Prompting — instead of resolving them in the keymap layer.
func (m Model) Searching() bool { return m.search != nil }

// startSearch opens the speed search on the current cursor position. Any
// armed gg chord and multi-select range are dropped: the search owns the
// keyboard from here.
func (m *Model) startSearch() {
	m.pendingG = false
	m.clearSel()
	m.search = &searchState{prev: m.cursor}
}

// handleSearchKey feeds one key to the open speed search. Every key is
// consumed while the field is open; only enter/esc close it.
func (m *Model) handleSearchKey(msg tea.KeyPressMsg) {
	s := m.search
	switch {
	case msg.Code == tea.KeyEscape:
		// Cancel: the cursor returns to where the search started (clamped —
		// an async rescan may have shrunk the rows meanwhile).
		m.search = nil
		if len(m.rows) > 0 {
			m.cursor = clamp(s.prev, 0, len(m.rows)-1)
			m.clampScroll()
		}
	case msg.Code == tea.KeyEnter:
		m.search = nil // accept: the cursor stays on the match
	case msg.Code == tea.KeyBackspace:
		if r := []rune(s.query); len(r) > 0 {
			s.query = string(r[:len(r)-1])
		}
		m.searchJump()
	case msg.String() == "ctrl+n" || msg.Code == tea.KeyDown:
		m.searchStep(1)
	case msg.String() == "ctrl+p" || msg.Code == tea.KeyUp:
		m.searchStep(-1)
	case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		s.query += msg.Text
		m.searchJump()
	}
}

// searchJump re-resolves the cursor after a query edit, always from the
// stable anchor (the row the search opened on), so growing and shrinking the
// query is deterministic: scanning forward from the anchor with wrap-around,
// the first row whose name STARTS with the query wins; when no row does, the
// first row whose name merely contains it is taken instead (prefix matches
// rank first). No match leaves the cursor put — the footer shows the miss.
// An empty query returns the cursor to the anchor.
func (m *Model) searchJump() {
	s := m.search
	if s == nil || len(m.rows) == 0 {
		return
	}
	if s.query == "" {
		m.cursor = clamp(s.prev, 0, len(m.rows)-1)
		m.clampScroll()
		return
	}
	q := strings.ToLower(s.query)
	start := clamp(s.prev, 0, len(m.rows)-1)
	contains := -1
	for i := 0; i < len(m.rows); i++ {
		idx := (start + i) % len(m.rows)
		name := strings.ToLower(m.rows[idx].name)
		if strings.HasPrefix(name, q) {
			m.cursor = idx
			m.clampScroll()
			return
		}
		if contains < 0 && strings.Contains(name, q) {
			contains = idx
		}
	}
	if contains >= 0 {
		m.cursor = contains
		m.clampScroll()
	}
}

// searchStep moves to the next (dir > 0) or previous match relative to the
// cursor, wrapping around the row set.
func (m *Model) searchStep(dir int) {
	matches := m.searchMatches()
	if len(matches) == 0 {
		return
	}
	if dir > 0 {
		for _, idx := range matches {
			if idx > m.cursor {
				m.cursor = idx
				m.clampScroll()
				return
			}
		}
		m.cursor = matches[0] // wrap to the first match
	} else {
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i] < m.cursor {
				m.cursor = matches[i]
				m.clampScroll()
				return
			}
		}
		m.cursor = matches[len(matches)-1] // wrap to the last match
	}
	m.clampScroll()
}

// searchMatches returns the visible-row indices whose names contain the
// query, case-insensitively, in tree order. Empty without an open search or
// with an empty query.
func (m Model) searchMatches() []int {
	s := m.search
	if s == nil || s.query == "" {
		return nil
	}
	q := strings.ToLower(s.query)
	var out []int
	for i, n := range m.rows {
		if strings.Contains(strings.ToLower(n.name), q) {
			out = append(out, i)
		}
	}
	return out
}

// searchMatchRange returns the byte range of the first case-insensitive
// occurrence of query in name, for the substring highlight. ok is false when
// there is no match — or when lowercasing changed the byte length (non-ASCII
// edge case), where mapped offsets would be unreliable; the row then simply
// skips the substring styling.
func searchMatchRange(name, query string) (start, end int, ok bool) {
	ln := strings.ToLower(name)
	if len(ln) != len(name) {
		return 0, 0, false
	}
	idx := strings.Index(ln, strings.ToLower(query))
	if idx < 0 {
		return 0, 0, false
	}
	return idx, idx + len(strings.ToLower(query)), true
}

// searchLine renders the speed-search input for the pane's last row,
// mirroring the editor's "/" command line: the slash prefix, the query with a
// block cursor at its end (ui.CursorView), and a dim match counter ("3/17",
// or "no matches" in the Error colour). Truncated to the pane width like
// every footer line.
func (m Model) searchLine() string {
	s := m.search
	pal := m.theme()
	counter := ""
	line := "/" + ui.CursorView(s.query, len([]rune(s.query)))
	if s.query != "" {
		matches := m.searchMatches()
		dim := lipgloss.NewStyle().Foreground(pal.InlayHint)
		if len(matches) == 0 {
			counter = lipgloss.NewStyle().Foreground(pal.Error).Render("  no matches")
		} else {
			cur := 0
			for i, idx := range matches {
				if idx == m.cursor {
					cur = i + 1
					break
				}
			}
			counter = dim.Render("  " + strconv.Itoa(cur) + "/" + strconv.Itoa(len(matches)))
		}
	}
	return ansi.Truncate(line+counter, maxz(m.width), "…")
}
