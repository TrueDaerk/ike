package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// tree_test.go covers the directory tree view of the Project tab (#2954):
// the view toggle and its persistence, the node shape over a fixture
// snapshot, the per-node aggregates, enter / l / h / space, the filter
// pruning, the copy and CSV export, the refresh keeping expansion and
// selection, and the mouse.

// treePanel is the issue's fixture: a/x.go (two functions, one in a
// class), a/b/y.go and c/z.py.
func treePanel(t *testing.T) *Model {
	t.Helper()
	m := New(theme.DefaultPalette())
	m.pathLang = langByExt
	m.SetSize(200, 14)
	m.SetRoot("/proj")
	m.Set(treeView(), complexity.Stats{Enabled: true})
	return &m
}

func treeView() complexity.View {
	heavy := fn("heavy", 40, 80, 20, 25)
	heavy.Sites = []complexity.Site{{Line: 42, Kind: "if", CCN: 1, Cognitive: 2, Nesting: 1}, {Line: 50, Kind: "for", CCN: 1, Cognitive: 1}}
	return complexity.View{Gen: 1, Files: map[string][]complexity.Function{
		"/proj/a/x.go":   {method("Box", "Open", 1, 10, 4, 3), fn("free", 12, 20, 2, 1)},
		"/proj/a/b/y.go": {heavy},
		"/proj/c/z.py":   {fn("main", 0, 5, 1, 0)},
	}}
}

func expandAll(m *Model) {
	for i := 0; i < 20; i++ {
		rows := m.tree.Visible()
		done := true
		for _, r := range rows {
			if !r.Expanded() && !r.Leaf() && r.Item.Kind != treeFn {
				m.tree.SetFetch(m.treeFetch())
				m.tree.Expand(r)
				done = false
			}
		}
		if done {
			return
		}
	}
}

func TestTreeToggleAndMode(t *testing.T) {
	m := treePanel(t)
	if m.TreeView() || !strings.Contains(m.View(), "v tree") {
		t.Fatalf("the grid must hint the tree:\n%s", m.View())
	}
	m.Update(key("v"))
	if !m.TreeView() || m.Mode() != viewTree {
		t.Fatalf("v: tree %v mode %q", m.TreeView(), m.Mode())
	}
	v := m.View()
	for _, want := range []string{"v grid", "name", "funcs", "Σccn", "ccn max", "Σcog", "cog max", "cmt%", "over", "excess", "▾ proj", "4 fns"} {
		if !strings.Contains(v, want) {
			t.Errorf("tree view lacks %q:\n%s", want, v)
		}
	}
	// The tab bar and the totals are the grid's.
	if !strings.Contains(v, "Project") || !strings.Contains(v, "CCN Σ27") {
		t.Errorf("header changed:\n%s", v)
	}
	m2 := New(theme.DefaultPalette())
	m2.SetMode("project:file,view:tree,hist:on")
	if !m2.TreeView() || !m2.Histogram() || m2.projectGroup != complexity.ByFile {
		t.Fatalf("SetMode: tree %v hist %v group %v", m2.TreeView(), m2.Histogram(), m2.projectGroup)
	}
	if got := m2.Mode(); got != "project:file,hist:on,view:tree" {
		t.Errorf("Mode = %q", got)
	}
	m.Update(key("v"))
	if m.TreeView() || m.Mode() != "" {
		t.Fatalf("second v must return to the grid: %v %q", m.TreeView(), m.Mode())
	}
}

func TestTreeShapeAndAggregates(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	expandAll(m)
	got := strings.Join(m.TreeNames(), "|")
	// Children sort by Σccn descending: a (26) before c (1); inside a,
	// b (20) before x.go (6); inside x.go, Box (4) before free (2).
	want := "proj|  a|    b|      y.go|        heavy|    x.go|      Box|        Box.Open|      free|  c|    z.py|      main"
	if got != want {
		t.Fatalf("tree =\n%s\nwant\n%s", got, want)
	}
	rows := m.TreeRows()
	root := rows[0].Group
	if root == nil || root.Summary != m.Summary() {
		t.Errorf("the root row must equal the tab's totals: %+v vs %+v", root, m.Summary())
	}
	a := rows[1].Group
	fns := []complexity.Function{method("Box", "Open", 1, 10, 4, 3), fn("free", 12, 20, 2, 1), treeView().Files["/proj/a/b/y.go"][0]}
	if a.Summary != complexity.Summarize(fns, complexity.Thresholds{CCN: DefaultWarn, Cog: DefaultWarn, Cmt: DefaultCmtWarn}) {
		t.Errorf("a aggregate = %+v", a.Summary)
	}
	if a.Functions != 3 || a.CCNSum != 26 || a.CCNMax != 20 || a.OverThreshold != 1 || a.Excess != 15 {
		t.Errorf("a = %+v", a)
	}
	if rows[1].Rel != "a" || rows[2].Rel != "a/b" || rows[3].Rel != "a/b/y.go" || rows[6].Group.Class != "Box" || rows[6].Group.Functions != 1 {
		t.Errorf("node rows: %q %q %q class %+v", rows[1].Rel, rows[2].Rel, rows[3].Rel, rows[6].Group)
	}
	if len(rows[1].Members) != 3 || len(rows[5].Members) != 2 {
		t.Errorf("members: a %d, x.go %d", len(rows[1].Members), len(rows[5].Members))
	}
	// The grid's rows stay the function population, the footer counts them.
	if len(m.Rows()) != 4 || !strings.Contains(m.View(), "4/4 rows") {
		t.Errorf("rows %d:\n%s", len(m.Rows()), m.View())
	}
}

