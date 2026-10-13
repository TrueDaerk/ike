package complexitypanel

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// cmt_test.go covers the comment-density column (#2934): the cmt% cell
// after depth, its sort and metric:cmt filter, the warning colour at the
// complexity.comment_density_warn threshold, the summary, the CSV export
// and the Changes tab's before→after cell.

// withCmt sets a unit's comment lines and the density they imply.
func withCmt(f complexity.Function, comment int) complexity.Function {
	f.CommentLines = comment
	f.CommentDensity = float64(comment) / float64(f.NLOC+comment)
	return f
}

// cmtPanel is the sample snapshot with three units carrying comments:
// render 21 code / 9 comment (30 %), ListNav 36 / 44 (55 %), tiny 3 / 2
// (40 %).
func cmtPanel(t *testing.T) *Model {
	t.Helper()
	v := sample()
	app := v.Files["/proj/internal/app/app.go"]
	v.Files["/proj/internal/app/app.go"] = []complexity.Function{withCmt(app[0], 9), withCmt(app[1], 2)}
	list := v.Files["/proj/internal/ui/list.go"]
	v.Files["/proj/internal/ui/list.go"] = []complexity.Function{withCmt(list[0], 44), list[1]}
	m := newPanel(t)
	m.Set(v, complexity.Stats{Enabled: true})
	return m
}

func TestCmtColumnFollowsDepth(t *testing.T) {
	m := cmtPanel(t)
	labels := m.headerLabels()
	if labels[len(labels)-1] != "cmt%" || columnLabels[ColDepth] != "depth" || projectLayout[len(projectLayout)-2] != ColDepth {
		t.Fatalf("Project header = %v, want cmt%% right after depth", labels)
	}
	m.SetTab(TabFile)
	if l := m.headerLabels(); l[len(l)-1] != "cmt%" {
		t.Fatalf("File header = %v", l)
	}
	m.SetTab(TabProject)
	for _, r := range m.Rows() {
		if r.Fn.Name == "ListNav" && r.cell(ColCmt) != "55" {
			t.Fatalf("ListNav cmt%% cell = %q, want 55", r.cell(ColCmt))
		}
	}
	if !strings.Contains(m.View(), "cmt%") {
		t.Fatalf("the view must show the column:\n%s", m.View())
	}
}

func TestCmtSortAndFilter(t *testing.T) {
	m := cmtPanel(t)
	m.SetSort(ColCmt)
	if got := names(m.Rows()); got[0] != "ListNav" || got[1] != "tiny" || got[2] != "render" {
		t.Fatalf("cmt order = %v", got)
	}
	// 's' reaches the column on the way from depth.
	m.SetSort(ColDepth)
	m.Update(key("s"))
	if col, desc := m.SortColumn(); col != ColCmt || !desc {
		t.Fatalf("s after depth = %v desc=%v, want cmt descending", col, desc)
	}
	m.SetSort(ColCCN)
	m.filter.SetText("metric:cmt min:40")
	m.Refresh()
	if got := strings.Join(names(m.Rows()), ","); got != "ListNav,tiny" {
		t.Fatalf("metric:cmt min:40 rows = %s", got)
	}
}

func TestCmtWarnThreshold(t *testing.T) {
	m := cmtPanel(t)
	m.SetWarn(100, 100, 40)
	m.SetSort(ColCmt)
	// Row 1 is tiny at exactly 40 %, row 2 render at 30 %; neither holds
	// the cursor, so only the warning colour can tell them apart.
	pal := theme.DefaultPalette()
	warnSeq := strings.TrimSuffix(lipgloss.NewStyle().Foreground(pal.Warning).Render("x"), "x\x1b[m")
	if warn := m.renderRow(pal, 1); !strings.Contains(warn, warnSeq) {
		t.Fatalf("a cmt%% at the threshold must take the warning colour:\n%q", warn)
	}
	if plain := m.renderRow(pal, 2); strings.Contains(plain, warnSeq) {
		t.Fatalf("a cmt%% below the threshold must stay plain:\n%q", plain)
	}
	if s := m.Summary(); s.OverThreshold != 2 || s.CommentLines != 55 {
		t.Fatalf("summary = %+v, want 2 over (55 %% and 40 %%) and 55 comment lines", s)
	}
	if !strings.Contains(m.View(), "cmt ") {
		t.Fatalf("the totals must carry the aggregate density:\n%s", m.View())
	}
	// 0 turns the check off.
	m.SetWarn(100, 100, 0)
	if s := m.Summary(); s.OverThreshold != 0 {
		t.Fatalf("cmt warn 0 still counts %d over", s.OverThreshold)
	}
	if off := m.renderRow(pal, 1); strings.Contains(off, warnSeq) {
		t.Fatalf("a disabled threshold must not paint the cell:\n%q", off)
	}
}

func TestCmtCSVAndChanges(t *testing.T) {
	m := cmtPanel(t)
	m.SetSort(ColCmt)
	lines := strings.Split(strings.TrimSpace(m.CSV()), "\n")
	if !strings.HasSuffix(lines[1], ",44,55") || !strings.HasPrefix(lines[1], "internal/ui/list.go,ListNav,") {
		t.Fatalf("csv first row = %q", lines[1])
	}
	base := map[string][]complexity.Function{"/proj/a.go": {withCmt(fn("f", 0, 9, 3, 2), 0)}}
	head := map[string][]complexity.Function{"/proj/a.go": {withCmt(fn("f", 0, 9, 3, 2), 10)}}
	m.SetChanges(Changes{Rev: "HEAD", Diff: complexity.DiffProject(base, head, 15, 15, 40), Ready: true})
	m.SetTab(TabChanges)
	rows := m.Rows()
	if len(rows) != 1 || rows[0].cell(ColCmt) != "0→50 (+50)" || rows[0].DeltaCmt() != 50 {
		t.Fatalf("changes rows = %+v", rows)
	}
	if l := m.headerLabels(); !strings.HasPrefix(l[len(l)-1], "cmt%") {
		t.Fatalf("Changes header = %v", l)
	}
	csv := strings.Split(strings.TrimSpace(m.CSV()), "\n")
	if !strings.HasSuffix(csv[0], "cmt_before,cmt_after,cmt_delta") || !strings.HasSuffix(csv[1], ",0,50,50") {
		t.Fatalf("changes csv = %q", m.CSV())
	}
}
