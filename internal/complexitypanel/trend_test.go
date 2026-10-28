package complexitypanel

import (
	"strings"
	"testing"
	"time"

	"ike/internal/complexity"
)

// trend_test.go covers the Trend tab (#2962): the rows newest first with
// their deltas, the header and the sparkline, the sort cycle over the Δ
// columns, the filter, enter → TrendBaseMsg, the copy text and the CSV,
// the computing / not-git / error states, the keys that do nothing here
// and the mouse.

func trendPoint(short string, day int, subject string, fns, ccn, cog, excess, over int) complexity.TrendPoint {
	return complexity.TrendPoint{Sha: short + "0000000000000000000000000000000000", Short: short,
		Time: time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC), Subject: subject, Files: 3,
		Summary: complexity.Summary{Functions: fns, CCNSum: ccn, CogSum: cog, Excess: excess, OverThreshold: over}}
}

// sampleTrend is four points, oldest first: excess 40 → 52 → 52 → 30.
func sampleTrend() Trend {
	return Trend{Window: 30, Ready: true, Points: []complexity.TrendPoint{
		trendPoint("aaa1111", 1, "init", 10, 100, 80, 40, 3),
		trendPoint("bbb2222", 2, "grow render", 11, 112, 95, 52, 4),
		trendPoint("ccc3333", 3, "docs only", 11, 112, 95, 52, 4),
		trendPoint("ddd4444", 4, "extract helper", 12, 110, 84, 30, 2),
	}}
}

func trendPanel(t *testing.T) *Model {
	t.Helper()
	m := newPanel(t)
	m.SetTrend(sampleTrend())
	m.SetTab(TabTrend)
	return m
}

func shorts(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Trend.Point.Short
	}
	return out
}

