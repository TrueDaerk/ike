package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
)

// changesgroup_test.go covers the Changes tab's per-file grouping (#2955):
// 'b' cycling none → file, the file rows' cells, the sort and the filter on
// |Δ|, open, copy, export and the persisted mode.

func fileLabels(rows []Row) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.label()
	}
	return strings.Join(out, ",")
}

func TestChangesGroupByFileRows(t *testing.T) {
	m := changesPanel(t)
	m.Update(key("b"))
	if g := m.Grouping(); g != complexity.ByFile {
		t.Fatalf("b on the Changes tab: grouping = %v, want file", g)
	}
	rows := m.Rows()
	if got := fileLabels(rows); got != "internal/app/app.go,internal/ui/old.go" {
		t.Fatalf("file rows = %q", got)
	}
	app, old := rows[0], rows[1]
	for col, want := range map[Column]string{
		ColFile: "internal/app/app.go*", ColChangedN: "2", ColAddedN: "1", ColRemovedN: "0",
		ColCCN: "14→20 (+6)", ColCog: "15→24 (+9)", ColNLOC: "24→33", ColExcess: "0→5 (+5)",
	} {
		if got := app.cell(col); got != want {
			t.Errorf("app.go %s = %q, want %q", columnLabels[col], got, want)
		}
	}
	if old.cell(ColRemovedN) != "1" || old.cell(ColCCN) != "6→0 (-6)" || old.cell(ColNLOC) != "10→0" {
		t.Errorf("old.go cells = %q %q %q", old.cell(ColRemovedN), old.cell(ColCCN), old.cell(ColNLOC))
	}
	// The section column disappears; the group columns show.
	if m.layoutIndex(ColChange) >= 0 || m.layoutIndex(ColFunc) >= 0 {
		t.Errorf("layout = %v", m.layout())
	}
	m.SetSize(200, 12)
	v := m.View()
	for _, want := range []string{"changed", "added", "removed", "excess", "b group: file", "2/2 rows"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	// A positive delta takes the warning colour, a negative one does not.
	if !(fileChangeDelta(app.FileChange, ColCCN) > 0) || fileChangeDelta(old.FileChange, ColCCN) > 0 {
		t.Error("warning sign of the deltas")
	}
	// b again: back to the unit rows.
	m.Update(key("b"))
	if m.Grouping() != complexity.ByNone || len(m.Rows()) != 4 {
		t.Fatalf("second b: grouping %v, %d rows", m.Grouping(), len(m.Rows()))
	}
}

func TestChangesGroupSortAndFilter(t *testing.T) {
	m := changesPanel(t)
	m.CycleGroup()
	// s cycles the group columns: ccn → cog.
	m.Update(key("s"))
	if col, desc := m.SortColumn(); col != ColCog || !desc {
		t.Fatalf("s: sort = %v desc %v", col, desc)
	}
	if got := fileLabels(m.Rows()); got != "internal/app/app.go,internal/ui/old.go" {
		t.Fatalf("|Δcog| desc = %q", got)
	}
	m.Update(key("S"))
	if got := fileLabels(m.Rows()); got != "internal/ui/old.go,internal/app/app.go" {
		t.Fatalf("S reverses: %q", got)
	}
	m.SetSort(ColRemovedN)
	if got := fileLabels(m.Rows()); got != "internal/ui/old.go,internal/app/app.go" {
		t.Fatalf("removed desc = %q", got)
	}
	for _, c := range []struct{ q, want string }{
		{"file:ui/", "internal/ui/old.go"},
		{"metric:cog min:3", "internal/app/app.go"},
		{"metric:cog max:3", "internal/ui/old.go"},
		{"metric:ccn min:7", ""},
		{"-file:app", "internal/ui/old.go"},
	} {
		m.filter.SetText(c.q)
		m.Refresh()
		if got := fileLabels(m.Rows()); got != c.want {
			t.Errorf("filter %q = %q, want %q", c.q, got, c.want)
		}
	}
	m.filter.SetText("")
	m.Refresh()
	// A header click sorts by the clicked group column: the file column
	// starts at x 1.
	m.Click(1, m.headerRows()-1)
	if col, _ := m.SortColumn(); col != ColFile {
		t.Errorf("header click sorts by %v", col)
	}
}

func TestChangesGroupOpenCopyExport(t *testing.T) {
	m := changesPanel(t)
	m.CycleGroup()
	cp, _ := m.Update(key("y"))().(CopyMsg)
	if want := "internal/app/app.go: changed 2 added 1 removed 0 ccn 14→20 (+6) cog 15→24 (+9)"; cp.Text != want {
		t.Errorf("copy = %q, want %q", cp.Text, want)
	}
	ex, _ := m.Update(key("e"))().(ExportMsg)
	lines := strings.Split(strings.TrimSpace(ex.CSV), "\n")
	if ex.Label != "Changes" || len(lines) != 3 ||
		lines[0] != "file,changed,added,removed,ccn_before,ccn_after,ccn_delta,cog_before,cog_after,cog_delta,nloc_before,nloc_after" ||
		lines[1] != "internal/app/app.go,2,1,0,14,20,6,15,24,9,24,33" ||
		lines[2] != "internal/ui/old.go,0,0,1,6,0,-6,2,0,-2,10,0" {
		t.Errorf("export = %q\n%s", ex.Label, ex.CSV)
	}
	// enter opens the file at its first changed function.
	base := map[string][]complexity.Function{"/proj/a.go": {fn("keep", 0, 5, 1, 0), fn("grow", 30, 40, 2, 1)}}
	head := map[string][]complexity.Function{"/proj/a.go": {fn("keep", 0, 5, 1, 0), fn("grow", 30, 40, 5, 4), fn("new", 50, 55, 1, 0)}}
	m.SetChanges(Changes{Rev: "HEAD", Diff: complexity.DiffProject(base, head, 15, 15, 40), Ready: true})
	msg := m.Update(key("enter"))().(OpenLocationMsg)
	if msg.Path != "/proj/a.go" || msg.Line != 30 {
		t.Errorf("enter = %+v, want a.go line 30", msg)
	}
	// Sites and marks have nothing to show on a file row.
	if m.ToggleSites() {
		t.Error("space expanded a file row")
	}
	if _, ok := m.ShowSites(); ok {
		t.Error("m on a file row")
	}
}

func TestChangesGroupMode(t *testing.T) {
	m := changesPanel(t)
	m.CycleGroup()
	if got := m.Mode(); got != "changes:file" {
		t.Fatalf("Mode = %q", got)
	}
	n := newPanel(t)
	n.SetChanges(sampleChanges())
	n.SetMode("project:class,changes:file")
	n.SetTab(TabChanges)
	if n.Grouping() != complexity.ByFile || len(n.Rows()) != 2 || n.Rows()[0].FileChange == nil {
		t.Fatalf("restored: grouping %v, rows %d", n.Grouping(), len(n.Rows()))
	}
	// Only the file level applies to the Changes tab.
	n.SetMode("changes:class")
	if n.Grouping() != complexity.ByNone {
		t.Errorf("changes:class restored as %v", n.Grouping())
	}
}
