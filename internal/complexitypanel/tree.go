package complexitypanel

// tree.go is the directory tree view of the Project tab (Roadmap 0551,
// #2954): 'v' (the complexity.view command) switches the tab between the
// grid and a hiertree over the measured files — root → directory → … →
// file → class → function, a class level only where functions carry a
// Class — whose directory, file and class nodes carry the file-group
// aggregate columns (complexity.Summarize over the functions below them,
// computed once per rebuild, bottom-up) and whose function leaves carry
// the function metrics in the matching columns. Every directory level is
// kept: it is what the user drills through. Children of a node sort by
// the sort column (Σccn descending by default; 's' / 'S' as in the grouped
// grid). The filter applies to the function leaves and prunes empty
// branches; 'enter' opens a function and toggles any other node; a
// function with sites expands into its breakdown rows (#2946) through
// 'space' or 'l'; 'y' copies the node's aggregate line; 'e' exports the
// visible rows as CSV with a path column. The tree is served by
// hiertree.Static (the whole tree is in memory) and rebuilt on every
// Refresh through hiertree.Refresh, which keeps the expansion and the
// selection across a snapshot change. The state persists as `view:tree`
// in the layout Mode.

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/complexity"
	"ike/internal/gridview"
	"ike/internal/hiertree"
	"ike/internal/theme"
	"ike/internal/ui"
)

// viewTree is the Mode token of the tree view.
const viewTree = "view:tree"

// treeKind is the level of a tree node.
type treeKind int

const (
	treeRoot treeKind = iota
	treeDir
	treeFile
	treeClass
	treeFn
	// treeChild is a site or an extraction-candidate row under a function
	// (#2946, #2948).
	treeChild
)

// String is the kind's CSV spelling.
func (k treeKind) String() string {
	switch k {
	case treeRoot, treeDir:
		return "dir"
	case treeFile:
		return "file"
	case treeClass:
		return "class"
	case treeFn:
		return "function"
	}
	return "site"
}

// treeNode is one node of the tree: its stable key (what hiertree.Refresh
// carries the expansion and selection by), its level, the name shown in
// the name column, the Row standing for it — a group row with Members on
// an aggregate node, the function row on a leaf, a site / candidate row
// under an expanded function — and its children, already sorted.
type treeNode struct {
	Key      string
	Kind     treeKind
	Name     string
	Row      Row
	children []treeNode
}

// The tree layout: the name column, then the file-group aggregates. The
// hotspot columns follow inside a repository (#2957).
var (
	treeLayout      = []Column{ColFunc, ColFuncs, ColNLOC, ColCCN, ColCCNMax, ColCog, ColCogMax, ColCmt, ColOver, ColExcess}
	treeChurnLayout = withColumns(treeLayout, ColCommits, ColHot)
	treeLabels      = map[Column]string{ColFunc: "name", ColCCN: "Σccn", ColCog: "Σcog"}
)

// TreeView reports whether the Project tab shows the tree ('v').
func (m *Model) TreeView() bool { return m.treeView }

// treeActive reports whether the tree is what the pane shows now: the view
// is on and the Project tab is selected.
func (m *Model) treeActive() bool { return m.treeView && m.tab == TabProject }

// ToggleTreeView switches the Project tab between the grid and the tree.
// A sort column the new layout lacks falls back to CCN descending.
func (m *Model) ToggleTreeView() {
	m.treeView = !m.treeView
	m.cursor, m.top = 0, 0
	m.fitSort()
	m.Refresh()
}

// treeKey is the identity hiertree.Refresh carries state by.
func treeKey(n treeNode) string { return n.Key }

// treeFetch is the Static expansion over the in-memory children. The tree
// lives by value in the model, so the fetch is re-pointed before every
// use (hiertree.SetFetch), as the trace window does.
func (m *Model) treeFetch() hiertree.Fetch[treeNode] {
	return hiertree.Static(&m.tree, func(n treeNode) []hiertree.Row[treeNode] {
		out := make([]hiertree.Row[treeNode], len(n.children))
		for i, c := range n.children {
			out[i] = hiertree.Row[treeNode]{Entry: hiertree.Entry{Name: c.Name, Line: -1}, Item: c}
		}
		return out
	})
}

