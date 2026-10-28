package complexitypanel

// sites.go is the per-site breakdown of the Complexity window (Roadmap
// 0551, #2946): `space` on a function row of an ungrouped tab expands it
// in place into one indented child row per complexity.Site — the kind and
// its `+3 (nesting 2)` cost in the function column, the line, the CCN and
// the cognitive increment — sorted by cognitive cost descending, then
// line. One function is expanded at a time — `shift+space`
// (complexity.sitesAll, #3082) expands or collapses every visible one at
// once; the expansion is remembered by file and function name, so a
// snapshot refresh that still holds the function rebuilds its rows. On
// the Changes tab the after side's sites are shown (a removed function
// has none in the working tree). The function's extraction candidates
// follow the sites (#2948, candidates.go).

import (
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// siteIndent leads the function cell of a site row.
const siteIndent = "  "

// ToggleSites expands the selected function row into its sites, or
// collapses it when it is expanded; on a site row it collapses the parent.
// Expanding replaces a single earlier expansion (one function at a time)
// but joins a bulk one ('shift+space', ToggleAllSites). It reports false
// when the selection has nothing to expand: no row, a group row, a removed
// function, or a function without sites.
func (m *Model) ToggleSites() bool {
	if m.treeActive() {
		return m.toggleTreeSites() // #2954
	}
	r, ok := m.Selected()
	if !ok {
		return false
	}
	if r.child() {
		if r.Parent >= 0 && r.Parent < len(m.rows) {
			m.cursor = r.Parent
		}
		delete(m.expanded, r.expandKey())
		m.Refresh()
		return true
	}
	if r.aggregate() || r.Change == complexity.Removed {
		return false
	}
	key := r.expandKey()
	if m.expanded[key] {
		delete(m.expanded, key)
		m.Refresh()
		return true
	}
	if len(r.Fn.Sites) == 0 {
		return false
	}
	if len(m.expanded) <= 1 {
		m.expanded = map[string]bool{}
	}
	m.expanded[key] = true
	m.Refresh()
	return true
}

// ToggleAllSites is complexity.sitesAll ('shift+space', #3082): when any
// visible function row is expanded it collapses every visible one, else it
// expands every visible function row that has sites. Rows the filter hides
// keep their state. It reports false when the tab shows no expandable row
// (a grouped tab, the tree, Trend, Duplicates, no function with sites).
func (m *Model) ToggleAllSites() bool {
	if m.treeActive() || m.Grouping() != complexity.ByNone {
		return false
	}
	var keys []string
	anyOpen := false
	for _, r := range m.rows {
		if !r.expandable() {
			continue
		}
		k := r.expandKey()
		keys = append(keys, k)
		anyOpen = anyOpen || m.expanded[k]
	}
	if len(keys) == 0 {
		return false
	}
	if r, ok := m.Selected(); ok && r.child() && r.Parent >= 0 && r.Parent < len(m.rows) {
		m.cursor = r.Parent // the site row under the cursor may vanish
	}
	if m.expanded == nil {
		m.expanded = map[string]bool{}
	}
	for _, k := range keys {
		if anyOpen {
			delete(m.expanded, k)
		} else {
			m.expanded[k] = true
		}
	}
	m.Refresh()
	return true
}

// expandable reports a function row space could expand: a unit of its own
// with sites, not removed, not a Trend / Duplicates / gone-pin row.
func (r Row) expandable() bool {
	return !r.child() && !r.aggregate() && r.Dup == nil && r.Trend == nil && !r.Gone &&
		r.Change != complexity.Removed && len(r.Fn.Sites) > 0
}

// Expanded reports the first expanded function row, false when none.
func (m *Model) Expanded() (Row, bool) {
	for _, r := range m.rows {
		if !r.child() && m.expanded[r.expandKey()] {
			return r, true
		}
	}
	return Row{}, false
}

// expandKey identifies a function across snapshots: its file, its name
// and its Changes section — not its line, which an edit above it moves.
func (r Row) expandKey() string {
	return r.Path + "\x00" + r.Fn.Name + "\x00" + strconv.Itoa(int(r.Change))
}

// spliceSites inserts every expanded function's site rows right below it,
// after the filter and the sort have placed the function rows, so the
// children stay glued under their parent whatever the sort. A grouped tab
// has no function rows and nothing to expand.
func (m *Model) spliceSites() {
	if len(m.expanded) == 0 || m.Grouping() != complexity.ByNone || m.treeActive() {
		return
	}
	var rows []Row
	for i, r := range m.rows {
		if r.child() || !m.expanded[r.expandKey()] || len(r.Fn.Sites) == 0 {
			if rows != nil {
				rows = append(rows, r)
			}
			continue
		}
		if rows == nil {
			rows = make([]Row, 0, len(m.rows)+len(r.Fn.Sites))
			rows = append(rows, m.rows[:i]...)
		}
		parent := len(rows)
		rows = append(rows, r)
		rows = append(rows, siteRows(r, parent)...)
		rows = append(rows, candidateRows(r, parent)...)
	}
	if rows != nil {
		m.rows = rows
	}
}

// siteRows builds the child rows of the function row parent at index
// parentIdx: one per site, cognitive descending, then line.
func siteRows(parent Row, parentIdx int) []Row {
	sites := make([]*complexity.Site, len(parent.Fn.Sites))
	for i := range parent.Fn.Sites {
		sites[i] = &parent.Fn.Sites[i]
	}
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].Cognitive != sites[j].Cognitive {
			return sites[i].Cognitive > sites[j].Cognitive
		}
		if sites[i].Line != sites[j].Line {
			return sites[i].Line < sites[j].Line
		}
		return sites[i].Col < sites[j].Col
	})
	out := make([]Row, len(sites))
	for i, s := range sites {
		out[i] = Row{Path: parent.Path, Rel: parent.Rel, Lang: parent.Lang, Fn: parent.Fn,
			Change: parent.Change, Test: parent.Test, Site: s, Parent: parentIdx}
	}
	return out
}