func TestTreeRowCells(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	expandAll(m)
	layout := m.layout()
	vis := m.tree.Visible()
	cell := func(i int, c Column) string {
		cells := m.treeCells(vis[i], layout)
		return cells[m.layoutIndex(c)]
	}
	if cell(1, ColFunc) != "  ▾ a" || cell(1, ColFuncs) != "3" || cell(1, ColCCN) != "26" || cell(1, ColCCNMax) != "20" || cell(1, ColExcess) != "15" {
		t.Errorf("a cells: %q %q %q %q %q", cell(1, ColFunc), cell(1, ColFuncs), cell(1, ColCCN), cell(1, ColCCNMax), cell(1, ColExcess))
	}
	// A function leaf: its metrics in nloc / Σccn / Σcog / cmt% / excess,
	// the count, maxima and over blank.
	if cell(4, ColFunc) != "        ▸ heavy" || cell(4, ColFuncs) != "" || cell(4, ColCCN) != "20" || cell(4, ColCCNMax) != "" ||
		cell(4, ColCog) != "25" || cell(4, ColNLOC) != "41" || cell(4, ColOver) != "" || cell(4, ColExcess) != "15" {
		t.Errorf("heavy cells: %q %q %q %q %q %q %q %q", cell(4, ColFunc), cell(4, ColFuncs), cell(4, ColCCN), cell(4, ColCCNMax), cell(4, ColCog), cell(4, ColNLOC), cell(4, ColOver), cell(4, ColExcess))
	}
	// A function without sites that was expanded is a leaf (·).
	if cell(11, ColFunc) != "      ▸ main" {
		t.Errorf("main cell %q", cell(11, ColFunc))
	}
}

func TestTreeEnterOpensAndToggles(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	// Root expanded only: proj, a, c.
	if got := strings.Join(m.TreeNames(), "|"); got != "proj|  a|  c" {
		t.Fatalf("initial tree %q", got)
	}
	m.Update(key("down"))
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Fatal("enter on a directory must not open anything")
	}
	if got := strings.Join(m.TreeNames(), "|"); got != "proj|  a|    b|    x.go|  c" {
		t.Fatalf("enter on a: %q", got)
	}
	m.Update(key("down")) // b
	m.Update(key("l"))
	m.Update(key("down")) // y.go
	m.Update(key("l"))
	m.Update(key("down")) // heavy
	r, ok := m.Selected()
	if !ok || r.Fn.Name != "heavy" {
		t.Fatalf("selected %+v %v", r, ok)
	}
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a function must open it")
	}
	if msg, ok := cmd().(OpenLocationMsg); !ok || msg.Path != "/proj/a/b/y.go" || msg.Line != 40 {
		t.Fatalf("open = %+v", cmd())
	}
	// space expands the breakdown; enter on a site opens its line.
	if !m.ToggleSites() {
		t.Fatal("space on a function with sites must expand it")
	}
	if got := strings.Join(m.TreeNames(), "|"); !strings.Contains(got, "heavy|          if +2 (nesting 1)|          for +1") {
		t.Fatalf("breakdown: %q", got)
	}
	m.Update(key("down"))
	cmd = m.Update(key("enter"))
	if msg, ok := cmd().(OpenLocationMsg); !ok || msg.Line != 42 {
		t.Fatalf("site open = %+v", cmd())
	}
	// space on a site row collapses the parent and lands on it.
	m.ToggleSites()
	if r, _ := m.Selected(); r.Fn.Name != "heavy" || r.Site != nil || strings.Contains(strings.Join(m.TreeNames(), "|"), "if +2") {
		t.Fatalf("collapse via space: %+v %q", r, m.TreeNames())
	}
	// h collapses, a second h climbs to the parent; tab still cycles tabs.
	m.Update(key("h"))
	m.Update(key("h"))
	if r, _ := m.Selected(); r.Rel != "a/b/y.go" {
		t.Fatalf("h must climb to the file: %q", r.Rel)
	}
	m.Update(key("tab"))
	if m.Tab() != TabFile || m.treeActive() {
		t.Fatalf("tab must cycle to File: %v", m.Tab())
	}
	m.Update(key("shift+tab"))
	if m.Tab() != TabProject || !m.treeActive() {
		t.Fatal("back to the Project tab, the tree is still on")
	}
	// main has no sites: space reports false, l makes it a leaf.
	m = treePanel(t)
	m.Update(key("v"))
	expandAll(m)
	m.tree.SetCursor(len(m.tree.Visible()) - 1)
	if m.ToggleSites() {
		t.Fatal("a function without sites has nothing to expand")
	}
}