// buildTree rebuilds the tree from the filtered function rows. The first
// build opens the tree with the root expanded; a later one refreshes it,
// keeping every expanded node and the selection. An empty population
// clears the tree so the empty notice shows.
func (m *Model) buildTree() {
	fetch := m.treeFetch()
	m.tree.SetFetch(fetch)
	if len(m.rows) == 0 {
		m.tree.Clear()
		m.hotWarn = 0
		return
	}
	root := m.treeRoot(m.rows)
	roots := []hiertree.Row[treeNode]{{Entry: hiertree.Entry{Name: root.Name, Line: -1}, Item: root}}
	if !m.tree.HasNodes() {
		m.tree.Open(roots, fetch)
	} else {
		m.tree.Refresh(roots, treeKey)
	}
	if !m.filter.Empty() {
		m.expandBranches() // a filtered tree shows its matches
	}
	m.markTreeHot()
}

// expandBranches unfolds every directory, file and class node — the
// gesture of a filtered tree, whose pruned branches would otherwise hide
// the matches behind nodes the user never expanded. Function leaves keep
// their breakdown folded.
func (m *Model) expandBranches() {
	var walk func(rs []*hiertree.Row[treeNode])
	walk = func(rs []*hiertree.Row[treeNode]) {
		for _, r := range rs {
			if r.Item.Row.Group == nil {
				continue
			}
			m.tree.Expand(r)
			walk(r.Children())
		}
	}
	walk(m.tree.Roots())
}

// treeDirBuild is the directory level while the tree is assembled.
type treeDirBuild struct {
	subs  map[string]*treeDirBuild
	files map[string][]Row
}

func newTreeDir() *treeDirBuild {
	return &treeDirBuild{subs: map[string]*treeDirBuild{}, files: map[string][]Row{}}
}

// treeRoot assembles the whole tree from the function rows: the
// project-relative paths split on '/', every directory kept.
func (m *Model) treeRoot(rows []Row) treeNode {
	root := newTreeDir()
	for _, r := range rows {
		if r.child() || r.Gone {
			continue
		}
		parts := strings.Split(r.Rel, "/")
		d := root
		for _, p := range parts[:len(parts)-1] {
			sub, ok := d.subs[p]
			if !ok {
				sub = newTreeDir()
				d.subs[p] = sub
			}
			d = sub
		}
		d.files[parts[len(parts)-1]] = append(d.files[parts[len(parts)-1]], r)
	}
	name := filepath.Base(m.root)
	if m.root == "" || name == "." || name == string(filepath.Separator) {
		name = "."
	}
	n := m.dirNode(root, "", name)
	n.Kind = treeRoot
	return n
}

// dirNode builds the node of the directory at rel (its name is name): the
// subdirectories and files below it, sorted together, and the aggregate
// over every function underneath.
func (m *Model) dirNode(d *treeDirBuild, rel, name string) treeNode {
	var kids []treeNode
	var fns []Row
	for sub, sd := range d.subs {
		child := m.dirNode(sd, joinRel(rel, sub), sub)
		kids = append(kids, child)
		fns = append(fns, child.Row.Members...)
	}
	for file, rows := range d.files {
		child := m.fileNode(rows, joinRel(rel, file), file)
		kids = append(kids, child)
		fns = append(fns, rows...)
	}
	m.sortNodes(kids)
	path := m.root
	if rel != "" {
		path = filepath.Join(m.root, filepath.FromSlash(rel))
	}
	row := m.aggregateRow(path, rel, "", fns)
	// A directory's commits and hot score sum its files' (#2957).
	row.Commits, row.Hot = 0, 0
	for _, k := range kids {
		row.Commits += k.Row.Commits
		row.Hot += k.Row.Hot
	}
	return treeNode{Key: "dir\x00" + rel, Kind: treeDir, Name: name, Row: row, children: kids}
}

