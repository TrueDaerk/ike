package dataview

// filter.go is the grid's SQL filter (#1777). `/` in the grid opens a one-line
// field holding everything that follows `SELECT * FROM <table>` — the fixed
// head is shown dimmed in front of it, so what the clause completes is never a
// guess. Enter applies it, esc drops it and returns the whole table.
//
// The head carries the `WHERE` too (#1885): a filter is almost always a
// condition on the current table, so typing one is what the line is for and
// `WHERE ` is not worth retyping. The keyword stays available — a line that
// opens with a clause keyword of its own (`ORDER BY id DESC LIMIT 10`, or an
// explicit `WHERE`) is passed to the backend untouched and the dimmed `WHERE `
// disappears from the head, so the query shown is always the query run.
//
// The pane still holds no SQL of its own: it hands the clause to the backend's
// PageWhere, which wraps it and keeps the read-only contract. Paging keeps
// working immediately; the filtered result's size is counted in the background
// like a table's (#1795), so the status line and the last-page jump pick it up
// when it lands instead of the apply waiting for a scan.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
	"ike/internal/theme"
	"ike/internal/ui"
)

// Filter returns the applied clause, "" when the grid is unfiltered (tests).
func (m *Model) Filter() string { return m.filter }

// Filtering reports whether the filter line is open (tests).
func (m *Model) Filtering() bool { return m.fEditing }

// FilterInput returns the text in the open filter line (tests).
func (m *Model) FilterInput() string { return m.fInput }

// FilterErr returns the engine error of the last rejected clause, nil when
// the last apply succeeded (tests).
func (m *Model) FilterErr() error { return m.fErr }

// startFilter opens the filter line, seeded with the applied clause so
// refining a filter is an edit rather than a retype.
func (m *Model) startFilter() {
	if m.src == nil || m.sel < 0 {
		return
	}
	m.fEditing = true
	m.fInput = conditionOf(m.filter)
	m.fCur = len([]rune(m.fInput))
	m.fErr = nil
	m.clampScroll()
}

// OpenSearch implements the pane's Searchable capability (#2409): the shared
// find chord opens the same filter line "/" does. It reports false with no
// table selected — there is nothing to filter then, and the root model says
// so rather than swallowing the chord.
func (m *Model) OpenSearch() bool {
	m.startFilter()
	return m.fEditing
}

// filterKey feeds one key to the open filter line. The line owns the keyboard
// while it is open — the grid's single-letter keys (j/k/n/p/s) are plain text
// here — and only enter and esc close it.
func (m *Model) filterKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		// Esc is "show me the whole table again": it closes the line *and*
		// drops the filter, the one gesture that always gets the plain grid
		// back.
		m.fEditing, m.fInput, m.fCur, m.fErr = false, "", 0, nil
		m.clearFilter()
	case "enter":
		m.applyFilter()
	default:
		if out, ncur, handled, changed := ui.EditKey(msg, m.fInput, m.fCur); handled {
			m.fInput, m.fCur = out, ncur
			if changed {
				m.fErr = nil // the error described the text that is now gone
			}
		}
	}
}

// applyFilter runs the typed clause. A clause the engine rejects — a syntax
// error, a `;` the backend refuses — leaves the grid exactly as it was and
// keeps the line open with the engine's message; nothing is written either
// way, the backends being read-only.
func (m *Model) applyFilter() {
	if m.src == nil || m.sel < 0 {
		return
	}
	if strings.TrimSpace(m.fInput) == "" {
		m.fEditing, m.fErr = false, nil
		m.clearFilter()
		return
	}
	clause := clauseOf(m.fInput)
	page, err := m.src.PageWhere(m.tables[m.sel].Name, clause, m.sort, 0, PageSize)
	if err != nil {
		m.fErr = err
		return
	}
	m.filter, m.page, m.pageErr = clause, page, nil
	// The filtered result has a size of its own, counted in the background
	// under its own cache key (#1795) — until it lands the status line says
	// "?" rather than stalling the apply on a scan.
	m.recordPageTotal()
	m.adoptTotal()
	m.rowCur, m.rowTop, m.colOff = 0, 0, 0
	m.fEditing, m.fErr = false, nil
	m.clampScroll()
}

// clearFilter drops the filter and reloads the table's first page.
func (m *Model) clearFilter() {
	if m.filter == "" {
		m.clampScroll()
		return
	}
	m.filter = ""
	m.rowCur, m.rowTop, m.colOff = 0, 0, 0
	m.fetch(0)
}

// clauseKeywords open a clause of their own: a line starting with one of them
// completes the `SELECT * FROM <table>` head by itself and gets no `WHERE`.
var clauseKeywords = []string{"where", "order by", "group by", "having", "limit", "offset", "window", "union", "except", "intersect"}

// impliedWhere reports whether the typed text is a bare condition — the usual
// case — and so needs the implicit `WHERE` in front of it (#1885).
func impliedWhere(input string) bool {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return false
	}
	// `order  by` is one keyword however it is spaced, so compare on words.
	words := strings.Join(strings.Fields(s), " ")
	for _, kw := range clauseKeywords {
		if words == kw || strings.HasPrefix(words, kw+" ") {
			return false
		}
	}
	return true
}

// clauseOf is the clause the backend runs for the typed text: the condition
// with its implicit `WHERE`, or the text itself when it opens a clause.
func clauseOf(input string) string {
	s := strings.TrimSpace(input)
	if impliedWhere(s) {
		return "WHERE " + s
	}
	return s
}

