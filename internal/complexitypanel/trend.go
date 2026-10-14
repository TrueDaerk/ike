package complexitypanel

// trend.go is the Trend tab of the Complexity window (Roadmap 0551,
// #2962): the project's ΣCCN, Σcog, excess and over-threshold count at
// each of the last N first-parent commits, one row per commit newest
// first, with a sparkline of the excess under the header. The pane stays a
// consumer: the root model walks the commits off the Update goroutine
// (complexity.WalkTrend) and hands the result in via SetTrend; enter on a
// row asks it — through TrendBaseMsg — to show the Changes tab against
// that commit. Grouping, the breakdown, the baseline and the pins do not
// apply here: a row is a commit, not a function.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/complexity"
	"ike/internal/filterexpr"
	"ike/internal/theme"
)

// Trend is the data behind the Trend tab, handed in whole by the root
// model.
type Trend struct {
	// Points are the measured commits, oldest first (complexity.Trend).
	Points []complexity.TrendPoint
	// Window is the configured number of commits (complexity.trend_commits);
	// a young repository has fewer points.
	Window int
	// Computing is true while the root model walks; Done / Total is its
	// progress ("computing… 12/30"). Ready once a walk landed at least
	// once; NotGit when the project is not a repository; Err the git
	// message of a failed walk (the previous points stay).
	Computing bool
	Done      int
	Total     int
	Ready     bool
	NotGit    bool
	Err       string
}

// TrendBaseMsg asks the root model to compare the Changes tab against the
// commit (Rev is its short sha, the base of a "sha..HEAD" range) and to
// switch to that tab.
type TrendBaseMsg struct{ Rev string }

// TrendRow is the per-commit data of a Trend tab row: the point and the
// previous (older) point its deltas are against, nil on the oldest.
type TrendRow struct {
	Point complexity.TrendPoint
	Prev  *complexity.TrendPoint
	// Index is the point's position oldest first, the chronological sort
	// key.
	Index int
}

// SetTrend replaces the Trend tab's data and rebuilds its rows.
func (m *Model) SetTrend(t Trend) {
	m.trend = t
	if m.tab == TabTrend {
		m.Refresh()
	}
}

// TrendData reports the tab's current data (tests, the root model).
func (m *Model) TrendData() Trend { return m.trend }

// trendLayout is the Trend tab's column order.
var trendLayout = []Column{ColCommit, ColDate, ColSubject, ColFuncs, ColCCNDelta, ColCogDelta, ColExcessDelta, ColOverDelta}

// trendRows lists the Trend tab's population: one row per point, each
// with its predecessor for the deltas.
func (m *Model) trendRows() []Row {
	pts := m.trend.Points
	out := make([]Row, 0, len(pts))
	for i := range pts {
		tr := &TrendRow{Point: pts[i], Index: i}
		if i > 0 {
			tr.Prev = &pts[i-1]
		}
		out = append(out, Row{Trend: tr})
	}
	return out
}

// delta is the row's change of one summary field against the previous
// point, 0 on the oldest.
func (tr *TrendRow) delta(pick func(complexity.Summary) int) int {
	if tr.Prev == nil {
		return 0
	}
	return pick(tr.Point.Summary) - pick(tr.Prev.Summary)
}

func sumCCN(s complexity.Summary) int    { return s.CCNSum }
func sumCog(s complexity.Summary) int    { return s.CogSum }
func sumExcess(s complexity.Summary) int { return s.Excess }
func sumOver(s complexity.Summary) int   { return s.OverThreshold }
func sumFuncs(s complexity.Summary) int  { return s.Functions }

// DeltaExcess is the row's excess change against the previous point.
func (tr *TrendRow) DeltaExcess() int { return tr.delta(sumExcess) }

// trendDate renders the commit date.
func (tr *TrendRow) date() string { return tr.Point.Time.Format("2006-01-02") }

// sumCell renders "sum (Δ)"; the oldest point has no Δ.
func (tr *TrendRow) sumCell(pick func(complexity.Summary) int) string {
	s := strconv.Itoa(pick(tr.Point.Summary))
	if tr.Prev == nil {
		return s
	}
	return s + " (" + signed(tr.delta(pick)) + ")"
}

// trendCell renders the row's text for column c.
func (r Row) trendCell(c Column) string {
	tr := r.Trend
	switch c {
	case ColCommit:
		return tr.Point.Short
	case ColDate:
		return tr.date()
	case ColSubject:
		return tr.Point.Subject
	case ColFuncs:
		return strconv.Itoa(tr.Point.Summary.Functions)
	case ColCCNDelta:
		return tr.sumCell(sumCCN)
	case ColCogDelta:
		return tr.sumCell(sumCog)
	case ColExcessDelta:
		return tr.sumCell(sumExcess)
	case ColOverDelta:
		return tr.sumCell(sumOver)
	}
	return ""
}