func TestTreeFilterPrunes(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	expandAll(m)
	m.filter.SetText("file:c/")
	m.Refresh()
	if got := strings.Join(m.TreeNames(), "|"); got != "proj|  c|    z.py|      main" {
		t.Fatalf("filter file:c/: %q", got)
	}
	root := m.TreeRows()[0].Group
	if root.Functions != 1 || root.CCNSum != 1 {
		t.Errorf("the root aggregates the kept leaves: %+v", root)
	}
	if !strings.Contains(m.View(), "1/4 rows") {
		t.Errorf("footer:\n%s", m.View())
	}
	// min: reads the sort column on the leaves.
	m.filter.SetText("min:20")
	m.Refresh()
	if got := strings.Join(m.TreeNames(), "|"); got != "proj|  a|    b|      y.go|        heavy" {
		t.Fatalf("min:20: %q", got)
	}
	m.filter.SetText("nomatch")
	m.Refresh()
	if len(m.TreeRows()) != 0 || !strings.Contains(m.View(), "(no row matches the filter)") {
		t.Fatalf("empty tree:\n%s", m.View())
	}
}

func TestTreeSortCycles(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	expandAll(m)
	m.Update(key("S"))
	if got := strings.Join(m.TreeNames()[:2], "|"); got != "proj|  c" {
		t.Fatalf("S reverses: %q", got)
	}
	m.Update(key("S"))
	m.Update(key("s")) // Σccn → ccn max
	if col, _ := m.SortColumn(); col != ColCCNMax || !strings.Contains(m.View(), "ccn max ▼") {
		t.Fatalf("s cycles to ccn max: %v\n%s", col, m.View())
	}
	m.SetSort(ColFunc)
	if got := strings.Join(m.TreeNames(), "|"); !strings.HasPrefix(got, "proj|  a|    b|      y.go|        heavy|    x.go|      Box|        Box.Open|      free|  c") {
		t.Fatalf("name sort: %q", got)
	}
	// Grouping has no effect on the tree.
	if !m.CycleGroup() || m.Grouping() != complexity.ByNone || m.note == "" {
		t.Errorf("b in the tree: group %v note %q", m.Grouping(), m.note)
	}
}

func TestTreeCopyAndCSV(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	m.Update(key("down"))
	cmd := m.Update(key("y"))
	if msg, ok := cmd().(CopyMsg); !ok || msg.Text != "a: funcs=3 ccn=26 (max 20) cog=29 (max 25)" {
		t.Fatalf("copy = %+v", cmd())
	}
	cmd = m.Update(key("e"))
	exp := cmd().(ExportMsg)
	lines := strings.Split(strings.TrimSpace(exp.CSV), "\n")
	if exp.Label != "Project" || len(lines) != 4 {
		t.Fatalf("export = %+v", exp)
	}
	if lines[0] != "path,kind,name,functions,nloc,ccn_sum,ccn_max,cog_sum,cog_max,comment_lines,cmt_pct,over_threshold,excess" {
		t.Errorf("header %q", lines[0])
	}
	if lines[1] != ",dir,proj,4,66,27,20,29,25,0,0,1,15" || lines[2] != "a,dir,a,3,60,26,20,29,25,0,0,1,15" || lines[3] != "c,dir,c,1,6,1,1,0,0,0,0,0,0" {
		t.Errorf("rows %q", lines[1:])
	}
	expandAll(m)
	lines = strings.Split(strings.TrimSpace(m.CSV()), "\n")
	if len(lines) != 13 {
		t.Fatalf("expanded export has %d lines:\n%s", len(lines), m.CSV())
	}
	if lines[5] != "a/b/y.go,function,heavy,,41,20,,25,,0,0,,15" || lines[8] != "a/x.go,function,Box.Open,,10,4,,3,,0,0,,0" {
		t.Errorf("function rows %q %q", lines[5], lines[8])
	}
	if lines[7] != "a/x.go,class,Box,1,10,4,4,3,3,0,0,0,0" {
		t.Errorf("class row %q", lines[7])
	}
}

