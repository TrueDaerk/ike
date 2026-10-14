package complexitypanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// complexitypanel_test.go covers the Complexity tool window's model
// (#2917): the Project / File tabs over a snapshot, the sort cycle, the
// filter language, the caret follow, the export, the copy text and the
// mouse.

func fn(name string, line, end, ccn, cog int) complexity.Function {
	return complexity.Function{Name: name, Kind: complexity.KindFunction, Line: line, EndLine: end,
		NLOC: end - line + 1, Params: 1, CCN: ccn, Cognitive: cog, Depth: 2}
}

// sample is a three-file snapshot under /proj.
func sample() complexity.View {
	return complexity.View{Gen: 3, Files: map[string][]complexity.Function{
		"/proj/internal/app/app.go": {fn("render", 0, 20, 25, 30), fn("tiny", 30, 32, 1, 0)},
		"/proj/internal/ui/list.go": {fn("ListNav", 5, 40, 12, 18), fn("inner", 10, 15, 3, 2)},
		"/proj/web/index.php":       {fn("Page::show", 0, 9, 16, 4)},
	}}
}

func langByExt(path string) string {
	switch {
	case strings.HasSuffix(path, ".go"):
		return "go"
	case strings.HasSuffix(path, ".php"):
		return "php"
	}
	return ""
}

func newPanel(t *testing.T) *Model {
	t.Helper()
	m := New(theme.DefaultPalette())
	m.pathLang = langByExt
	m.SetSize(170, 12)
	m.SetRoot("/proj")
	m.Set(sample(), complexity.Stats{Enabled: true})
	return &m
}

