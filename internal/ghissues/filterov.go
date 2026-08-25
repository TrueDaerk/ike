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
// the sort cycle and the grouping toggle (the last one on the issue tab only).
const (
	fovMatch = 0
	fovState = 1
	fovSort  = 2
	fovGroup = 3
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

// fovFixedRows is how many non-label rows the overlay has on the active tab —
// the PR view has no label filter and no grouping.
func (m *Model) fovFixedRows() int {
	if m.tab == TabPRs {
		return 3 // match, state, sort
	}
	return 4
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

// openFilterOverlay opens the unified overlay with the cursor on row, taking
// the snapshot esc restores.
func (m *Model) openFilterOverlay(row int) {
	m.fSaved = m.snapshotFilters()
	m.ov, m.ovTop = ovFilter, 0
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
	n := m.fovFixedRows() + len(m.filterLabels())
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
	return m.sectionRowKey(msg.String())
}

// matchRowKey feeds one key to the match input; edits re-narrow live and send
// the cursor to the top — with a fuzzy pattern the best match belongs first.
func (m *Model) matchRowKey(msg tea.KeyPressMsg) tea.Cmd {
	if out, ncur, handled, changed := ui.EditKey(msg, m.fInput, m.fCur); handled {
		m.fInput, m.fCur = out, ncur
		if changed {
			m.resetCursors()
			m.applyFilter()
		}
	}
	return nil
}

// sectionRowKey handles the non-input rows: space (or left/right, h/l) cycles
// or toggles the row under the cursor, backspace clears its section, and j/k
// stay navigation — the input does not own them here.
func (m *Model) sectionRowKey(key string) tea.Cmd {
	n := m.fovFixedRows() + len(m.filterLabels())
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
	if m.tab != TabPRs && m.ovCursor >= m.fovFixedRows() {
		return m.labelRowKey(key)
	}
	switch m.ovCursor {
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
	}
	return nil
}

// labelRowKey handles one label row: space toggles the label under the
// cursor, backspace clears the whole selection. Both re-narrow live while the
// cursor stays on the issue it was on.
func (m *Model) labelRowKey(key string) tea.Cmd {
	labels := m.filterLabels()
	i := m.ovCursor - m.fovFixedRows()
	switch key {
	case "space", " ", "x":
		if i >= 0 && i < len(labels) {
			name := labels[i].Name
			if m.labelSel[name] {
				delete(m.labelSel, name)
			} else {
				m.labelSel[name] = true
			}
			m.keepSelection()
		}
	case "backspace", "delete":
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
			clear: func(m *Model) tea.Cmd { m.fInput, m.fCur = "", 0; m.keepSelection(); return nil }})
	}
	for _, name := range m.LabelFilter() {
		name := name
		chips = append(chips, filterChip{text: name,
			clear: func(m *Model) tea.Cmd { delete(m.labelSel, name); m.keepSelection(); return nil }})
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