// trendValue is the row's sort key on column c: the chronological index
// on commit, the time on date, the subject as text, the function count,
// and the signed Δ on the sum columns — so "excess (Δ) ▼" lists the
// commits that cost the most first.
func (r Row) trendValue(c Column) (float64, string) {
	tr := r.Trend
	switch c {
	case ColCommit:
		return float64(tr.Index), ""
	case ColDate:
		return float64(tr.Point.Time.Unix()), ""
	case ColSubject:
		return 0, tr.Point.Subject
	case ColFuncs:
		return float64(tr.Point.Summary.Functions), ""
	case ColCCNDelta:
		return float64(tr.delta(sumCCN)), ""
	case ColCogDelta:
		return float64(tr.delta(sumCog)), ""
	case ColExcessDelta:
		return float64(tr.delta(sumExcess)), ""
	case ColOverDelta:
		return float64(tr.delta(sumOver)), ""
	}
	return 0, ""
}

// trendMatches applies the filter row to a Trend row: the bare words
// match the short sha and the subject, min: / max: bound the sort column's
// value (the Δ on a sum column); the function-level keys match nothing.
func (m *Model) trendMatches(q filterexpr.Query, r Row) bool {
	if q.Empty() {
		return true
	}
	bound := func(min bool) func(string) bool {
		return func(v string) bool {
			n, err := strconv.Atoi(v)
			if err != nil {
				return false
			}
			col := m.sortCol
			if !col.numeric() {
				col = ColExcessDelta
			}
			x, _ := r.trendValue(col)
			if min {
				return x >= float64(n)
			}
			return x <= float64(n)
		}
	}
	for _, field := range []string{"file", "lang", "fn", "kind", "class", "depth", "params", "nloc", "test", "baseline", "pinned"} {
		if !q.KeepsAll(field, func(string) bool { return false }) {
			return false
		}
	}
	if !q.KeepsAll("min", bound(true)) || !q.KeepsAll("max", bound(false)) {
		return false
	}
	_, ok := q.MatchFree(r.trendLabel())
	return ok
}

// trendLabel is what the bare-word match reads: the short sha and the
// subject.
func (r Row) trendLabel() string { return r.Trend.Point.Short + " " + r.Trend.Point.Subject }

// trendKey identifies the row across refreshes.
func (r Row) trendKey() string { return "t\x00" + r.Trend.Point.Sha }

// trendCopyText renders a row for 'y': "43b2a00 2026-10-14 subject: fns
// 120 Σccn 410 (+6) Σcog 380 (-9) excess 42 (-12) over 3 (0)".
func trendCopyText(r Row) string {
	tr := r.Trend
	return tr.Point.Short + " " + tr.date() + " " + tr.Point.Subject + ": fns " + strconv.Itoa(tr.Point.Summary.Functions) +
		" Σccn " + tr.sumCell(sumCCN) + " Σcog " + tr.sumCell(sumCog) +
		" excess " + tr.sumCell(sumExcess) + " over " + tr.sumCell(sumOver)
}

// trendCSVHeader is the Trend tab's export header.
var trendCSVHeader = []string{"commit", "sha", "date", "subject", "functions",
	"ccn_sum", "ccn_delta", "cog_sum", "cog_delta", "excess", "excess_delta", "over", "over_delta"}

// trendCSV renders the Trend tab: one row per visible commit in the
// displayed order, the sums and their deltas.
func (m *Model) trendCSV() string {
	var b strings.Builder
	writeCSVRow(&b, trendCSVHeader)
	for _, r := range m.rows {
		tr := r.Trend
		if tr == nil {
			continue
		}
		s := tr.Point.Summary
		writeCSVRow(&b, []string{tr.Point.Short, tr.Point.Sha, tr.Point.Time.Format("2006-01-02T15:04:05Z07:00"), tr.Point.Subject,
			strconv.Itoa(s.Functions),
			strconv.Itoa(s.CCNSum), strconv.Itoa(tr.delta(sumCCN)),
			strconv.Itoa(s.CogSum), strconv.Itoa(tr.delta(sumCog)),
			strconv.Itoa(s.Excess), strconv.Itoa(tr.delta(sumExcess)),
			strconv.Itoa(s.OverThreshold), strconv.Itoa(tr.delta(sumOver))})
	}
	return b.String()
}