// joinRel appends a path element to a project-relative slash path.
func joinRel(rel, name string) string {
	if rel == "" {
		return name
	}
	return rel + "/" + name
}

// fileNode builds a file's node: one class node per Function.Class among
// its functions, the free functions directly under the file, sorted
// together; the aggregate over the file's functions.
func (m *Model) fileNode(rows []Row, rel, name string) treeNode {
	src := rows[0]
	var kids []treeNode
	classes := map[string][]Row{}
	var order []string
	for _, r := range rows {
		if r.Fn.Class == "" {
			kids = append(kids, m.fnNode(r))
			continue
		}
		if _, ok := classes[r.Fn.Class]; !ok {
			order = append(order, r.Fn.Class)
		}
		classes[r.Fn.Class] = append(classes[r.Fn.Class], r)
	}
	for _, c := range order {
		kids = append(kids, m.classNode(src, c, classes[c]))
	}
	m.sortNodes(kids)
	row := m.aggregateRow(src.Path, rel, "", rows)
	row.Lang, row.Test = src.Lang, src.Test
	row.Commits = m.churn[rel]
	row.Hot = complexity.Hot(row.Commits, row.Group.Summary)
	return treeNode{Key: "file\x00" + src.Path, Kind: treeFile, Name: name, Row: row, children: kids}
}

// classNode builds a class's node under its file: its methods, sorted,
// and the aggregate over them.
func (m *Model) classNode(src Row, class string, rows []Row) treeNode {
	kids := make([]treeNode, len(rows))
	for i, r := range rows {
		kids[i] = m.fnNode(r)
	}
	m.sortNodes(kids)
	row := m.aggregateRow(src.Path, src.Rel, class, rows)
	row.Lang, row.Test = src.Lang, src.Test
	row.Commits = m.churn[src.Rel]
	row.Hot = complexity.Hot(row.Commits, row.Group.Summary)
	return treeNode{Key: "class\x00" + src.Path + "\x00" + class, Kind: treeClass, Name: class, Row: row, children: kids}
}

// fnNode builds a function leaf; a function with sites or candidates
// carries them as children (the breakdown, #2946 / #2948), in the
// breakdown's own order.
func (m *Model) fnNode(r Row) treeNode {
	var kids []treeNode
	for _, c := range append(siteRows(r, -1), candidateRows(r, -1)...) {
		c.Ordinal, c.Pin = r.Ordinal, r.Pin
		name := strings.TrimPrefix(c.cell(ColFunc), siteIndent) // the tree indents itself
		kids = append(kids, treeNode{Key: c.key(), Kind: treeChild, Name: name, Row: c})
	}
	// The name carries the grid's marks (accepted ✓, pinned ★).
	return treeNode{Key: "fn\x00" + r.expandKey() + "\x00" + strconv.Itoa(r.Ordinal), Kind: treeFn, Name: r.cell(ColFunc), Row: r, children: kids}
}

// aggregateRow is the group row of a directory, file or class node:
// complexity.Summarize over the function rows below it, with the depth /
// params maxima the filter keys read and the Members 'i' accepts.
func (m *Model) aggregateRow(path, rel, class string, rows []Row) Row {
	fns := make([]complexity.Function, len(rows))
	var sum complexity.Summer
	for i, r := range rows {
		fns[i] = r.Fn
		sum.Add(r.Fn, m.th(r.Path)) // a directory spans files of differing thresholds (#2960)
	}
	g := &complexity.Group{File: path, Class: class, Summary: sum.Summary()}
	for i, f := range fns {
		if i == 0 || f.Line < g.Line {
			g.Line = f.Line
		}
		g.DepthMax = max(g.DepthMax, f.Depth)
		g.ParamsMax = max(g.ParamsMax, f.Params)
	}
	return Row{Path: path, Rel: rel, Group: g, Members: rows}
}

