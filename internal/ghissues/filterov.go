package ghissues

// filterov.go is the unified filter overlay (#2104): one modal combining the
// fuzzy match text, the state gate, the sort order, the grouping and the label
// selection that used to live behind disconnected keys. 'f' opens it on the
// match input, 'l' opens it scrolled to the label section, and 't'/'a'/'g' on
// the list remain one-key accelerators into the same state — three doors into
// one room. Every change applies to the list live; enter keeps, esc restores
// what the overlay opened with. The active narrowing renders as chips in the
// permanent filter row (view.go), each clearable by a click (mouse.go) or
// peeled one at a time by esc on the list (clearFilters).

import (
	"sort"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/ui"
)

// The fixed rows above the label section: the match input, the state radio,
// the sort cycle, the grouping toggle (the issue tab only) and the saved
// filter cycle (#2115, only when issues.saved_filters names any). They are
// row *kinds*, not indices — which rows the overlay has depends on the tab
// and the config, so fovRows resolves the order and fovKind reads it back.
// Only fovMatch is also an index: the match input is always the first row.
const (
	fovMatch = 0
	fovState = 1
	fovSort  = 2
	fovGroup = 3
	fovSaved = 4
)

// filterSnapshot is what esc restores: every dimension the overlay edits.
type filterSnapshot struct {
	input  string
	cur    int
	labels map[string]bool
	state  StateFilter
	sort   SortOrder
	group  bool
}

// snapshotFilters copies the live filter state for the overlay's esc-revert.
func (m *Model) snapshotFilters() filterSnapshot {
	labels := map[string]bool{}
	for k, v := range m.labelSel {
		labels[k] = v
	}
	return filterSnapshot{input: m.fInput, cur: m.fCur, labels: labels,
		state: m.state, sort: m.sort, group: m.group}
}

// fovRows is the overlay's non-label rows in render order for the active tab:
// the PR view has no label filter and no grouping, and the saved row only
// exists once issues.saved_filters names a filter to pick.
func (m *Model) fovRows() []int {
	rows := []int{fovMatch, fovState, fovSort}
	if m.tab != TabPRs {
		rows = append(rows, fovGroup)
	}
	if len(m.saved) > 0 {
		rows = append(rows, fovSaved)
	}
	return rows
}

// fovFixedRows is how many non-label rows the overlay has on the active tab.
func (m *Model) fovFixedRows() int { return len(m.fovRows()) }

// fovKind names the fixed row at index i, -1 when i is not one (the label
// section, or a cursor the caller has not clamped yet).
func (m *Model) fovKind(i int) int {
	rows := m.fovRows()
	if i < 0 || i >= len(rows) {
		return -1
	}
	return rows[i]
}