func TestTrendTabRowsNewestFirstWithDeltas(t *testing.T) {
	m := trendPanel(t)
	if m.Tab() != TabTrend || !tabEnabled(TabTrend) {
		t.Fatalf("tab = %v", m.Tab())
	}
	if got := strings.Join(shorts(m.Rows()), ","); got != "ddd4444,ccc3333,bbb2222,aaa1111" {
		t.Fatalf("rows = %s, want newest first", got)
	}
	if col, desc := m.SortColumn(); col != ColCommit || !desc {
		t.Fatalf("sort = %v desc=%v, want commit descending", col, desc)
	}
	newest := m.Rows()[0]
	cells := newest.cells(trendLayout)
	want := []string{"ddd4444", "2026-10-04", "extract helper", "12", "110 (-2)", "84 (-11)", "30 (-22)", "2 (-2)"}
	if strings.Join(cells, "|") != strings.Join(want, "|") {
		t.Errorf("newest cells = %v, want %v", cells, want)
	}
	oldest := m.Rows()[3]
	if cells := oldest.cells(trendLayout); cells[4] != "100" || cells[6] != "40" {
		t.Errorf("the oldest point has no Δ: %v", cells)
	}
	if m.Total() != 4 || m.Summary().Functions != 0 {
		t.Errorf("total %d summary %+v: commit rows are no functions", m.Total(), m.Summary())
	}
	view := m.View()
	for _, want := range []string{"Trend", "last 4 commits (window 30)", "Σexcess 40 → 30 (-10)", "ΣCCN 100 → 110 (+10)",
		"commit", "Σccn (Δ)", "excess (Δ)", "over (Δ)", "extract helper", "grow render",
		"no grouping or breakdown on this tab", "enter changes since", "r recompute"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	for _, no := range []string{"D dist", "b group", "i accept", "R base", "t tests"} {
		if strings.Contains(view, no) {
			t.Errorf("view offers %q on the Trend tab:\n%s", no, view)
		}
	}
	if !strings.Contains(view, "Σexcess") || strings.Contains(view, "0 fns") {
		t.Errorf("header must be the trend header:\n%s", view)
	}
}

func TestTrendSparkline(t *testing.T) {
	if got := sparkline(sampleTrend().Points); got != "▄██▁" {
		t.Errorf("sparkline = %q, want ▄██▁ (40, 52, 52, 30 between 30 and 52)", got)
	}
	flat := []complexity.TrendPoint{trendPoint("a", 1, "", 1, 1, 1, 5, 0), trendPoint("b", 2, "", 1, 1, 1, 5, 0)}
	if got := sparkline(flat); got != "▄▄" {
		t.Errorf("flat sparkline = %q", got)
	}
	if sparkline(nil) != "" {
		t.Error("empty sparkline")
	}
	m := trendPanel(t)
	if !m.sparkShown() || m.chromeRows() != 5 {
		t.Fatalf("spark shown %v chrome %d", m.sparkShown(), m.chromeRows())
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) < 3 || !strings.Contains(lines[2], "excess ") || !strings.Contains(lines[2], "▄██") || !strings.Contains(lines[2], "▁") || !strings.Contains(lines[2], "30–52") {
		t.Fatalf("sparkline row = %q", lines[2])
	}
	// The histogram toggle takes no row on this tab; the sparkline stays.
	m.ToggleHistogram()
	if m.histShown() || m.chromeRows() != 5 {
		t.Errorf("hist shown %v chrome %d on the Trend tab", m.histShown(), m.chromeRows())
	}
	// A click on the second bar selects that commit's row.
	m.Click(len([]rune(sparkLabel))+1, 2)
	if r, ok := m.Selected(); !ok || r.Trend.Point.Short != "bbb2222" {
		t.Errorf("spark click selected %+v", r.Trend)
	}
	// Without points the row is gone.
	m.SetTrend(Trend{Window: 30, Ready: true})
	if m.sparkShown() || m.chromeRows() != 4 {
		t.Errorf("spark shown %v chrome %d without points", m.sparkShown(), m.chromeRows())
	}
	if !strings.Contains(m.View(), "(no commit to measure)") {
		t.Errorf("empty view:\n%s", m.View())
	}
}

func TestTrendSortCycleAndFilter(t *testing.T) {
	m := trendPanel(t)
	// 's' cycles the Trend layout: commit → date → subject → fns → Σccn Δ
	// → Σcog Δ → excess Δ → over Δ → commit.
	cycle := []Column{ColDate, ColSubject, ColFuncs, ColCCNDelta, ColCogDelta, ColExcessDelta, ColOverDelta, ColCommit}
	for _, want := range cycle {
		m.Update(key("s"))
		if col, _ := m.SortColumn(); col != want {
			t.Fatalf("sort = %v, want %v", col, want)
		}
	}
	m.SetSort(ColExcessDelta)
	if got := strings.Join(shorts(m.Rows()), ","); got != "bbb2222,ccc3333,aaa1111,ddd4444" {
		t.Errorf("excess Δ descending = %s (want +12, 0, 0, -22)", got)
	}
	m.Update(key("S"))
	if got := shorts(m.Rows())[0]; got != "ddd4444" {
		t.Errorf("reversed first = %s, want the -22 commit", got)
	}
	m.SetSort(ColSubject)
	if got := strings.Join(shorts(m.Rows()), ","); got != "ccc3333,ddd4444,bbb2222,aaa1111" {
		t.Errorf("subject ascending = %s", got)
	}
	// The filter's bare words match the subject and the short sha; min:
	// bounds the sort column's Δ.
	m.SetSort(ColCommit)
	m.filter.Focus()
	m.filter.SetText("helper")
	m.Refresh()
	if got := strings.Join(shorts(m.Rows()), ","); got != "ddd4444" {
		t.Errorf("filter helper = %s", got)
	}
	m.filter.SetText("bbb2")
	m.Refresh()
	if got := strings.Join(shorts(m.Rows()), ","); got != "bbb2222" {
		t.Errorf("filter sha = %s", got)
	}
	m.filter.SetText("")
	m.SetSort(ColExcessDelta)
	m.filter.SetText("min:1")
	m.Refresh()
	if got := strings.Join(shorts(m.Rows()), ","); got != "bbb2222" {
		t.Errorf("min:1 on excess Δ = %s", got)
	}
	m.filter.SetText("fn:render")
	m.Refresh()
	if len(m.Rows()) != 0 {
		t.Errorf("fn: matches no commit row: %v", shorts(m.Rows()))
	}
}

func TestTrendEnterCopyExportAndIdleKeys(t *testing.T) {
	m := trendPanel(t)
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter yields no command")
	}
	msg, ok := cmd().(TrendBaseMsg)
	if !ok || msg.Rev != "ddd4444" {
		t.Fatalf("enter = %+v", msg)
	}
	cmd = m.Update(key("y"))
	copyMsg, ok := cmd().(CopyMsg)
	want := "ddd4444 2026-10-04 extract helper: fns 12 Σccn 110 (-2) Σcog 84 (-11) excess 30 (-22) over 2 (-2)"
	if !ok || copyMsg.Text != want {
		t.Fatalf("copy = %q, want %q", copyMsg.Text, want)
	}
	cmd = m.Update(key("e"))
	exp, ok := cmd().(ExportMsg)
	if !ok || exp.Label != "Trend" {
		t.Fatalf("export = %+v", exp)
	}
	lines := strings.Split(strings.TrimSpace(exp.CSV), "\n")
	if len(lines) != 5 || lines[0] != strings.Join(trendCSVHeader, ",") {
		t.Fatalf("csv = %v", lines)
	}
	if !strings.HasPrefix(lines[1], "ddd4444,ddd44440000000000000000000000000000000000,2026-10-04T12:00:00Z,extract helper,12,110,-2,84,-11,30,-22,2,-2") {
		t.Errorf("csv row = %s", lines[1])
	}
	if !strings.HasSuffix(lines[4], ",10,100,0,80,0,40,0,3,0") {
		t.Errorf("oldest csv row = %s", lines[4])
	}
	// The function-level keys do nothing here and leave the other tabs'
	// state alone.
	for _, k := range []string{"b", "v", "D", "space", "m", "i", "p", "d", "R"} {
		if cmd := m.Update(key(k)); cmd != nil {
			t.Errorf("%q yields a command on the Trend tab", k)
		}
	}
	if m.Grouping() != complexity.ByNone || m.treeView || m.Prompting() || len(m.expanded) != 0 {
		t.Errorf("a Trend key changed state: group %v tree %v prompt %v expanded %v", m.Grouping(), m.treeView, m.Prompting(), m.expanded)
	}
	// 'r' asks for a recompute like the other tabs.
	if cmd := m.Update(key("r")); cmd == nil {
		t.Error("r yields no refresh")
	} else if _, ok := cmd().(RefreshMsg); !ok {
		t.Error("r must yield RefreshMsg")
	}
	// The row key keeps the cursor on the commit across a refresh.
	m.Update(key("down"))
	m.SetTrend(sampleTrend())
	if r, _ := m.Selected(); r.Trend.Point.Short != "ccc3333" {
		t.Errorf("cursor after refresh on %s", r.Trend.Point.Short)
	}
}