func TestTreeRefreshKeepsExpansionAndSelection(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	for _, k := range []string{"down", "l", "down", "down", "l", "down"} { // a, x.go, Box
		m.Update(key(k))
	}
	if r, _ := m.Selected(); r.Group == nil || r.Group.Class != "Box" {
		t.Fatalf("selected %+v", r)
	}
	before := strings.Join(m.TreeNames(), "|")
	v := treeView()
	v.Gen = 2
	v.Files["/proj/a/x.go"] = []complexity.Function{method("Box", "Open", 1, 10, 9, 8), fn("free", 12, 20, 2, 1), fn("added", 30, 33, 1, 0)}
	m.Set(v, complexity.Stats{Enabled: true})
	after := strings.Join(m.TreeNames(), "|")
	if after != strings.Replace(before, "|      free", "|      free|      added", 1) {
		t.Fatalf("refresh:\n%s\n%s", before, after)
	}
	r, _ := m.Selected()
	if r.Group == nil || r.Group.Class != "Box" || r.Group.CCNSum != 9 {
		t.Fatalf("selection after refresh %+v", r)
	}
	// Leaving and re-entering the tree keeps it too.
	m.Update(key("tab"))
	m.Update(key("shift+tab"))
	if got := strings.Join(m.TreeNames(), "|"); got != after {
		t.Fatalf("tab round trip: %q", got)
	}
}

func TestTreeMouse(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	hr := m.headerRows()
	m.Click(3, hr+1) // a
	if r, _ := m.Selected(); r.Rel != "a" {
		t.Fatalf("click selected %+v", r)
	}
	m.Click(3, hr+1) // double click toggles
	if got := strings.Join(m.TreeNames(), "|"); got != "proj|  a|    b|    x.go|  c" {
		t.Fatalf("double click: %q", got)
	}
	m.SetSize(200, 6) // 2 body rows
	m.Wheel(2)
	if m.tree.Top() != 2 {
		t.Errorf("wheel top = %d", m.tree.Top())
	}
	// A column header click sorts the tree.
	m.SetSize(200, 14)
	x := 1
	for i, w := range m.widths {
		if m.layout()[i] == ColFuncs {
			break
		}
		x += w + 2
	}
	m.Click(x, hr-1)
	if col, _ := m.SortColumn(); col != ColFuncs {
		t.Errorf("header click sort = %v", col)
	}
}

func TestTreeNoPinSection(t *testing.T) {
	m := treePanel(t)
	pins := &complexity.Pins{Pinned: []complexity.Pin{{File: "a/b/y.go", Function: "heavy", CCN: 20, Cognitive: 25}, {File: "gone.go", Function: "x", CCN: 3}}}
	m.SetPins(pins)
	if !m.pinSection {
		t.Fatal("the grid shows the Pinned section")
	}
	m.Update(key("v"))
	expandAll(m)
	if m.pinSection || m.layoutIndex(ColSincePin) >= 0 {
		t.Fatal("the tree has no Pinned section")
	}
	for _, r := range m.TreeRows() {
		if r.Gone {
			t.Fatal("a gone pin must not become a tree node")
		}
	}
	if !strings.Contains(strings.Join(m.TreeNames(), "|"), "heavy"+pinMark) {
		t.Errorf("the pinned leaf keeps its mark: %q", m.TreeNames())
	}
}

func TestTreeWidthsFollowExpansion(t *testing.T) {
	m := treePanel(t)
	m.Update(key("v"))
	before := m.widths[m.layoutIndex(ColFunc)]
	m.Update(key("down"))
	m.Update(key("l")) // a: b and x.go appear one level deeper
	m.Update(key("down"))
	m.Update(key("l")) // b: y.go
	if after := m.widths[m.layoutIndex(ColFunc)]; after <= before || !strings.Contains(m.View(), "▸ y.go") {
		t.Fatalf("name column %d → %d after expanding:\n%s", before, after, m.View())
	}
}
