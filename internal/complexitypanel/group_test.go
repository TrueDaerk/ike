package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// group_test.go covers the grouping of the Project and File tabs (#2937).

func method(class, name string, line, end, ccn, cog int) complexity.Function {
	f := fn(class+"."+name, line, end, ccn, cog)
	f.Class, f.Kind = class, complexity.KindMethod
	return f
}

// groupPanel holds two files, two classes and free functions: ΣCCN per
// class Client 16, Server 11; per file client.py 17, server.go 14.
func groupPanel(t *testing.T) *Model {
	t.Helper()
	m := New(theme.DefaultPalette())
	m.pathLang = langByExt
	m.SetSize(200, 12)
	m.SetRoot("/proj")
	m.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{
		"/proj/a/server.go": {method("Server", "Handle", 10, 30, 10, 12), method("Server", "Close", 32, 34, 1, 0), fn("helper", 40, 45, 3, 2)},
		"/proj/b/client.py": {method("Client", "send", 5, 30, 16, 20), fn("main", 40, 42, 1, 0)},
	}}, complexity.Stats{Enabled: true})
	return &m
}

func labels(rows []Row) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.label()
	}
	return strings.Join(out, ",")
}

func TestGroupCycleProjectTab(t *testing.T) {
	m := groupPanel(t)
	whole := m.Summary()
	m.Update(key("b"))
	if m.Grouping() != complexity.ByClass || labels(m.Rows()) != "Client,Server" || m.Total() != 2 {
		t.Fatalf("class level: %v rows %q total %d", m.Grouping(), labels(m.Rows()), m.Total())
	}
	g := m.Rows()[1].Group
	if g.Functions != 2 || g.CCNSum != 11 || g.CCNMax != 10 || g.CogSum != 12 || g.CCNAvg != 5.5 {
		t.Errorf("Server group = %+v", g)
	}
	if m.Summary() != whole {
		t.Errorf("the header summary must stay the whole tab's: %+v vs %+v", m.Summary(), whole)
	}
	v := m.View()
	for _, want := range []string{"class", "funcs", "Σccn", "ccn avg", "ccn max", "Σcog", "cog max", "over", "5.5", "b group: class", "5 fns"} {
		if !strings.Contains(v, want) {
			t.Errorf("class view lacks %q:\n%s", want, v)
		}
	}
	m.Update(key("b"))
	if m.Grouping() != complexity.ByFile || labels(m.Rows()) != "b/client.py,a/server.go" {
		t.Fatalf("file level: %v rows %q", m.Grouping(), labels(m.Rows()))
	}
	if strings.Contains(m.headerLabels()[1], "class") {
		t.Error("the file level must not show a class column")
	}
	m.Update(key("b"))
	if m.Grouping() != complexity.ByNone || len(m.Rows()) != 5 || m.Rows()[0].Group != nil {
		t.Fatalf("back to none: %v, %d rows", m.Grouping(), len(m.Rows()))
	}
}

func TestGroupCycleFileTab(t *testing.T) {
	m := groupPanel(t)
	m.Follow("/proj/a/server.go", 11)
	m.SetTab(TabFile)
	m.Update(key("b"))
	if m.Grouping() != complexity.ByClass || labels(m.Rows()) != "Server" {
		t.Fatalf("File tab class level: %v rows %q", m.Grouping(), labels(m.Rows()))
	}
	if m.layoutIndex(ColFile) >= 0 || m.CaretRow() != -1 {
		t.Error("File tab groups hide the file column and mark no caret row")
	}
	m.Update(key("b"))
	if m.Grouping() != complexity.ByNone || len(m.Rows()) != 3 {
		t.Fatalf("File tab skips the file level: %v, %d rows", m.Grouping(), len(m.Rows()))
	}
	// Each tab keeps its own level; the Changes tab never groups.
	m.Update(key("b"))
	m.SetTab(TabProject)
	if m.Grouping() != complexity.ByNone {
		t.Errorf("Project tab level = %v, want none", m.Grouping())
	}
	m.SetTab(TabChanges)
	if m.CycleGroup() || m.Grouping() != complexity.ByNone {
		t.Error("the Changes tab must not group")
	}
}