// filterLabels is the label section's row set: the repository's labels (the
// mutation pickers' source, falling back to the listing) plus any selected
// name the repository no longer reports, so an active chip is always visible
// and clearable. Sorted by name.
func (m *Model) filterLabels() []forge.Label {
	seen := map[string]bool{}
	var out []forge.Label
	for _, l := range m.pickerLabels() {
		if !seen[l.Name] {
			seen[l.Name] = true
			out = append(out, l)
		}
	}
	// The cached distinct names keep the section populated while a
	// state-change refetch has the listing dropped (#2107).
	for _, name := range m.labels {
		if !seen[name] {
			seen[name] = true
			out = append(out, forge.Label{Name: name})
		}
	}
	for name, on := range m.labelSel {
		if on && !seen[name] {
			out = append(out, forge.Label{Name: name})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// filterViewLabels is what the label section renders and navigates:
// filterLabels() narrowed by the section's type-ahead (#2111). The fixed rows
// above it are never narrowed — they are toggles, not a list.
func (m *Model) filterViewLabels() []forge.Label {
	return ui.Narrow(&m.ovSearch, m.filterLabels(), func(l forge.Label) string { return l.Name })
}

// FilterVisibleLabels lists the label section's currently visible names
// (tests).
func (m *Model) FilterVisibleLabels() []string {
	labels := m.filterViewLabels()
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, l.Name)
	}
	return out
}

// openFilterOverlay opens the unified overlay with the cursor on row, taking
// the snapshot esc restores.
func (m *Model) openFilterOverlay(row int) {
	m.fSaved = m.snapshotFilters()
	m.ov, m.ovTop = ovFilter, 0
	m.ovSearch.Reset()
	m.ovCursor = row
	if row == fovMatch {
		m.fCur = len([]rune(m.fInput))
	}
	m.clampOverlay()
}

// openLabelSection opens the overlay on the label section ('l'), landing on
// the first selected label so the active filter reads first.
func (m *Model) openLabelSection() {
	if m.tab == TabPRs {
		m.openFilterOverlay(fovMatch)
		return
	}
	row := m.fovFixedRows()
	for i, l := range m.filterLabels() {
		if m.labelSel[l.Name] {
			row += i
			break
		}
	}
	m.openFilterOverlay(row)
}

// filterOvKey routes one key inside the open filter overlay. The match row
// owns every printable key; the other rows are toggles and cycles.
func (m *Model) filterOvKey(msg tea.KeyPressMsg) tea.Cmd {
	// While the label section's type-ahead runs it owns the overlay (#2111):
	// the cursor stays inside the narrowed labels and every printable key
	// feeds the query, so a label named "state" is reachable even though the
	// rows above are called state and sort.
	if m.ovSearch.Active() {
		return m.searchingFilterKey(msg)
	}
	n := m.fovFixedRows() + m.fovLabelRows()
	switch msg.String() {
	case "enter":
		m.closeOverlay()
		return nil
	case "esc":
		return m.revertFilters()
	case "down", "tab", "ctrl+n":
		m.ovCursor = (m.ovCursor + 1) % n
		m.clampOverlay()
		return nil
	case "up", "shift+tab", "ctrl+p":
		m.ovCursor = (m.ovCursor - 1 + n) % n
		m.clampOverlay()
		return nil
	}
	if m.ovCursor == fovMatch {
		return m.matchRowKey(msg)
	}
	return m.sectionRowKey(msg)
}

// fovLabelRows is how many rows the label section occupies: its visible
// labels, or the single "nothing matched" placeholder a fruitless type-ahead
// leaves behind (#2111) so the cursor still has a row to sit on while the
// query is edited back into something that matches.
func (m *Model) fovLabelRows() int {
	if n := len(m.filterViewLabels()); n > 0 {
		return n
	}
	if m.tab != TabPRs && m.ovSearch.Active() {
		return 1
	}
	return 0
}

// searchingFilterKey routes a key while the label section's type-ahead is
// running: enter keeps the filters, esc peels the query, up/down walk the
// narrowed labels only, and everything else is the query or a label toggle.
func (m *Model) searchingFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	first, rows := m.fovFixedRows(), m.fovLabelRows()
	if m.ovCursor < first {
		m.ovCursor = first
	}
	switch msg.String() {
	case "enter":
		m.closeOverlay()
		return nil
	case "esc":
		m.ovSearch.Reset()
		m.ovCursor = first
		m.clampOverlay()
		return nil
	case "down", "tab", "ctrl+n":
		m.ovCursor = first + (m.ovCursor-first+1)%rows
		m.clampOverlay()
		return nil
	case "up", "shift+tab", "ctrl+p":
		m.ovCursor = first + (m.ovCursor-first-1+rows)%rows
		m.clampOverlay()
		return nil
	}
	return m.labelRowKey(msg)
}

// matchRowKey feeds one key to the match input; edits re-narrow live and send
// the cursor to the top — with a fuzzy pattern the best match belongs first.
func (m *Model) matchRowKey(msg tea.KeyPressMsg) tea.Cmd {
	if out, ncur, handled, changed := ui.EditKey(msg, m.fInput, m.fCur); handled {
		m.fInput, m.fCur = out, ncur
		if changed {
			m.filterTouched = true
			m.resetCursors()
			m.applyFilter()
		}
	}
	return nil
}

