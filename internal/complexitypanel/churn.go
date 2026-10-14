package complexitypanel

import (
	"sort"

	"ike/internal/complexity"
)

// churn.go is the hotspot half of the panel (Roadmap 0551, #2957): Adam
// Tornhill's churn × complexity. The root model hands the pane the commits
// per project-relative path of the last complexity.churn_days days
// (vcs.ChurnCmd, once per VCS snapshot generation) via SetChurn; inside a
// repository the function layouts gain a commits column and the file-group
// layout commits and hot = commits × Σexcess (× ΣCCN when nothing exceeds,
// complexity.Hot). Outside git the map is nil and the columns hide. The hot
// cell takes the warning colour on the top decile of the visible rows;
// metric:hot / metric:commits and 's' sort by them.

// The hotspot layouts: the plain ones plus the churn columns.
var (
	projectChurnLayout   = withColumns(projectLayout, ColCommits)
	fileChurnLayout      = projectChurnLayout[1:]
	fileGroupChurnLayout = withColumns(fileGroupLayout, ColCommits, ColHot)
)

// withColumns copies base and appends cols, so no layout shares a backing
// array with another.
func withColumns(base []Column, cols ...Column) []Column {
	return append(append(make([]Column, 0, len(base)+len(cols)), base...), cols...)
}

// churnLayout picks the hotspot variant of a layout while churn is known.
func (m *Model) churnLayout(plain, churn []Column) []Column {
	if m.churn != nil {
		return churn
	}
	return plain
}

// SetChurn hands the pane the commits per project-relative slash path in
// the last days days; nil hides the hotspot columns (outside git). A sort
// column the new layout hides falls back to CCN.
func (m *Model) SetChurn(counts map[string]int, days int) {
	m.churn, m.churnDays = counts, days
	m.fitSort()
	m.Refresh()
}

// Churn reports the counts the pane shows and their window; nil outside
// git.
func (m *Model) Churn() (map[string]int, int) { return m.churn, m.churnDays }

// fnHot is a function row's hot score — only a sort key (metric:hot on the
// function list), the column shows on file groups: commits × the
// function's excess, × its CCN when it exceeds nothing.
func (m *Model) fnHot(path string, commits int, fn complexity.Function) int {
	return complexity.Hot(commits, complexity.Summary{Excess: m.th(path).Excess(fn), CCNSum: fn.CCN})
}

// markHot computes the warning threshold of the hot column: the score at
// the top decile of the visible rows (at least one row), 0 — no warning —
// while the column is hidden or every score is 0.
func (m *Model) markHot() {
	m.hotWarn = 0
	if m.layoutIndex(ColHot) < 0 {
		return
	}
	var scores []int
	for _, r := range m.rows {
		if !r.child() {
			scores = append(scores, r.Hot)
		}
	}
	if len(scores) == 0 {
		return
	}
	sort.Sort(sort.Reverse(sort.IntSlice(scores)))
	m.hotWarn = scores[(len(scores)+9)/10-1]
}

// hotWarns reports whether a row's hot cell takes the warning colour.
func (m *Model) hotWarns(r Row) bool { return m.hotWarn > 0 && r.Hot >= m.hotWarn }