// siteCell renders a site row's text for column c: the kind and its cost
// in the function column, the line, the CCN and cognitive increments; the
// other columns stay blank.
func (r Row) siteCell(c Column) string {
	s := r.Site
	switch c {
	case ColFunc:
		return siteIndent + s.Kind + siteCost(s)
	case ColLine:
		return strconv.Itoa(s.Line + 1)
	case ColCCN:
		return strconv.Itoa(s.CCN)
	case ColCog:
		return strconv.Itoa(s.Cognitive)
	}
	return ""
}

// siteCost is the Sonar-style cost text of a site: " +3 (nesting 2)",
// " +1", or "" for a CCN-only arm.
func siteCost(s *complexity.Site) string {
	if s.Cognitive == 0 {
		return ""
	}
	t := " +" + strconv.Itoa(s.Cognitive)
	if s.Nesting > 0 {
		t += " (nesting " + strconv.Itoa(s.Nesting) + ")"
	}
	return t
}

// siteKey is the cursor-keeping key of a site row under its parent.
func (r Row) siteKey() string {
	return r.expandKey() + "\x00s\x00" + strconv.Itoa(r.Site.Line) + "\x00" + strconv.Itoa(r.Site.Col) + "\x00" + r.Site.Kind
}

// siteCopyText renders a site row as "path:line: kind +cog (nesting n)".
func siteCopyText(r Row) string {
	return r.Rel + ":" + strconv.Itoa(r.Site.Line+1) + ": " + r.Site.Kind + siteCost(r.Site)
}

// ShowSitesMsg asks the root model to paint one function's per-site marks
// in the editor once (#2947, 'm'), whatever editor.marks.complexity_sites
// says, until the buffer's version moves. Path is the file, Line the
// function's 0-based header line.
type ShowSitesMsg struct {
	Path string
	Line int
}

// ShowSites is the selection's ShowSitesMsg: a function row, or the parent
// of a site row. A group row or a removed function has none.
func (m *Model) ShowSites() (ShowSitesMsg, bool) {
	r, ok := m.Selected()
	if !ok || r.aggregate() || r.Change == complexity.Removed || r.Path == "" {
		return ShowSitesMsg{}, false
	}
	return ShowSitesMsg{Path: r.Path, Line: r.Fn.Line}, true
}

// showSites is the pane's 'm'.
func (m *Model) showSites() tea.Cmd {
	msg, ok := m.ShowSites()
	if !ok {
		return nil
	}
	return func() tea.Msg { return msg }
}

// openSite asks the root model to open the site's line.
func (m *Model) openSite(r Row) tea.Cmd {
	msg := OpenLocationMsg{Path: r.Path, Line: r.Site.Line}
	return func() tea.Msg { return msg }
}