// sortNodes orders sibling nodes by the sort column: the name column as
// text, every other as a number; ties by name, then by line.
func (m *Model) sortNodes(nodes []treeNode) {
	col, desc := m.sortCol, m.desc
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if col == ColFunc {
			if a.Name != b.Name {
				if desc {
					return a.Name > b.Name
				}
				return a.Name < b.Name
			}
		} else if av, bv := m.nodeValue(a, col), m.nodeValue(b, col); av != bv {
			if desc {
				return av > bv
			}
			return av < bv
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Row.Fn.Line < b.Row.Fn.Line
	})
}

// nodeValue is a node's sort key on column c: the group value on an
// aggregate node, the function's matching metric on a leaf.
func (m *Model) nodeValue(n treeNode, c Column) float64 {
	if n.Row.Group != nil {
		v, _ := n.Row.groupValue(c)
		return v
	}
	return float64(m.fnTreeValue(n.Row, c))
}

// fnTreeValue maps the tree layout's aggregate columns onto a function
// row: Σccn / ccn max read the function's CCN, Σcog / cog max its
// cognitive complexity, over whether it exceeds a threshold, excess its
// excess; funcs is 0; the shared columns read as in the grid.
func (m *Model) fnTreeValue(r Row, c Column) int {
	switch c {
	case ColFuncs:
		return 0
	case ColCCNMax:
		return r.Fn.CCN
	case ColCogMax:
		return r.Fn.Cognitive
	case ColOver:
		if m.th(r.Path).Over(r.Fn) {
			return 1
		}
		return 0
	case ColExcess:
		return m.th(r.Path).Excess(r.Fn)
	}
	n, _ := r.intValue(c)
	return n
}

// treeCells renders a tree row's grid cells: the indented name with its
// fold marker, then the aggregate columns (a function leaf shows its
// own metrics in nloc, Σccn, Σcog, cmt%, excess and commits; funcs, the
// maxima and over stay blank; a breakdown row shows its line-less site
// cells).
func (m *Model) treeCells(hr *hiertree.Row[treeNode], layout []Column) []string {
	n := hr.Item
	out := make([]string, len(layout))
	for i, c := range layout {
		if c == ColFunc {
			out[i] = strings.Repeat("  ", hr.Depth()) + hr.Marker() + " " + n.Name
			continue
		}
		switch {
		case n.Row.Group != nil:
			out[i] = n.Row.groupCell(c)
		case n.Row.child():
			out[i] = n.Row.cell(c)
		default:
			out[i] = m.fnTreeCell(n.Row, c)
		}
	}
	return out
}

// fnTreeCell is a function leaf's text in an aggregate column.
func (m *Model) fnTreeCell(r Row, c Column) string {
	switch c {
	case ColFuncs, ColCCNMax, ColCogMax, ColOver, ColHot:
		return ""
	case ColExcess:
		return strconv.Itoa(m.th(r.Path).Excess(r.Fn))
	}
	return r.cell(c)
}

// treeWarn marks the warning cells of a tree row: the group rule on an
// aggregate node, the function rule on a leaf, plus the hot decile.
func (m *Model) treeWarn(n treeNode, layout []Column, cells []gridview.Cell) {
	r := n.Row
	if r.Group != nil {
		for i, c := range layout {
			cells[i].Warn = m.groupWarn(r.Group, c) || c == ColHot && m.hotWarns(r)
		}
		return
	}
	if r.child() {
		return
	}
	accepted := r.Fn.Baseline == complexity.BaselineAccepted
	for i, c := range layout {
		switch c {
		case ColCCN:
			cells[i].Warn = r.Fn.CCN >= m.th(r.Path).CCN && !accepted
			cells[i].Muted = r.Fn.CCN >= m.th(r.Path).CCN && accepted
		case ColCog:
			cells[i].Warn = r.Fn.Cognitive >= m.th(r.Path).Cog && !accepted
			cells[i].Muted = r.Fn.Cognitive >= m.th(r.Path).Cog && accepted
		case ColCmt:
			cells[i].Warn = m.th(r.Path).CommentsOver(r.Fn)
		case ColExcess:
			cells[i].Warn = m.th(r.Path).Excess(r.Fn) > 0 && !accepted
		}
	}
}

