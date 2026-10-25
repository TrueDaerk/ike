package complexitypanel

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// hidden.go is the panel half of the hidden set (Roadmap 0551, #3031): the
// root model hands the pane the project's hidden functions
// (complexity.HiddenSet, the `hidden` section of
// .ike/complexity-baseline.json) via SetHidden, independent of the
// baseline setting. A hidden function is dropped from every tab, the group
// and tree aggregates, the histogram, the totals and the exports as if it
// were not measured; the header counts them (`N hidden`). The `hidden:yes`
// filter term — toggled by 'H' — shows them instead, muted and marked ✕,
// so one of them can be un-hidden with the same key that hid it. 'x'
// (complexity.hide) hides the selected function, or — on a group, tree
// directory, file or Changes file row — the file through a
// complexity.exclude glob (HideFileMsg); 'X' (complexity.hideFile) hides
// the selected row's file from a function row too. The pane never touches
// the file or the settings itself.

// hiddenMark follows the name of a hidden function shown by hidden:yes.
const hiddenMark = " ✕"

// HideToggleMsg asks the root model to hide Items (Remove false) or to
// un-hide them (Remove true) and save the baseline file.
type HideToggleMsg struct {
	Items  []BaselineItem
	Remove bool
}

// HideFileMsg asks the root model to add Globs to complexity.exclude so
// the index drops the files; Label names what was hidden for the notice.
type HideFileMsg struct {
	Globs []string
	Label string
}

// SetHidden hands the pane the hidden set in effect; nil hides nothing.
// The rows are rebuilt either way: the same pointer may hold new entries.
func (m *Model) SetHidden(h *complexity.HiddenSet) {
	m.hidden = h
	m.Refresh()
}

// Hidden reports the hidden set the pane applies (tests).
func (m *Model) Hidden() *complexity.HiddenSet { return m.hidden }

// HiddenCount is how many functions of the current tab's population the
// hidden set dropped (or, under hidden:yes, kept marked).
func (m *Model) HiddenCount() int { return m.hiddenN }

// ShowingHidden reports whether the filter carries hidden:yes.
func (m *Model) ShowingHidden() bool { return m.filter.HasTerm("hidden", "yes") }