// trendTotals renders the Trend tab's header: the window and the excess
// of the oldest → newest point with its delta; the walk's progress while
// it runs.
func (m *Model) trendTotals() string {
	t := m.trend
	var parts []string
	switch {
	case t.NotGit:
		return "not a git repository"
	case t.Err != "":
		parts = append(parts, "trend kept · "+t.Err)
	case t.Computing && t.Total > 0:
		parts = append(parts, "computing… "+itoa(t.Done)+"/"+itoa(t.Total))
	case t.Computing:
		parts = append(parts, "computing…")
	}
	if n := len(t.Points); n > 0 {
		label := "last " + itoa(n) + " commits"
		if n == 1 {
			label = "last commit"
		}
		if t.Window > n {
			label += " (window " + itoa(t.Window) + ")"
		}
		tr := complexity.Trend{Points: t.Points}
		o, nw := tr.Oldest().Summary, tr.Newest().Summary
		parts = append(parts, label,
			"Σexcess "+itoa(o.Excess)+" → "+itoa(nw.Excess)+" ("+signed(nw.Excess-o.Excess)+")",
			"ΣCCN "+itoa(o.CCNSum)+" → "+itoa(nw.CCNSum)+" ("+signed(nw.CCNSum-o.CCNSum)+")",
			"Σcog "+itoa(o.CogSum)+" → "+itoa(nw.CogSum)+" ("+signed(nw.CogSum-o.CogSum)+")")
	} else if t.Window > 0 && !t.Computing {
		parts = append(parts, "last "+itoa(t.Window)+" commits")
	}
	return strings.Join(parts, " · ")
}

// trendEmpty explains an empty Trend list.
func (m *Model) trendEmpty() string {
	t := m.trend
	switch {
	case t.NotGit:
		return "(not a git repository)"
	case t.Computing && !t.Ready:
		return "(measuring the last commits…)"
	case !t.Ready:
		return "(no trend yet — the tab fills once the last commits are measured)"
	case !m.filter.Empty():
		return "(no row matches the filter)"
	}
	return "(no commit to measure)"
}

// sparkLevels are the eight bar heights of the sparkline.
var sparkLevels = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// sparkShown reports whether the sparkline row takes a body line: the
// Trend tab with at least one point.
func (m *Model) sparkShown() bool { return m.tab == TabTrend && len(m.trend.Points) > 0 }

// sparkline renders the excess of every point, oldest left to newest
// right, scaled between the lowest and the highest point; a flat trend is
// a row of middle bars.
func sparkline(points []complexity.TrendPoint) string {
	if len(points) == 0 {
		return ""
	}
	lo, hi := points[0].Summary.Excess, points[0].Summary.Excess
	for _, p := range points {
		lo, hi = min(lo, p.Summary.Excess), max(hi, p.Summary.Excess)
	}
	var b strings.Builder
	for _, p := range points {
		level := 3
		if hi > lo {
			level = (p.Summary.Excess - lo) * (len(sparkLevels) - 1) / (hi - lo)
		}
		b.WriteString(sparkLevels[level])
	}
	return b.String()
}

// sparkLabel prefixes the sparkline row.
const sparkLabel = " excess "

// sparkView renders the sparkline row: the label, the bars and the range,
// the bar of the selected commit in the accent colour.
func (m *Model) sparkView(pal *theme.Palette) string {
	pts := m.trend.Points
	bars := sparkline(pts)
	sel := -1
	if r, ok := m.Selected(); ok && r.Trend != nil {
		sel = r.Trend.Index
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(sparkLabel))
	for i, bar := range strings.Split(bars, "") {
		if i == sel {
			b.WriteString(lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(bar))
		} else {
			b.WriteString(bar)
		}
	}
	lo, hi := pts[0].Summary.Excess, pts[0].Summary.Excess
	for _, p := range pts {
		lo, hi = min(lo, p.Summary.Excess), max(hi, p.Summary.Excess)
	}
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(" " + itoa(lo) + "–" + itoa(hi) + " · oldest → newest"))
	return m.clip(b.String())
}

// clickSpark maps a sparkline-row x to its point and selects that commit's
// row.
func (m *Model) clickSpark(x int) {
	i := x - lipgloss.Width(sparkLabel)
	if i < 0 || i >= len(m.trend.Points) {
		return
	}
	sha := m.trend.Points[i].Sha
	for j, r := range m.rows {
		if r.Trend != nil && r.Trend.Point.Sha == sha {
			m.cursor = j
			m.clampScroll()
			return
		}
	}
}

// trendKey routes the keys the Trend tab handles itself: enter sends the
// selected commit as the Changes base; the function-level keys (grouping,
// the breakdown, the marks, the baseline, the pins, the function diff, the
// base prompt, the tree) do nothing here, so they neither misfire on a
// commit row nor leak to the other tabs' state.
func (m *Model) trendKey(k string) (tea.Cmd, bool) {
	switch k {
	case "enter":
		r, ok := m.Selected()
		if !ok || r.Trend == nil {
			return nil, true
		}
		rev := r.Trend.Point.Short
		return func() tea.Msg { return TrendBaseMsg{Rev: rev} }, true
	case "b", "v", "D", "space", "m", "i", "p", "d", "R":
		return nil, true
	}
	return nil, false
}

// trendFooter is the Trend tab's hint line.
func (m *Model) trendFooter() string {
	return " · tab view · s sort · S reverse · enter changes since · e csv · E md · y copy · r recompute · / filter · no grouping or breakdown on this tab"
}