// renderTree draws the tree scrolled around its cursor, one grid row per
// visible node; an empty tree shows the empty notice.
func (m *Model) renderTree(pal *theme.Palette, height int) string {
	rows, top := m.tree.Window(height)
	empty := lipgloss.NewStyle().Faint(true).Render(" " + m.emptyText())
	layout := m.layout()
	return ui.RenderWindow(top, height, len(rows), empty, func(i int) string {
		texts := m.treeCells(rows[i], layout)
		cells := make([]gridview.Cell, len(texts))
		for c, t := range texts {
			cells[c] = gridview.Cell{Text: t}
		}
		m.treeWarn(rows[i].Item, layout, cells)
		return m.dataRow(cells, i == m.tree.Cursor())
	})
}

// treeWidths sizes the tree layout's columns to the visible rows; the
// name column shares the text budget like the grid's (#2992).
func (m *Model) treeWidths(ws []int) []int {
	layout := m.layout()
	for _, r := range m.tree.Visible() {
		for i, c := range m.treeCells(r, layout) {
			if w := lipgloss.Width(c); w > ws[i] {
				ws[i] = w
			}
		}
	}
	return m.fitWidths(ws)
}

// markTreeHot computes the hot column's warning threshold over the
// visible aggregate nodes (markHot's rule on the tree).
func (m *Model) markTreeHot() {
	m.hotWarn = 0
	if m.layoutIndex(ColHot) < 0 {
		return
	}
	var scores []int
	for _, r := range m.tree.Visible() {
		if r.Item.Row.Group != nil {
			scores = append(scores, r.Item.Row.Hot)
		}
	}
	if len(scores) == 0 {
		return
	}
	sort.Sort(sort.Reverse(sort.IntSlice(scores)))
	m.hotWarn = scores[(len(scores)+9)/10-1]
}

// TreeRows exposes the visible tree nodes' rows in display order (tests).
func (m *Model) TreeRows() []Row {
	vis := m.tree.Visible()
	out := make([]Row, len(vis))
	for i, r := range vis {
		out[i] = r.Item.Row
	}
	return out
}

// TreeNames renders the visible tree as indented names (tests).
func (m *Model) TreeNames() []string {
	vis := m.tree.Visible()
	out := make([]string, len(vis))
	for i, r := range vis {
		out[i] = strings.Repeat("  ", r.Depth()) + r.Item.Name
	}
	return out
}

// treeSelected is the row of the node under the tree cursor.
func (m *Model) treeSelected() (Row, bool) {
	cur := m.tree.Current()
	if cur == nil {
		return Row{}, false
	}
	return cur.Item.Row, true
}

// treeKeyPress routes one key to the tree: enter opens a function (or a
// breakdown row's line) and toggles any other node, l / right / space
// expand, h / left collapse or climb to the parent, tab cycles the tab;
// the list navigation keys move the cursor. Keys the tree does not own
// come back unhandled for the grid's switch.
func (m *Model) treeKeyPress(key string) (tea.Cmd, bool) {
	m.tree.SetFetch(m.treeFetch())
	if key == "space" {
		m.ToggleSites()
		return nil, true
	}
	cmd, ok := m.tree.Key(key, m.bodyHeight(),
		func(hiertree.Row[treeNode]) tea.Cmd { return m.treeEnter() },
		func() tea.Cmd { m.cycleTab(1); return nil })
	m.fitTree()
	return cmd, ok
}

// fitTree re-measures the columns after the visible rows changed (an
// expansion or a fold), so a deeper name never clips.
func (m *Model) fitTree() {
	if m.treeActive() {
		m.widths = m.columnWidths()
	}
}