func TestTrendStatesAndHeaderBar(t *testing.T) {
	m := newPanel(t)
	m.SetTab(TabTrend)
	if !strings.Contains(m.View(), "(no trend yet") {
		t.Errorf("fresh tab:\n%s", m.View())
	}
	m.SetTrend(Trend{Window: 30, Computing: true, Done: 12, Total: 30})
	view := m.View()
	if !strings.Contains(view, "computing… 12/30") || !strings.Contains(view, "(measuring the last commits…)") {
		t.Errorf("computing view:\n%s", view)
	}
	m.SetTrend(Trend{NotGit: true})
	if view := m.View(); !strings.Contains(view, "not a git repository") || !strings.Contains(view, "(not a git repository)") {
		t.Errorf("not-git view:\n%s", view)
	}
	tr := sampleTrend()
	tr.Err = "git: unknown revision"
	m.SetTrend(tr)
	if view := m.View(); !strings.Contains(view, "trend kept · git: unknown revision") || !strings.Contains(view, "last 4 commits") {
		t.Errorf("error view keeps the points:\n%s", view)
	}
	// A one-commit repository.
	m.SetTrend(Trend{Window: 30, Ready: true, Points: sampleTrend().Points[:1]})
	if view := m.View(); !strings.Contains(view, "last commit (window 30)") {
		t.Errorf("single point header:\n%s", view)
	}
	// The header tab bar reaches the tab by mouse.
	m.SetTab(TabProject)
	x := len([]rune(" Complexity")) + 3 + len("Project File Changes ")
	m.Click(x+1, 0)
	if m.Tab() != TabTrend {
		t.Errorf("header click selected %v", m.Tab())
	}
	// Switching away and back keeps the data.
	m.SetTab(TabChanges)
	m.SetTab(TabTrend)
	if len(m.Rows()) != 1 || m.TrendData().Window != 30 {
		t.Errorf("rows after a round trip = %d", len(m.Rows()))
	}
}