// conditionOf is clauseOf's inverse for seeding the line: an applied clause is
// shown the way it was typed, without the `WHERE` the head now carries.
func conditionOf(clause string) string {
	s := strings.TrimSpace(clause)
	rest, ok := cutFold(s, "where")
	if !ok || !impliedWhere(rest) {
		return s
	}
	return rest
}

// cutFold strips a leading keyword plus its separating space, case-insensitive.
func cutFold(s, kw string) (string, bool) {
	if len(s) <= len(kw) || !strings.EqualFold(s[:len(kw)], kw) {
		return s, false
	}
	rest := s[len(kw):]
	if trimmed := strings.TrimLeft(rest, " \t"); trimmed != rest {
		return trimmed, true
	}
	return s, false
}

// filterPrefix is the fixed query head the line shows before the clause. It
// comes from the backend, so the table is quoted the way its engine quotes it
// (and a Parquet file names its `read_parquet(…)` call instead) — plus the
// implicit `WHERE ` whenever the typed text is a bare condition (#1885).
func (m *Model) filterPrefix() string {
	base := "SELECT * FROM "
	if m.src != nil && m.sel >= 0 {
		base = m.src.FilterPrefix(m.tables[m.sel].Name)
	}
	if m.fInput == "" || impliedWhere(m.fInput) {
		return base + "WHERE "
	}
	return base
}

// filterLine renders the open field: the dimmed prefix, then the clause
// SQL-highlighted with a block cursor in it. On a narrow pane the prefix is
// clipped first — it is context, the clause is the content.
func (m *Model) filterLine(pal *theme.Palette) string {
	prefix := m.filterPrefix()
	shown := prefix
	if half := m.w / 2; lipgloss.Width(shown) > half {
		shown = clipTo(shown, half)
	}
	avail := m.w - lipgloss.Width(shown)
	if avail < 4 {
		avail = 4
	}
	return lipgloss.NewStyle().Faint(true).Render(shown) + m.renderClause(pal, prefix, avail)
}

// renderClause draws the clause with SQL highlighting and the cursor. The
// whole statement is highlighted, prefix included — `WHERE x = 1` is not a
// statement, and the grammar only makes sense of the clause with its head
// attached — and the prefix's columns are then skipped.
func (m *Model) renderClause(pal *theme.Palette, prefix string, avail int) string {
	r := []rune(m.fInput)
	cur := m.fCur
	if cur < 0 {
		cur = 0
	}
	if cur > len(r) {
		cur = len(r)
	}
	ix := highlight.NewIndex(highlight.HighlightFenced("sql", []string{prefix + m.fInput}))
	off := len([]rune(prefix))
	// The cursor stays visible: a clause longer than the line scrolls with it.
	start := 0
	if cur >= avail {
		start = cur - avail + 1
	}
	var b strings.Builder
	drawn := 0
	for i := start; i < len(r) && drawn < avail; i++ {
		st := m.captureStyle(pal, ix, off+i)
		if i == cur {
			st = st.Reverse(true)
		}
		b.WriteString(st.Render(string(r[i])))
		drawn++
	}
	if cur >= len(r) && drawn < avail {
		b.WriteString(lipgloss.NewStyle().Reverse(true).Render(" "))
	}
	return b.String()
}

// captureStyle resolves one column's capture to a theme style, falling back to
// plain foreground for uncaptured text and for builds without the SQL grammar
// (CGo-free: HighlightFenced yields nothing and the clause renders plain).
func (m *Model) captureStyle(pal *theme.Palette, ix highlight.Index, col int) lipgloss.Style {
	if st, ok := m.hl.Style(ix.CaptureAt(0, col)); ok {
		return st
	}
	return lipgloss.NewStyle().Foreground(pal.Foreground)
}

// filterNote is the header's active-filter marker: the clause is visible even
// while the line is closed, so a filtered grid never reads as the whole table.
// The column sort (#2248) joins it for the same reason — the grid's arrow is
// on a column that may be scrolled out of sight.
func (m *Model) filterNote(pal *theme.Palette, width int) string {
	var parts []string
	if m.filter != "" {
		parts = append(parts, "filter: "+m.filter)
	}
	if m.sort.Active() {
		parts = append(parts, "sort: "+m.sort.String())
	}
	if len(parts) == 0 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(pal.Warning).Render(clipTo("   "+strings.Join(parts, " · "), width))
}

// rebuildTheme re-resolves the capture styles of the filter line from the
// active palette.
func (m *Model) rebuildTheme() {
	var captures map[string]string
	if m.pal != nil {
		captures = m.pal.Captures
	}
	m.hl = highlight.NewTheme(captures, nil)
}

// PasteText inserts a pasted block into the open filter line at its cursor
// (#2002); it reports whether the line consumed it, so a closed line lets the
// paste fall through.
func (m *Model) PasteText(text string) bool {
	// The export line (#2248) takes a path, which is exactly the kind of text
	// one pastes rather than types, so it consumes a paste the same way.
	if m.exp != nil {
		if m.exp.cancel != nil {
			return false // the path is settled while its export runs
		}
		out, ncur, changed := ui.PasteText(m.exp.input, m.exp.cur, text)
		if !changed {
			return false
		}
		m.exp.input, m.exp.cur = out, ncur
		m.exp.err, m.exp.overwrite = nil, false
		return true
	}
	if !m.fEditing {
		return false
	}
	out, ncur, changed := ui.PasteText(m.fInput, m.fCur, text)
	if !changed {
		return false
	}
	m.fInput, m.fCur = out, ncur
	m.fErr = nil
	return true
}
