package complexitypanel

// sites.go is the per-site breakdown of the Complexity window (Roadmap
// 0551, #2946): `space` on a function row of an ungrouped tab expands it
// in place into one indented child row per complexity.Site — the kind and
// its `+3 (nesting 2)` cost in the function column, the line, the CCN and
// the cognitive increment — sorted by cognitive cost descending, then
// line. One function is expanded at a time; the expansion is remembered
// by file and function name, so a snapshot refresh that still holds the
// function rebuilds its rows. On the Changes tab the after side's sites
// are shown (a removed function has none in the working tree).

import (
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// siteIndent leads the function cell of a site row.
const siteIndent = "  "

// ToggleSites expands the selected function row into its sites, or
// collapses it when it is the expanded one; on a site row it collapses the
// parent. It reports false when the selection has nothing to expand: no
// row, a group row, a removed function, or a function without sites.
func (m *Model) ToggleSites() bool {
	r, ok := m.Selected()
	if !ok {
		return false
	}
	if r.Site != nil {
		if r.Parent >= 0 && r.Parent < len(m.rows) {
			m.cursor = r.Parent
		}
		m.expanded = ""
		m.Refresh()
		return true
	}
	if r.Group != nil || r.Change == complexity.Removed {
		return false
	}
	key := r.expandKey()
	if m.expanded == key {
		m.expanded = ""
		m.Refresh()
		return true
	}
	if len(r.Fn.Sites) == 0 {
		return false
	}
	m.expanded = key
	m.Refresh()
	return true
}

// Expanded reports the function row currently expanded, false when none.
func (m *Model) Expanded() (Row, bool) {
	if m.expanded == "" {
		return Row{}, false
	}
	for _, r := range m.rows {
		if r.Site == nil && r.expandKey() == m.expanded {
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

// spliceSites inserts the expanded function's site rows right below it,
// after the filter and the sort have placed the function rows, so the
// children stay glued under their parent whatever the sort. A grouped tab
// has no function rows and nothing to expand.
func (m *Model) spliceSites() {
	if m.expanded == "" || m.Grouping() != complexity.ByNone {
		return
	}
	for i, r := range m.rows {
		if r.Site != nil || r.expandKey() != m.expanded || len(r.Fn.Sites) == 0 {
			continue
		}
		kids := siteRows(r, i)
		rows := make([]Row, 0, len(m.rows)+len(kids))
		rows = append(rows, m.rows[:i+1]...)
		rows = append(rows, kids...)
		rows = append(rows, m.rows[i+1:]...)
		m.rows = rows
		return
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

// openSite asks the root model to open the site's line.
func (m *Model) openSite(r Row) tea.Cmd {
	msg := OpenLocationMsg{Path: r.Path, Line: r.Site.Line}
	return func() tea.Msg { return msg }
}
