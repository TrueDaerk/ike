package ghissues

// filter.go computes what each view shows: the state gate, the label
// selection and the fuzzy pattern narrow the listing, the sort order arranges
// it, and the optional grouping by label turns the flat index list into rows
// with headers (#2090).

import (
	"sort"
	"strconv"

	"ike/internal/forge"
)

// noLabelGroup is the bucket unlabelled issues fall into while grouping; it
// sorts last so the named groups read first.
const noLabelGroup = "(no label)"

// applyFilter recomputes both views' match sets and rows, then re-clamps the
// active view's cursor.
func (m *Model) applyFilter() {
	m.applyIssueFilter()
	m.applyPRFilter()
	m.clampScroll()
}

// applyIssueFilter narrows and orders the issue list: the state gate and the
// label selection are hard filters, the fuzzy pattern gates and scores, and
// the sort order arranges what survives.
func (m *Model) applyIssueFilter() {
	labels := m.LabelFilter()
	m.visible = m.visible[:0]
	scores := map[int]int{}
	for i := range m.issues {
		is := &m.issues[i]
		if !stateAllows(m.state, is.State) {
			continue
		}
		if len(labels) > 0 && !m.matchesLabels(is, labels) {
			continue
		}
		score, ok := m.fuzzyGate(matchText(is))
		if !ok {
			continue
		}
		scores[i] = score
		m.visible = append(m.visible, i)
	}
	sort.SliceStable(m.visible, m.issueLess(scores))
	m.rows = m.buildRows()
}

// applyPRFilter narrows and orders the pull-request list. Pull requests are
// always fetched in every state, so the state gate is purely client-side:
// open shows OPEN, closed shows MERGED and CLOSED, all shows everything.
func (m *Model) applyPRFilter() {
	m.prVisible = m.prVisible[:0]
	scores := map[int]int{}
	for i := range m.prs {
		pr := &m.prs[i]
		if !prStateAllows(m.state, pr.State) {
			continue
		}
		score, ok := m.fuzzyGate(prMatchText(pr))
		if !ok {
			continue
		}
		scores[i] = score
		m.prVisible = append(m.prVisible, i)
	}
	sort.SliceStable(m.prVisible, m.prLess(scores))
	m.prRows = m.prRows[:0]
	for _, idx := range m.prVisible {
		m.prRows = append(m.prRows, listRow{idx: idx})
	}
}

// stateAllows gates one issue by the state filter. A backend that does not
// report a per-issue state leaves it empty; the fetch already asked for the
// right state, so an unknown state always passes.
func stateAllows(f StateFilter, state string) bool {
	if state == "" {
		return true
	}
	switch f {
	case FilterOpen:
		return state != "CLOSED"
	case FilterClosed:
		return state == "CLOSED"
	default:
		return true
	}
}

// prStateAllows gates one pull request by the state filter, folding MERGED in
// with CLOSED — both are "done" from the list's point of view.
func prStateAllows(f StateFilter, state string) bool {
	switch f {
	case FilterOpen:
		return state == "" || state == "OPEN"
	case FilterClosed:
		return state == "MERGED" || state == "CLOSED"
	default:
		return true
	}
}

// matchesLabels applies the selection with the active semantics (#2112):
// any-of by default, all-of once the overlay's mode row is switched.
func (m *Model) matchesLabels(is *forge.Issue, labels []string) bool {
	if m.labelAll {
		return hasAllLabels(is, labels)
	}
	return hasAnyLabel(is, labels)
}

// hasAllLabels reports whether the issue carries every selected label — the
// all-of (AND) mode, where selecting "bug" and "feature" narrows to their
// intersection.
func hasAllLabels(is *forge.Issue, labels []string) bool {
	for _, name := range labels {
		if !hasLabel(is, name) {
			return false
		}
	}
	return true
}

// hasAnyLabel reports whether the issue carries at least one selected label —
// the any-of (OR) mode, where selecting "bug" and "feature" widens rather
// than narrowing to their intersection.
func hasAnyLabel(is *forge.Issue, labels []string) bool {
	for _, name := range labels {
		if hasLabel(is, name) {
			return true
		}
	}
	return false
}

