package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
)

// sites_test.go covers the per-site breakdown (#2946): space expands a
// function row into its sites and collapses it again, site rows open their
// line and copy their cost, the expansion survives a refresh and a sort,
// and a grouped tab shows none.

// sitesView is the sample snapshot with sites on render and ListNav.
func sitesView() complexity.View {
	v := sample()
	render := &v.Files["/proj/internal/app/app.go"][0]
	render.Sites = []complexity.Site{
		{Line: 2, Col: 1, EndLine: 10, Kind: "if", CCN: 1, Cognitive: 1},
		{Line: 2, Col: 6, EndLine: 2, Kind: "&&", CCN: 2, Cognitive: 1},
		{Line: 4, Col: 2, EndLine: 8, Kind: "switch", Cognitive: 2, Nesting: 1, Level: 1},
		{Line: 5, Col: 2, EndLine: 6, Kind: "case", CCN: 1, Level: 2},
		{Line: 6, Col: 3, EndLine: 6, Kind: "if", CCN: 1, Cognitive: 3, Nesting: 2, Level: 2},
		{Line: 12, Col: 8, EndLine: 12, Kind: "recursion", Cognitive: 1},
	}
	listNav := &v.Files["/proj/internal/ui/list.go"][0]
	listNav.Sites = []complexity.Site{{Line: 7, Col: 1, EndLine: 9, Kind: "for", CCN: 1, Cognitive: 1}}
	return v
}

func sitesPanel(t *testing.T) *Model {
	t.Helper()
	m := newPanel(t)
	m.Set(sitesView(), complexity.Stats{Enabled: true})
	return m
}

// funcCells lists the function column of every row.
func funcCells(m *Model) []string {
	out := make([]string, len(m.Rows()))
	for i, r := range m.Rows() {
		out[i] = r.cell(ColFunc)
	}
	return out
}

func TestSpaceExpandsAndCollapsesSites(t *testing.T) {
	m := sitesPanel(t)
	if got := strings.Join(names(m.Rows()), ","); got != "render,Page::show,ListNav,inner,tiny" {
		t.Fatalf("rows = %s", got)
	}
	if !strings.Contains(m.View(), "space sites") {
		t.Fatalf("footer:\n%s", m.View())
	}
	m.Update(key(" "))
	want := "render,  if +3 (nesting 2),  switch +2 (nesting 1),  if +1,  && +1,  recursion +1,  case,Page::show,ListNav,inner,tiny"
	if got := strings.Join(funcCells(m), ","); got != want {
		t.Fatalf("expanded rows =\n%s\nwant\n%s", got, want)
	}
	if r, ok := m.Expanded(); !ok || r.Fn.Name != "render" {
		t.Fatalf("Expanded = %+v %v", r, ok)
	}
	rows := m.Rows()
	// The children carry their parent's index and the site's own cells.
	if rows[1].Site == nil || rows[1].Parent != 0 || rows[1].cell(ColLine) != "7" || rows[1].cell(ColCCN) != "1" || rows[1].cell(ColCog) != "3" || rows[1].cell(ColFile) != "" || rows[1].cell(ColNLOC) != "" {
		t.Errorf("site row = %+v cells %q %q %q", rows[1], rows[1].cell(ColLine), rows[1].cell(ColCCN), rows[1].cell(ColCog))
	}
	if rows[6].cell(ColCCN) != "1" || rows[6].cell(ColCog) != "0" {
		t.Errorf("arm cells = %q %q", rows[6].cell(ColCCN), rows[6].cell(ColCog))
	}
	// The totals and the row count stay the functions'.
	if m.Total() != 5 || m.Summary().Functions != 5 {
		t.Errorf("total %d functions %d", m.Total(), m.Summary().Functions)
	}
	if v := m.View(); !strings.Contains(v, "if +3 (nesting 2)") || !strings.Contains(v, "11/5 rows") {
		t.Errorf("view:\n%s", v)
	}
	// Space on the parent collapses.
	m.Update(key(" "))
	if got := strings.Join(names(m.Rows()), ","); got != "render,Page::show,ListNav,inner,tiny" {
		t.Fatalf("collapsed rows = %s", got)
	}
	if _, ok := m.Expanded(); ok {
		t.Error("still expanded")
	}
	// One expanded function at a time: expanding ListNav closes render.
	m.Update(key(" "))
	m.Update(key("G"))
	m.Update(key("k"))
	m.Update(key("k"))
	if r, _ := m.Selected(); r.Fn.Name != "ListNav" {
		t.Fatalf("selected %s", r.Fn.Name)
	}
	m.Update(key(" "))
	if got := strings.Join(funcCells(m), ","); got != "render,Page::show,ListNav,  for +1,inner,tiny" {
		t.Fatalf("rows = %s", got)
	}
	// Space on a site row collapses its parent and selects it.
	m.Update(key("j"))
	m.Update(key(" "))
	if r, _ := m.Selected(); r.Fn.Name != "ListNav" || r.Site != nil || len(m.Rows()) != 5 {
		t.Errorf("after collapse: selected %s site %v rows %d", r.Fn.Name, r.Site, len(m.Rows()))
	}
	// A function without sites does not expand.
	m.Update(key("G"))
	if m.ToggleSites() || len(m.Rows()) != 5 {
		t.Errorf("tiny expanded: %v", names(m.Rows()))
	}
}