// sectionRowKey handles the non-input rows: space (or left/right, h/l) cycles
// or toggles the row under the cursor, backspace clears its section, and j/k
// stay navigation — the input does not own them here. On a label row the
// letters belong to the section's type-ahead instead (#2111), so j/k are
// navigation only above the label section.
func (m *Model) sectionRowKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if m.tab != TabPRs && m.ovCursor >= m.fovFixedRows() {
		return m.labelRowKey(msg)
	}
	n := m.fovFixedRows() + m.fovLabelRows()
	switch key {
	case "j":
		m.ovCursor = (m.ovCursor + 1) % n
		m.clampOverlay()
		return nil
	case "k":
		m.ovCursor = (m.ovCursor - 1 + n) % n
		m.clampOverlay()
		return nil
	}
	switch m.fovKind(m.ovCursor) {
	case fovState:
		switch key {
		case "space", " ", "x", "right", "l":
			return m.setState(StateFilter((int(m.state) + 1) % 3))
		case "left", "h":
			return m.setState(StateFilter((int(m.state) + 2) % 3))
		case "backspace", "delete":
			return m.setState(FilterOpen)
		}
	case fovSort:
		switch key {
		case "space", " ", "x", "right", "l":
			m.cycleSort()
		case "left", "h":
			m.sortTouched = true
			m.sort = SortOrder((int(m.sort) + len(sortOrders) - 1) % len(sortOrders))
			m.keepSelection()
		case "backspace", "delete":
			m.sortTouched = true
			m.sort = SortRelevance
			m.keepSelection()
		}
	case fovGroup:
		switch key {
		case "space", " ", "x", "right", "left", "h", "l":
			m.toggleGroup()
		case "backspace", "delete":
			if m.group {
				m.toggleGroup()
			}
		}
	case fovSaved:
		switch key {
		case "space", " ", "x", "right", "l":
			return m.cycleSaved(1)
		case "left", "h":
			return m.cycleSaved(-1)
		case "backspace", "delete":
			return m.applySaved(0)
		}
	}
	return nil
}

// labelRowKey handles one label row: space toggles the label under the
// cursor, backspace clears the whole selection. Both re-narrow live while the
// cursor stays on the issue it was on. Every printable key is the section's
// type-ahead (#2111): it narrows the visible labels, and backspace peels it
// one rune at a time before it falls back to clearing the selection.
func (m *Model) labelRowKey(msg tea.KeyPressMsg) tea.Cmd {
	labels := m.filterViewLabels()
	i := m.ovCursor - m.fovFixedRows()
	switch msg.String() {
	case "space", " ":
		if i >= 0 && i < len(labels) {
			m.filterTouched = true
			name := labels[i].Name
			if m.labelSel[name] {
				delete(m.labelSel, name)
			} else {
				m.labelSel[name] = true
			}
			m.keepSelection()
		}
		return nil
	}
	if handled, changed := m.ovSearch.Key(msg); handled {
		if changed {
			// The query moved the section under the cursor: land on its first
			// visible label so the best match reads first.
			m.ovCursor, m.ovTop = m.fovFixedRows(), 0
			m.clampOverlay()
		}
		return nil
	}
	switch msg.String() {
	case "backspace", "delete":
		m.filterTouched = true
		m.labelSel = map[string]bool{}
		m.keepSelection()
	}
	return nil
}

// revertFilters is esc inside the overlay: every dimension goes back to the
// snapshot the overlay opened with, refetching only when the restored state
// gate needs a listing the pane no longer holds.
func (m *Model) revertFilters() tea.Cmd {
	s := m.fSaved
	m.closeOverlay()
	m.fInput, m.fCur = s.input, s.cur
	m.labelSel = s.labels
	if m.labelSel == nil {
		m.labelSel = map[string]bool{}
	}
	m.sort, m.group = s.sort, s.group
	if s.state != m.state {
		return m.setState(s.state)
	}
	m.keepSelection()
	return nil
}