// treeEnter is enter / a double click on the tree: a function opens, a
// breakdown row opens its line or selects its range, any other node
// toggles its expansion.
func (m *Model) treeEnter() tea.Cmd {
	cur := m.tree.Current()
	if cur == nil {
		return nil
	}
	r := cur.Item.Row
	switch {
	case r.Cand != nil:
		return m.openCandidate(r)
	case r.Site != nil:
		return m.openSite(r)
	case r.Group != nil:
		return m.tree.Toggle(cur)
	}
	msg := OpenLocationMsg{Path: r.Path, Line: r.Fn.Line}
	return func() tea.Msg { return msg }
}

// toggleTreeSites is ToggleSites on the tree: a function with sites
// toggles its breakdown, a breakdown row collapses its parent, an
// aggregate node toggles its expansion; a function without sites reports
// false.
func (m *Model) toggleTreeSites() bool {
	m.tree.SetFetch(m.treeFetch())
	cur := m.tree.Current()
	if cur == nil {
		return false
	}
	switch cur.Item.Kind {
	case treeChild:
		m.tree.Key("left", m.bodyHeight(), nil, nil) // land on the parent
		if p := m.tree.Current(); p != nil {
			m.tree.Collapse(p)
		}
		m.fitTree()
		return true
	case treeFn:
		if len(cur.Item.children) == 0 {
			return false
		}
	}
	m.tree.Toggle(cur)
	m.fitTree()
	return true
}

// treeClick selects the tree row under content-local y and, on a double
// click, acts as enter does.
func (m *Model) treeClick(y int) tea.Cmd {
	m.tree.SetFetch(m.treeFetch())
	rows, top := m.tree.Window(m.bodyHeight())
	cursor := m.tree.Cursor()
	cmd := m.clicks.ClickRow(y, top, m.headerRows(), m.bodyHeight(), len(rows), m.now(), &cursor,
		func(int) tea.Cmd { m.tree.SetCursor(cursor); return m.treeEnter() })
	m.tree.SetCursor(cursor)
	m.fitTree()
	return cmd
}

// treeFooter is the key hint line of the tree view.
func (m *Model) treeFooter() string {
	return " · tab view · v grid · s sort · S reverse · enter open/fold · l/h unfold/fold · e csv · E md · y copy · r rescan · / filter · space sites · m marks · i accept"
}

// treeCSVHeader is the tree export's first row: the path leads, so a
// directory, file, class or function row stays self-describing.
var treeCSVHeader = []string{"path", "kind", "name", "functions", "nloc", "ccn_sum", "ccn_max",
	"cog_sum", "cog_max", "comment_lines", "cmt_pct", "over_threshold", "excess"}

// treeCSV renders the visible tree rows (breakdown rows left out): an
// aggregate node's group numbers, a function's own metrics in the
// matching columns. Inside a repository commits and hot follow (#2957).
func (m *Model) treeCSV() string {
	var b strings.Builder
	hot := m.layoutIndex(ColHot) >= 0
	header := treeCSVHeader
	if hot {
		header = append(header[:len(header):len(header)], "commits", "hot")
	}
	writeCSVRow(&b, header)
	for _, hr := range m.tree.Visible() {
		n := hr.Item
		r := n.Row
		var fields []string
		if g := r.Group; g != nil {
			fields = []string{r.Rel, n.Kind.String(), n.Name, itoa(g.Functions), itoa(g.NLOC),
				itoa(g.CCNSum), itoa(g.CCNMax), itoa(g.CogSum), itoa(g.CogMax),
				itoa(g.CommentLines), itoa(complexity.DensityPercent(g.CommentDensity)), itoa(g.OverThreshold), itoa(g.Excess)}
			if hot {
				fields = append(fields, itoa(r.Commits), itoa(r.Hot))
			}
		} else if !r.child() {
			f := r.Fn
			fields = []string{r.Rel, n.Kind.String(), f.Name, "", itoa(f.NLOC),
				itoa(f.CCN), "", itoa(f.Cognitive), "",
				itoa(f.CommentLines), itoa(cmtOf(f)), "", itoa(m.th(r.Path).Excess(f))}
			if hot {
				fields = append(fields, itoa(r.Commits), "")
			}
		} else {
			continue
		}
		writeCSVRow(&b, fields)
	}
	return b.String()
}