func TestSiteRowOpensLineAndCopies(t *testing.T) {
	m := sitesPanel(t)
	m.Update(key(" "))
	m.Update(key("j"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter yields no command")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "/proj/internal/app/app.go" || msg.Line != 6 {
		t.Fatalf("open = %+v", msg)
	}
	cp, ok := m.Update(key("y"))().(CopyMsg)
	if !ok || cp.Text != "internal/app/app.go:7: if +3 (nesting 2)" {
		t.Fatalf("copy = %+v", cp)
	}
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("j"))
	m.Update(key("j"))
	if cp := m.Update(key("y"))().(CopyMsg); cp.Text != "internal/app/app.go:6: case" {
		t.Errorf("arm copy = %q", cp.Text)
	}
	// The CSV export lists the functions only.
	if csv := m.CSV(); strings.Contains(csv, "nesting") || strings.Count(csv, "\n") != 6 {
		t.Errorf("csv:\n%s", csv)
	}
}

func TestSitesSurviveRefreshSortAndFilter(t *testing.T) {
	m := sitesPanel(t)
	m.Update(key(" "))
	m.Update(key("j"))
	m.Update(key("j"))
	// A new snapshot that still holds render keeps the expansion and the
	// cursor on the same site.
	v := sitesView()
	v.Gen = 4
	m.Set(v, complexity.Stats{Enabled: true})
	if r, ok := m.Expanded(); !ok || r.Fn.Name != "render" {
		t.Fatalf("expansion lost: %+v %v", r, ok)
	}
	if r, _ := m.Selected(); r.Site == nil || r.Site.Kind != "switch" {
		t.Errorf("cursor moved to %+v", r)
	}
	// The sort moves the parent and its children together.
	m.SetSort(ColFunc)
	if got := strings.Join(funcCells(m), ","); got != "ListNav,Page::show,inner,render,  if +3 (nesting 2),  switch +2 (nesting 1),  if +1,  && +1,  recursion +1,  case,tiny" {
		t.Fatalf("sorted rows = %s", got)
	}
	// A filter that drops the parent drops the children; lifting it brings
	// them back.
	m.filter.SetText("fn:ListNav")
	m.Refresh()
	if got := strings.Join(names(m.Rows()), ","); got != "ListNav" {
		t.Fatalf("filtered rows = %s", got)
	}
	m.filter.SetText("")
	m.Refresh()
	if len(m.Rows()) != 11 {
		t.Errorf("rows after the filter = %d", len(m.Rows()))
	}
	// A snapshot without the function forgets nothing but shows nothing.
	m.Set(sample(), complexity.Stats{Enabled: true})
	if len(m.Rows()) != 5 {
		t.Errorf("rows without sites = %d", len(m.Rows()))
	}
	// Grouped tabs show no site rows; ungrouping restores them.
	m.Set(sitesView(), complexity.Stats{Enabled: true})
	m.Update(key("b"))
	for _, r := range m.Rows() {
		if r.Site != nil {
			t.Fatalf("site row on a grouped tab: %+v", r)
		}
	}
	m.Update(key("b"))
	m.Update(key("b"))
	if len(m.Rows()) != 11 {
		t.Errorf("rows after ungrouping = %d", len(m.Rows()))
	}
}

