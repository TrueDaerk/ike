package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
)

// churn_test.go covers the hotspot columns of #2957 on groupPanel's two
// files: b/client.py has excess 6 (Client.send 16/20 at 15/15) and ΣCCN
// 17, a/server.go no excess and ΣCCN 14.

func hotPanel(t *testing.T) *Model {
	t.Helper()
	m := groupPanel(t)
	m.SetChurn(map[string]int{"b/client.py": 2, "a/server.go": 5}, 90)
	return m
}

func rowByRel(t *testing.T, m *Model, rel string) Row {
	t.Helper()
	for _, r := range m.Rows() {
		if r.Rel == rel {
			return r
		}
	}
	t.Fatalf("no row %s in %q", rel, labels(m.Rows()))
	return Row{}
}

// TestChurnHiddenOutsideGit: without churn no layout carries the columns
// and the footer has no window.
func TestChurnHiddenOutsideGit(t *testing.T) {
	m := groupPanel(t)
	for _, level := range []string{"none", "class", "file"} {
		for _, h := range m.headerLabels() {
			if strings.HasPrefix(h, "commits") || strings.HasPrefix(h, "hot") {
				t.Errorf("%s level shows %q outside git", level, h)
			}
		}
		if strings.Contains(m.View(), "churn ") {
			t.Errorf("%s level footer names a churn window outside git", level)
		}
		m.Update(key("b"))
	}
}

// TestChurnFunctionRows: function rows show their file's commits on both
// tabs, the footer names the window, the CSV appends commits.
func TestChurnFunctionRows(t *testing.T) {
	m := hotPanel(t)
	if h := m.headerLabels(); h[len(h)-1] != "commits" {
		t.Fatalf("project layout = %v, want commits last", h)
	}
	for _, r := range m.Rows() {
		want := map[string]int{"b/client.py": 2, "a/server.go": 5}[r.Rel]
		if r.Commits != want {
			t.Errorf("%s %s commits = %d, want %d", r.Rel, r.Fn.Name, r.Commits, want)
		}
	}
	m.SetSize(240, 12) // the footer's tail holds the churn window
	if v := m.View(); !strings.Contains(v, "churn 90d") {
		t.Errorf("footer lacks the churn window:\n%s", v)
	}
	if csv := m.CSV(); !strings.HasSuffix(strings.SplitN(csv, "\n", 2)[0], ",commits") {
		t.Errorf("CSV header lacks commits: %q", strings.SplitN(csv, "\n", 2)[0])
	}
	m.Follow("/proj/a/server.go", 11)
	m.SetTab(TabFile)
	if h := m.headerLabels(); h[len(h)-1] != "commits" || m.Rows()[0].Commits != 5 {
		t.Errorf("file tab: layout %v, commits %d", h, m.Rows()[0].Commits)
	}
}

// TestChurnFileGroups: file groups carry commits and hot (commits × excess,
// × ΣCCN without excess), metric:hot sorts by hot, the top decile warns,
// 's' reaches both columns, class groups carry neither.
func TestChurnFileGroups(t *testing.T) {
	m := hotPanel(t)
	m.Update(key("b")) // class
	for _, h := range m.headerLabels() {
		if strings.HasPrefix(h, "hot") || strings.HasPrefix(h, "commits") {
			t.Errorf("class groups show %q", h)
		}
	}
	m.Update(key("b")) // file
	h := m.headerLabels()
	if h[len(h)-2] != "commits" || h[len(h)-1] != "hot" {
		t.Fatalf("file-group layout = %v, want commits, hot last", h)
	}
	client, server := rowByRel(t, m, "b/client.py"), rowByRel(t, m, "a/server.go")
	if client.Commits != 2 || client.Hot != 12 {
		t.Errorf("client.py commits/hot = %d/%d, want 2/12 (2 × excess 6)", client.Commits, client.Hot)
	}
	if server.Commits != 5 || server.Hot != 70 {
		t.Errorf("server.go commits/hot = %d/%d, want 5/70 (5 × ΣCCN 14)", server.Commits, server.Hot)
	}
	if labels(m.Rows()) != "b/client.py,a/server.go" {
		t.Fatalf("default sort (ΣCCN) = %q", labels(m.Rows()))
	}
	m.filter.SetText("metric:hot")
	m.Refresh()
	if col, desc := m.SortColumn(); col != ColHot || !desc || labels(m.Rows()) != "a/server.go,b/client.py" {
		t.Fatalf("metric:hot: sort %v desc %v rows %q", col, desc, labels(m.Rows()))
	}
	if !m.hotWarns(m.Rows()[0]) || m.hotWarns(m.Rows()[1]) {
		t.Errorf("only the top decile (server.go) warns: threshold %d", m.hotWarn)
	}
	m.filter.SetText("metric:commits")
	m.Refresh()
	if col, _ := m.SortColumn(); col != ColCommits || labels(m.Rows()) != "a/server.go,b/client.py" {
		t.Fatalf("metric:commits: sort %v rows %q", col, labels(m.Rows()))
	}
	m.filter.SetText("")
	m.Refresh()
	m.SetSort(ColExcess)
	m.Update(key("s"))
	if col, _ := m.SortColumn(); col != ColCommits {
		t.Errorf("s after excess = %v, want commits", col)
	}
	m.Update(key("s"))
	if col, _ := m.SortColumn(); col != ColHot {
		t.Errorf("s after commits = %v, want hot", col)
	}
	csv := m.CSV()
	if !strings.Contains(csv, ",commits,hot\n") || !strings.Contains(csv, ",5,70\n") {
		t.Errorf("group CSV lacks the hotspot columns:\n%s", csv)
	}
	// Leaving git hides the columns and drops the hidden sort column.
	m.SetChurn(nil, 90)
	if col, _ := m.SortColumn(); col != ColCCN {
		t.Errorf("sort after the columns hid = %v, want ccn", col)
	}
	if v := m.View(); strings.Contains(v, "churn 90d") || strings.Contains(v, " hot") {
		t.Errorf("the hotspot columns must hide outside git:\n%s", v)
	}
}

// TestChurnTopDecile: of twelve rows the top two (ceil 12/10) warn; all
// zero scores warn nothing.
func TestChurnTopDecile(t *testing.T) {
	m := groupPanel(t)
	files := map[string][]complexity.Function{}
	churn := map[string]int{}
	for i := 1; i <= 12; i++ {
		p := "/proj/f" + itoa(i) + ".go"
		files[p] = []complexity.Function{fn("f", 1, 2, 1, 0)}
		churn["f"+itoa(i)+".go"] = i
	}
	m.Set(complexity.View{Gen: 2, Files: files}, complexity.Stats{Enabled: true})
	m.SetChurn(churn, 30)
	m.Update(key("b"))
	m.Update(key("b"))
	warned := 0
	for _, r := range m.Rows() {
		if m.hotWarns(r) {
			warned++
			if r.Hot < 11 {
				t.Errorf("%s (hot %d) warns below the top decile", r.Rel, r.Hot)
			}
		}
	}
	if warned != 2 {
		t.Errorf("%d rows warn, want 2", warned)
	}
	m.SetChurn(map[string]int{}, 30)
	for _, r := range m.Rows() {
		if m.hotWarns(r) {
			t.Errorf("%s warns with no commits at all", r.Rel)
		}
	}
}