// markHidden flags the hidden rows of a population and, unless the filter
// names the hidden: field, drops them — so the totals, the groups, the
// tree and the histogram follow the visible population, like the test
// files under 't' (#2950).
func (m *Model) markHidden(rows []Row) []Row {
	m.hiddenN = 0
	show := m.filter.Query().Has("hidden")
	out := rows[:0]
	for _, r := range rows {
		if r.Hidden {
			m.hiddenN++
			if !show {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// hiddenOf reports whether the function at index i of file rel is hidden,
// given the Mask of its file (nil: nothing hidden).
func hiddenOf(mask []bool, i int) bool { return mask != nil && i < len(mask) && mask[i] }

// hiddenMatches reads a hidden: filter value: yes keeps the hidden rows
// (groups: with a hidden member), no the rest.
func hiddenMatches(v string, r Row) bool {
	want, ok := testValue(v)
	if !ok {
		return false
	}
	if r.Group != nil || r.FileChange != nil {
		for _, mem := range r.Members {
			if mem.Hidden == want {
				return true
			}
		}
		return len(r.Members) == 0 && !want
	}
	return r.Hidden == want
}

// hiddenTotals is the header's "N hidden" part, "" while nothing is.
func (m *Model) hiddenTotals() string {
	if m.hiddenN == 0 {
		return ""
	}
	return strconv.Itoa(m.hiddenN) + " hidden"
}

// ToggleHiddenView flips the hidden:yes filter term ('H',
// complexity.showHidden): on, the hidden functions are listed (muted, ✕)
// so one can be un-hidden; off, the filter term goes again. It reports
// whether the view now shows the hidden rows.
func (m *Model) ToggleHiddenView() bool {
	if m.ShowingHidden() {
		m.filter.SetTerm("hidden", "")
	} else {
		m.filter.SetTerm("hidden", "yes")
	}
	m.Refresh()
	return m.ShowingHidden()
}

// ToggleHide builds the message for 'x' on the selected row and reports
// the reason the footer shows when there is none: a function row (a site
// or candidate row stands for its function, a Changes row for its
// working-tree side) hides its function, or un-hides a hidden one shown
// by hidden:yes; a group, tree directory, file or Changes file row hides
// the file (or directory) through complexity.exclude; a Trend row, a
// removed function and the tree's project root yield nothing.
func (m *Model) ToggleHide() (tea.Cmd, string) {
	r, ok := m.Selected()
	if !ok {
		return nil, "no row selected"
	}
	if r.Trend != nil {
		return nil, "the Trend tab lists commits, not functions"
	}
	if r.child() && r.Parent >= 0 && r.Parent < len(m.rows) {
		r = m.rows[r.Parent]
	}
	switch {
	case r.Group != nil || r.FileChange != nil:
		return m.hideFileOf(r)
	case r.Change == complexity.Removed:
		return nil, "a removed function is not in the working tree"
	case r.Gone:
		return nil, "a gone pin has no function to hide"
	}
	item := r.item()
	if r.Change != 0 {
		item.Ordinal = m.changeOrdinal(r)
	}
	msg := HideToggleMsg{Items: []BaselineItem{item}, Remove: r.Hidden}
	return func() tea.Msg { return msg }, ""
}

// toggleHide is the 'x' key: the message, or the reason in the footer.
func (m *Model) toggleHide() tea.Cmd {
	cmd, why := m.ToggleHide()
	m.note = why
	return cmd
}

// HideFile builds the HideFileMsg for the selected row's file ('X'): the
// file of a function, site, group or Changes row, the directory of a tree
// directory node. The project root and a Trend row yield nothing.
func (m *Model) HideFile() (tea.Cmd, string) {
	r, ok := m.Selected()
	if !ok {
		return nil, "no row selected"
	}
	if r.Trend != nil {
		return nil, "the Trend tab lists commits, not files"
	}
	if r.child() && r.Parent >= 0 && r.Parent < len(m.rows) {
		r = m.rows[r.Parent]
	}
	return m.hideFileOf(r)
}

// hideFile is the 'X' key: the message, or the reason in the footer.
func (m *Model) hideFile() tea.Cmd {
	cmd, why := m.HideFile()
	m.note = why
	return cmd
}

// hideFileOf is the HideFileMsg for r's file: its project-relative path as
// the glob, `dir/**` for a tree directory node; the tree's root node is
// refused.
func (m *Model) hideFileOf(r Row) (tea.Cmd, string) {
	glob := r.Rel
	if m.treeActive() {
		switch m.treeSelectedKind() {
		case treeRoot:
			return nil, "hide a file or a directory, not the project root"
		case treeDir:
			glob = strings.TrimSuffix(r.Rel, "/") + "/**"
		}
	}
	if glob == "" || glob == "." {
		return nil, "this row names no file"
	}
	msg := HideFileMsg{Globs: []string{glob}, Label: glob}
	return func() tea.Msg { return msg }, ""
}

// treeSelectedKind is the level of the selected tree node, treeRoot when
// nothing is selected.
func (m *Model) treeSelectedKind() treeKind {
	cur := m.tree.Current()
	if cur == nil {
		return treeRoot
	}
	return cur.Item.Kind
}

// hiddenFooter is the footer's hide hints: 'x' always, 'H' once there is
// something hidden or the hidden view is on.
func (m *Model) hiddenFooter() string {
	s := " · x hide"
	switch {
	case m.ShowingHidden():
		s += " · H hidden: shown"
	case m.hiddenN > 0:
		s += " · H hidden"
	}
	return s
}