func TestSitesOnFileAndChangesTabs(t *testing.T) {
	m := sitesPanel(t)
	m.Follow("/proj/internal/app/app.go", 5)
	m.SetTab(TabFile)
	m.Update(key(" "))
	rows := m.Rows()
	if len(rows) != 8 || rows[0].Caret != true || rows[1].Caret || rows[1].cell(ColLine) != "7" {
		t.Fatalf("File tab rows = %+v", funcCells(m))
	}
	// The Changes tab expands the after side; a removed function has none.
	c := sampleChanges()
	head := map[string][]complexity.Function{
		"/proj/internal/app/app.go": {fn("render", 0, 24, 15, 20), fn("tiny", 34, 36, 3, 3), fn("fresh", 40, 44, 2, 1)},
	}
	head["/proj/internal/app/app.go"][0].Sites = []complexity.Site{{Line: 3, Col: 1, EndLine: 5, Kind: "for", CCN: 1, Cognitive: 1}}
	base := map[string][]complexity.Function{
		"/proj/internal/app/app.go": {fn("render", 0, 20, 10, 12), fn("tiny", 30, 32, 4, 3)},
		"/proj/internal/ui/old.go":  {fn("gone", 0, 9, 6, 2)},
	}
	base["/proj/internal/ui/old.go"][0].Sites = []complexity.Site{{Line: 1, Col: 1, EndLine: 2, Kind: "if", CCN: 1, Cognitive: 1}}
	c.Diff = complexity.DiffProject(base, head, complexity.Uniform(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 40}))
	m.SetChanges(c)
	m.SetTab(TabChanges)
	m.Update(key(" "))
	if got := strings.Join(funcCells(m), ","); got != "render*,  for +1,tiny*,fresh*,gone" {
		t.Fatalf("Changes rows = %s", got)
	}
	if r := m.Rows()[1]; r.cell(ColCCN) != "1" || r.cell(ColChange) != "" {
		t.Errorf("site row on Changes = %q %q", r.cell(ColCCN), r.cell(ColChange))
	}
	m.Update(key("G"))
	if m.ToggleSites() {
		t.Error("a removed function expanded")
	}
}

// TestMShowsSitesInEditor: 'm' on a function row (or one of its site
// rows) asks the root model to paint that function's sites (#2947); a
// group row has none to show.
func TestMShowsSitesInEditor(t *testing.T) {
	m := sitesPanel(t)
	cmd := m.Update(key("m"))
	if cmd == nil {
		t.Fatal("m on a function row must emit ShowSitesMsg")
	}
	msg, ok := cmd().(ShowSitesMsg)
	if !ok || msg.Path != "/proj/internal/app/app.go" || msg.Line != m.Rows()[0].Fn.Line {
		t.Fatalf("msg = %+v (ok=%v)", msg, ok)
	}
	m.Update(key(" "))
	m.Update(key("j"))
	if r, _ := m.Selected(); r.Site == nil {
		t.Fatal("the cursor must sit on a site row")
	}
	if got, ok := m.ShowSites(); !ok || got != msg {
		t.Fatalf("on a site row: %+v (ok=%v), want the parent's %+v", got, ok, msg)
	}
	if !strings.Contains(m.View(), "m marks") {
		t.Fatalf("footer must name m:\n%s", m.View())
	}
	m.CycleGroup()
	if _, ok := m.ShowSites(); ok {
		t.Fatal("a group row has no sites to show")
	}
}