// setState switches the state gate, keeping the selection and refetching only
// when the current listing cannot answer the new gate — a listing fetched as
// "all" already covers every narrower filter client-side (#2104).
func (m *Model) setState(s StateFilter) tea.Cmd {
	if s == m.state {
		return nil
	}
	m.filterTouched = true
	m.state = s
	if m.fetched == forge.IssuesAll || m.fetched == s.issueState() {
		// The listing already covers the new gate — pure client-side split,
		// no fetch, and the cursor stays on its entry.
		m.keepSelection()
		return nil
	}
	// A gate the listing cannot answer refetches; the stale rows are dropped
	// first so they cannot lie about the filter while the fetch runs (#2107).
	// The cached label names (m.labels) survive on purpose, so the filter
	// overlay's label section does not blank out for the fetch's width.
	m.dropListingForRefetch()
	m.keepSelection()
	return m.startRefresh()
}

// keepSelection re-applies the filters while keeping the cursor on the issue
// and pull request it was on (#2104): toggling a label or the sort order must
// not yank the view to the top. An entry the new filter hides leaves the
// cursor at its old row, clamped.
func (m *Model) keepSelection() {
	issue, pr := 0, 0
	if is := m.Selected(); is != nil {
		issue = is.Number
	}
	if p := m.SelectedPR(); p != nil {
		pr = p.Number
	}
	m.applyFilter()
	m.restoreCursor(issue)
	m.restorePRCursor(pr)
}

// restorePRCursor is restoreCursor for the PR view.
func (m *Model) restorePRCursor(n int) {
	if n == 0 {
		return
	}
	for i, r := range m.prRows {
		if r.idx >= 0 && m.prs[r.idx].Number == n {
			m.prCursor = i
			break
		}
	}
	m.clampScroll()
}

// filterChip is one active narrowing the filter row renders — its display
// text and the clear action a click on it runs.
type filterChip struct {
	text  string
	clear func(*Model) tea.Cmd
}

// filterChips lists every narrowing and ordering that differs from the
// pane's defaults, in the row's render order. Each label is its own chip, so
// one of three labels can be cleared without touching the other two.
func (m *Model) filterChips() []filterChip {
	var chips []filterChip
	if m.fInput != "" {
		chips = append(chips, filterChip{text: "match: " + m.fInput,
			clear: func(m *Model) tea.Cmd {
				m.filterTouched = true
				m.fInput, m.fCur = "", 0
				m.keepSelection()
				return nil
			}})
	}
	for _, name := range m.LabelFilter() {
		name := name
		chips = append(chips, filterChip{text: name,
			clear: func(m *Model) tea.Cmd {
				m.filterTouched = true
				delete(m.labelSel, name)
				m.keepSelection()
				return nil
			}})
	}
	if m.state != FilterOpen {
		chips = append(chips, filterChip{text: "state: " + m.state.String(),
			clear: func(m *Model) tea.Cmd { return m.setState(FilterOpen) }})
	}
	if m.sort != SortRelevance {
		chips = append(chips, filterChip{text: "sort: " + m.sort.String(),
			clear: func(m *Model) tea.Cmd { m.sortTouched = true; m.sort = SortRelevance; m.keepSelection(); return nil }})
	}
	if m.group {
		chips = append(chips, filterChip{text: "grouped",
			clear: func(m *Model) tea.Cmd { m.toggleGroup(); return nil }})
	}
	return chips
}

// chipText is one chip's rendered text, with the clear affordance spelled out.
func chipText(c filterChip) string { return "[" + c.text + " ⌫]" }

// chipSpans returns each chip's [start, end) column range in the rendered
// filter row, so a click resolves to the chip it landed on (mouse.go). The
// geometry mirrors renderChips exactly.
func (m *Model) chipSpans() [][2]int {
	chips := m.filterChips()
	spans := make([][2]int, 0, len(chips))
	x := 1 // the row's leading space
	for i, c := range chips {
		if i > 0 {
			x++ // the separating space
		}
		w := len([]rune(chipText(c)))
		spans = append(spans, [2]int{x, x + w})
		x += w
	}
	return spans
}

// clearChip runs the clear action of the chip at column x, nil when the click
// missed every chip.
func (m *Model) clearChip(x int) tea.Cmd {
	chips := m.filterChips()
	for i, span := range m.chipSpans() {
		if x >= span[0] && x < span[1] {
			return chips[i].clear(m)
		}
	}
	return nil
}