func names(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Fn.Name
	}
	return out
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "S":
		return tea.KeyPressMsg{Code: 'S', Text: "S", Mod: tea.ModShift}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestProjectTabSortsByCCNDescending(t *testing.T) {
	m := newPanel(t)
	if m.Tab() != TabProject {
		t.Fatalf("tab = %v, want Project", m.Tab())
	}
	col, desc := m.SortColumn()
	if col != ColCCN || !desc {
		t.Fatalf("sort = %v desc=%v, want CCN descending", col, desc)
	}
	got := names(m.Rows())
	want := []string{"render", "Page::show", "ListNav", "inner", "tiny"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if m.Total() != 5 || m.Rows()[0].Rel != "internal/app/app.go" {
		t.Fatalf("total=%d rel=%q", m.Total(), m.Rows()[0].Rel)
	}
	s := m.Summary()
	if s.Functions != 5 || s.CCNMax != 25 || s.CogMax != 30 || s.OverThreshold != 3 {
		t.Fatalf("summary = %+v", s)
	}
	v := m.View()
	for _, want := range []string{"Complexity", "Project", "File", "Changes", "5 fns", "max 25", "3 over", "excess 29", "gen 3", "ccn ▼", "5/5 rows"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestSortCycleAndReverse(t *testing.T) {
	m := newPanel(t)
	m.Update(key("s"))
	if col, desc := m.SortColumn(); col != ColCog || !desc {
		t.Fatalf("after s: %v desc=%v, want cog descending", col, desc)
	}
	if got := names(m.Rows()); got[0] != "render" || got[1] != "ListNav" {
		t.Fatalf("cog order = %v", got)
	}
	m.Update(key("S"))
	if _, desc := m.SortColumn(); desc {
		t.Fatal("S must reverse the order")
	}
	if got := names(m.Rows()); got[0] != "tiny" {
		t.Fatalf("reversed cog order = %v", got)
	}
	// The cycle wraps to the file column, a text column sorted ascending.
	for i := 0; i < 5; i++ {
		m.Update(key("s"))
	}
	if col, desc := m.SortColumn(); col != ColFile || desc {
		t.Fatalf("after wrap: %v desc=%v, want file ascending", col, desc)
	}
	if got := m.Rows()[0].Rel; got != "internal/app/app.go" {
		t.Fatalf("file order starts at %q", got)
	}
}

func TestFilterFieldsAndMetric(t *testing.T) {
	m := newPanel(t)
	m.filter.SetText("file:internal/app min:20")
	m.Refresh()
	if got := names(m.Rows()); len(got) != 1 || got[0] != "render" {
		t.Fatalf("file+min rows = %v", got)
	}
	if !strings.Contains(m.View(), "1/5 rows") {
		t.Fatalf("status row must show filtered/total:\n%s", m.View())
	}
	// metric:cog switches the sort and the min: column: cog ≥ 18 keeps
	// render (30) and ListNav (18), not Page::show (16 ccn, 4 cog).
	m.filter.SetText("min:18 metric:cog")
	m.Refresh()
	if col, _ := m.SortColumn(); col != ColCog {
		t.Fatalf("metric:cog must sort by cog, got %v", col)
	}
	if got := names(m.Rows()); strings.Join(got, ",") != "render,ListNav" {
		t.Fatalf("metric rows = %v", got)
	}
	m.filter.SetText("lang:php")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "Page::show" {
		t.Fatalf("lang rows = %v", got)
	}
	m.filter.SetText("fn:nav")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "ListNav" {
		t.Fatalf("fn rows = %v", got)
	}
	m.filter.SetText("rndr")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "render" {
		t.Fatalf("fuzzy rows = %v", got)
	}
	m.filter.SetText("file:*.php")
	m.Refresh()
	if got := names(m.Rows()); strings.Join(got, ",") != "Page::show" {
		t.Fatalf("glob rows = %v", got)
	}
}

func TestFileTabFollowsCaret(t *testing.T) {
	m := newPanel(t)
	m.Follow("/proj/internal/ui/list.go", 12)
	m.Update(key("tab"))
	if m.Tab() != TabFile {
		t.Fatalf("tab = %v, want File", m.Tab())
	}
	if got := names(m.Rows()); strings.Join(got, ",") != "ListNav,inner" {
		t.Fatalf("file rows = %v", got)
	}
	// The innermost unit containing the caret is marked.
	if i := m.CaretRow(); i != 1 || !m.Rows()[1].Caret {
		t.Fatalf("caret row = %d, want inner", i)
	}
	if !strings.Contains(m.View(), caretMark+"inner") {
		t.Fatalf("view must mark the caret row:\n%s", m.View())
	}
	// The file column is hidden on the File tab.
	if strings.Contains(m.View(), "internal/ui/list.go") {
		t.Fatalf("File tab must hide the file column:\n%s", m.View())
	}
	m.Follow("/proj/internal/ui/list.go", 30)
	if i := m.CaretRow(); i != 0 {
		t.Fatalf("after move caret row = %d, want ListNav", i)
	}
	m.Follow("/proj/internal/ui/list.go", 50)
	if i := m.CaretRow(); i != -1 {
		t.Fatalf("outside every unit caret row = %d, want -1", i)
	}
	// A buffer switch rebuilds the tab.
	m.Follow("/proj/web/index.php", 3)
	if got := names(m.Rows()); strings.Join(got, ",") != "Page::show" || m.CaretRow() != 0 {
		t.Fatalf("switched rows = %v caret=%d", got, m.CaretRow())
	}
	// tab moves on to Changes (#2918) and Trend (#2962), then wraps to Project.
	m.Update(key("tab"))
	if m.Tab() != TabChanges {
		t.Fatalf("tab after File = %v, want Changes", m.Tab())
	}
	m.Update(key("tab"))
	if m.Tab() != TabTrend {
		t.Fatalf("tab after Changes = %v, want Trend", m.Tab())
	}
	m.Update(key("tab"))
	if m.Tab() != TabProject {
		t.Fatalf("tab after wrap = %v, want Project", m.Tab())
	}
	m.Update(key("shift+tab"))
	if m.Tab() != TabTrend {
		t.Fatalf("shift+tab = %v, want Trend", m.Tab())
	}
}

func TestEnterOpensAndCopyFormats(t *testing.T) {
	m := newPanel(t)
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter yields no command")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "/proj/internal/app/app.go" || msg.Line != 0 {
		t.Fatalf("open = %+v", msg)
	}
	m.Update(key("j"))
	cmd = m.Update(key("y"))
	cp, ok := cmd().(CopyMsg)
	if !ok || cp.Text != "web/index.php:1: Page::show ccn=16 cog=4" {
		t.Fatalf("copy = %+v", cp)
	}
	if cmd := m.Update(key("r")); cmd == nil {
		t.Fatal("r yields no refresh")
	} else if _, ok := cmd().(RefreshMsg); !ok {
		t.Fatal("r must yield RefreshMsg")
	}
}

func TestExportCSV(t *testing.T) {
	m := newPanel(t)
	m.filter.SetText("lang:go")
	m.Refresh()
	cmd := m.Update(key("e"))
	msg, ok := cmd().(ExportMsg)
	if !ok || msg.Label != "Project" {
		t.Fatalf("export = %+v", msg)
	}
	lines := strings.Split(strings.TrimSpace(msg.CSV), "\n")
	if len(lines) != 5 || lines[0] != "file,function,kind,line,ccn,cognitive,nloc,params,depth,comment_lines,cmt_pct" {
		t.Fatalf("csv = %q", msg.CSV)
	}
	if lines[1] != "internal/app/app.go,render,function,1,25,30,21,1,2,0,0" {
		t.Fatalf("first row = %q", lines[1])
	}
}

func TestWarnThresholdPaintsCells(t *testing.T) {
	m := newPanel(t)
	warn := m.renderRow(theme.DefaultPalette(), 0)  // render: ccn 25, cog 30
	plain := m.renderRow(theme.DefaultPalette(), 4) // tiny: ccn 1
	if strings.Count(warn, "\x1b[") <= strings.Count(plain, "\x1b[") {
		t.Fatalf("the over-threshold row must carry extra styling:\nwarn=%q\nplain=%q", warn, plain)
	}
	m.SetWarn(100, 100, 0)
	if s := m.Summary(); s.OverThreshold != 0 {
		t.Fatalf("over threshold with 100/100 = %d", s.OverThreshold)
	}
}

func TestMouseSelectsOpensAndSwitches(t *testing.T) {
	m := newPanel(t)
	now := time.Unix(100, 0)
	m.SetNow(func() time.Time { return now })
	if cmd := m.Click(5, 4); cmd != nil || m.Cursor() != 1 {
		t.Fatalf("single click: cmd=%v cursor=%d", cmd, m.Cursor())
	}
	now = now.Add(50 * time.Millisecond)
	cmd := m.Click(5, 4)
	if cmd == nil {
		t.Fatal("double click must open")
	}
	if msg, ok := cmd().(OpenLocationMsg); !ok || msg.Path != "/proj/web/index.php" {
		t.Fatalf("double click = %+v", msg)
	}
	// A click on the "File" tab name switches the tab.
	m.Click(len(" Complexity")+3+len("Project")+1, 0)
	if m.Tab() != TabFile {
		t.Fatalf("header click tab = %v, want File", m.Tab())
	}
	m.Click(len(" Complexity")+3+len("Project")+1+len("File")+1, 0)
	if m.Tab() != TabChanges {
		t.Fatalf("header click tab = %v, want Changes", m.Tab())
	}
	m.SetTab(TabProject)
	// A click on a column header sorts by it; a second one reverses.
	x := 1 + m.widths[ColFile] + 2 + m.widths[ColFunc] + 2 // the line column
	m.Click(x, 2)
	if col, desc := m.SortColumn(); col != ColLine || !desc {
		t.Fatalf("column click = %v desc=%v", col, desc)
	}
	m.Click(x, 2)
	if _, desc := m.SortColumn(); desc {
		t.Fatal("second column click must reverse")
	}
	m.SetSize(170, 6) // two list rows: the wheel has something to scroll
	m.Wheel(2)
	if m.Cursor() < 2 {
		t.Fatalf("wheel cursor = %d", m.Cursor())
	}
}

func TestEmptyStates(t *testing.T) {
	m := New(theme.DefaultPalette())
	m.SetSize(80, 8)
	if !strings.Contains(m.View(), "(no measured function in the project)") {
		t.Fatalf("fresh view:\n%s", m.View())
	}
	m.Set(complexity.View{Files: map[string][]complexity.Function{}}, complexity.Stats{Enabled: true, Scanning: true})
	if v := m.View(); !strings.Contains(v, "scanning…") || !strings.Contains(v, "(scanning the project…)") {
		t.Fatalf("scanning view:\n%s", v)
	}
	// A walk seeded from the persisted cache says so in the header (#2967).
	m.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{}}, complexity.Stats{Enabled: true, Cached: 2981})
	if v := m.View(); !strings.Contains(v, "gen 1 (2981 cached)") {
		t.Fatalf("cached view:\n%s", v)
	}
	m.Set(complexity.View{Gen: 2, Files: map[string][]complexity.Function{}}, complexity.Stats{Enabled: true})
	if v := m.View(); !strings.Contains(v, "gen 2") || strings.Contains(v, "cached") {
		t.Fatalf("cold view:\n%s", v)
	}
	m.Set(complexity.View{Files: map[string][]complexity.Function{}}, complexity.Stats{Enabled: false})
	if !strings.Contains(m.View(), "complexity.enabled is off") {
		t.Fatalf("disabled view:\n%s", m.View())
	}
	m.SetTab(TabFile)
	m.Set(complexity.View{Files: map[string][]complexity.Function{}}, complexity.Stats{Enabled: true})
	if !strings.Contains(m.View(), "(no file focused)") {
		t.Fatalf("file tab view:\n%s", m.View())
	}
}

func TestFilterRowAndPaste(t *testing.T) {
	m := newPanel(t)
	m.Update(key("/"))
	if !m.Filtering() || !m.LineInputOpen() {
		t.Fatal("/ must focus the filter row")
	}
	if !m.PasteText("lang:php") {
		t.Fatal("paste into the open filter must apply")
	}
	if got := names(m.Rows()); strings.Join(got, ",") != "Page::show" {
		t.Fatalf("pasted filter rows = %v", got)
	}
	if st := m.NextMatch(); st.Total != 1 || !st.Handled {
		t.Fatalf("step = %+v", st)
	}
}