func TestGroupSortAndFilter(t *testing.T) {
	m := groupPanel(t)
	m.SetSort(ColLine)
	m.CycleGroup()
	if col, desc := m.SortColumn(); col != ColCCN || !desc {
		t.Fatalf("a hidden sort column resets to CCN: %v %v", col, desc)
	}
	m.SetSort(ColFuncs)
	if labels(m.Rows()) != "Server,Client" {
		t.Errorf("by funcs: %q", labels(m.Rows()))
	}
	m.SetSort(ColCCNAvg)
	if labels(m.Rows()) != "Client,Server" {
		t.Errorf("by ccn avg: %q", labels(m.Rows()))
	}
	m.Update(key("S"))
	if labels(m.Rows()) != "Server,Client" {
		t.Errorf("reversed: %q", labels(m.Rows()))
	}
	m.SetSort(ColClass)
	if labels(m.Rows()) != "Client,Server" {
		t.Errorf("by class: %q", labels(m.Rows()))
	}
	// 's' walks the group layout.
	m.Update(key("s"))
	if col, _ := m.SortColumn(); col != ColFuncs {
		t.Errorf("s after class = %v, want funcs", col)
	}
	// min: reads the sort column — ΣCCN Client 16, Server 11.
	for _, c := range []struct{ q, want string }{
		{"fn:serv", "Server"},
		{"min:12", "Client"},
		{"file:a/", "Server"},
		{"lang:py", ""},
		{"cli", "Client"},
		{"min:12 fn:ser", ""},
		{"metric:cog", "Client,Server"},
	} {
		m.SetSort(ColCCN)
		m.filter.SetText(c.q)
		m.Refresh()
		if got := labels(m.Rows()); got != c.want {
			t.Errorf("filter %q = %q, want %q", c.q, got, c.want)
		}
	}
	if col, _ := m.SortColumn(); col != ColCog {
		t.Errorf("metric:cog sorts by %v", col)
	}
}

func TestGroupOpenCopyExportWarn(t *testing.T) {
	m := groupPanel(t)
	m.CycleGroup()
	msg, ok := m.Update(key("enter"))().(OpenLocationMsg)
	if !ok || msg.Path != "/proj/b/client.py" || msg.Line != 5 {
		t.Fatalf("open = %+v", msg)
	}
	cp, _ := m.Update(key("y"))().(CopyMsg)
	if cp.Text != "b/client.py: Client funcs=1 ccn=16 (max 16) cog=20 (max 20)" {
		t.Errorf("copy = %q", cp.Text)
	}
	ex, _ := m.Update(key("e"))().(ExportMsg)
	lines := strings.Split(strings.TrimSpace(ex.CSV), "\n")
	if len(lines) != 3 || lines[0] != strings.Join(groupCSVHeader, ",") ||
		lines[2] != "a/server.go,Server,2,24,11,5.5,10,12,6.0,12,0,0,0" {
		t.Errorf("csv = %q", ex.CSV)
	}
	warn := m.renderRow(theme.DefaultPalette(), 0)  // Client: max CCN 16, max cog 20
	plain := m.renderRow(theme.DefaultPalette(), 1) // Server: max 10 / 12
	if strings.Count(warn, "\x1b[") <= strings.Count(plain, "\x1b[") {
		t.Errorf("the over-threshold group must carry extra styling:\nwarn=%q\nplain=%q", warn, plain)
	}
	m.CycleGroup()
	cp, _ = m.Update(key("y"))().(CopyMsg)
	if cp.Text != "b/client.py: funcs=2 ccn=17 (max 16) cog=20 (max 20)" {
		t.Errorf("file copy = %q", cp.Text)
	}
}

func TestGroupCursorKeptAcrossRefresh(t *testing.T) {
	m := groupPanel(t)
	m.CycleGroup()
	m.Update(key("j"))
	m.SetWarn(5, 5, 0)
	if r, _ := m.Selected(); r.label() != "Server" {
		t.Errorf("cursor moved to %q", r.label())
	}
}

func TestGroupModeRoundTrip(t *testing.T) {
	m := groupPanel(t)
	if m.GroupMode() != "" {
		t.Fatalf("fresh mode = %q", m.GroupMode())
	}
	m.CycleGroup()
	m.CycleGroup()
	m.SetTab(TabFile)
	m.CycleGroup()
	if got := m.GroupMode(); got != "project:file,file:class" {
		t.Fatalf("mode = %q", got)
	}
	r := groupPanel(t)
	r.SetGroupMode(m.GroupMode())
	if r.Grouping() != complexity.ByFile || r.fileGroup != complexity.ByClass || len(r.Rows()) != 2 {
		t.Errorf("restored project %v file %v rows %d", r.Grouping(), r.fileGroup, len(r.Rows()))
	}
	// A level the tab cannot take, or a newer build's spelling, is ignored.
	r.SetGroupMode("file:file,project:method,bogus")
	if r.projectGroup != complexity.ByNone || r.fileGroup != complexity.ByNone {
		t.Errorf("garbage mode: project %v file %v", r.projectGroup, r.fileGroup)
	}
}

func TestGroupEmptyClassLevel(t *testing.T) {
	m := newPanel(t) // free functions only
	m.CycleGroup()
	if len(m.Rows()) != 0 || !strings.Contains(m.View(), "(no class method among the measured functions)") {
		t.Errorf("view:\n%s", m.View())
	}
}