// issueLess is the comparator for the active sort order over m.visible.
func (m *Model) issueLess(scores map[int]int) func(a, b int) bool {
	at := func(i int) *forge.Issue { return &m.issues[m.visible[i]] }
	switch m.effectiveSort() {
	case SortRelevance:
		return func(a, b int) bool { return scores[m.visible[a]] > scores[m.visible[b]] }
	case SortOldest:
		return func(a, b int) bool { return at(a).CreatedAt.Before(at(b).CreatedAt) }
	case SortUpdated:
		return func(a, b int) bool { return at(a).UpdatedAt.After(at(b).UpdatedAt) }
	case SortNumber:
		return func(a, b int) bool { return at(a).Number < at(b).Number }
	default: // SortNewest
		return func(a, b int) bool { return at(a).CreatedAt.After(at(b).CreatedAt) }
	}
}

// prLess is issueLess for the PR view.
func (m *Model) prLess(scores map[int]int) func(a, b int) bool {
	at := func(i int) *forge.PR { return &m.prs[m.prVisible[i]] }
	switch m.effectiveSort() {
	case SortRelevance:
		return func(a, b int) bool { return scores[m.prVisible[a]] > scores[m.prVisible[b]] }
	case SortOldest:
		return func(a, b int) bool { return at(a).CreatedAt.Before(at(b).CreatedAt) }
	case SortUpdated:
		return func(a, b int) bool { return at(a).UpdatedAt.After(at(b).UpdatedAt) }
	case SortNumber:
		return func(a, b int) bool { return at(a).Number < at(b).Number }
	default: // SortNewest
		return func(a, b int) bool { return at(a).CreatedAt.After(at(b).CreatedAt) }
	}
}

// effectiveSort resolves SortRelevance: it only means "best fuzzy match" while
// a pattern is typed; without one it reads as SortNewest, which — with the
// forge listing already arriving newest first — keeps the pane's pre-#2090
// order. Every comparator is stable, so entries the order cannot separate
// (missing timestamps, equal scores) keep the listing order.
func (m *Model) effectiveSort() SortOrder {
	if m.sort == SortRelevance && m.fInput == "" {
		return SortNewest
	}
	return m.sort
}

// buildRows turns the ordered issue index list into rendered rows: flat when
// grouping is off, otherwise one header per label group followed by its
// issues. An issue is filed under its alphabetically first label, so it
// appears exactly once however many labels it carries.
func (m *Model) buildRows() []listRow {
	rows := make([]listRow, 0, len(m.visible))
	if !m.group {
		for _, idx := range m.visible {
			rows = append(rows, listRow{idx: idx})
		}
		return rows
	}
	groups := map[string][]int{}
	var names []string
	for _, idx := range m.visible {
		key := groupKey(&m.issues[idx])
		if _, ok := groups[key]; !ok {
			names = append(names, key)
		}
		groups[key] = append(groups[key], idx)
	}
	sort.Slice(names, func(a, b int) bool {
		if (names[a] == noLabelGroup) != (names[b] == noLabelGroup) {
			return names[b] == noLabelGroup
		}
		return names[a] < names[b]
	})
	for _, name := range names {
		rows = append(rows, listRow{header: name + " (" + strconv.Itoa(len(groups[name])) + ")", idx: -1})
		for _, idx := range groups[name] {
			rows = append(rows, listRow{idx: idx})
		}
	}
	return rows
}

// groupKey is the label an issue is filed under while grouping.
func groupKey(is *forge.Issue) string {
	best := ""
	for _, l := range is.Labels {
		if best == "" || l.Name < best {
			best = l.Name
		}
	}
	if best == "" {
		return noLabelGroup
	}
	return best
}

// filterRowShown reports whether the pane spends a row on the filter status.
// Since #2104 the row is permanent: the chips, a mutation's in-flight and
// error states (#2088) and the idle hint all live there, and a chip appearing
// no longer shifts the whole body by a line.
func (m *Model) filterRowShown() bool { return true }
