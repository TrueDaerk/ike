package complexitypanel

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
)

// histogram_test.go covers the distribution row (#2953): the 'D' toggle,
// the scaled bars, the warning buckets, the bucket click narrowing through
// min: / max:, the shifted mouse rows and the Mode round trip; and the
// header's percentiles for the sort metric.

// viewLine is line i of the panel's view without escapes.
func viewLine(m *Model, i int) string {
	lines := strings.Split(m.View(), "\n")
	if i >= len(lines) {
		return ""
	}
	return ansi.Strip(lines[i])
}

func TestHistogramToggleAndBars(t *testing.T) {
	m := newPanel(t)
	body := m.bodyHeight()
	if strings.Contains(m.View(), "1–5") {
		t.Fatalf("histogram shown before D:\n%s", m.View())
	}
	m.Update(key("D"))
	if !m.Histogram() || m.bodyHeight() != body-1 {
		t.Fatalf("D: on=%v body %d, want %d", m.Histogram(), m.bodyHeight(), body-1)
	}
	// CCN of sample(): 25, 1, 12, 3, 16 — the widest bucket fills the bar.
	want := " 1–5 ████████ 2 · 6–10 0 · 11–15 ████ 1 · 16–25 ████████ 2 · 26+ 0"
	if got := viewLine(m, 2); got != want {
		t.Errorf("histogram row = %q, want %q", got, want)
	}
	if got := viewLine(m, 3); !strings.Contains(got, "ccn ▼") {
		t.Errorf("column header moved off line 3: %q", got)
	}
	// Sorting by cognitive counts cognitive: 30, 0, 18, 2, 4.
	m.SetSort(ColCog)
	if got := viewLine(m, 2); !strings.HasPrefix(got, " 1–5 ████████ 3 · 6–10 0 · 11–15 0 · 16–25 ██▋ 1 · 26+ ██▋ 1") {
		t.Errorf("cognitive histogram row = %q", got)
	}
	// The Changes tab has no histogram row.
	m.SetTab(TabChanges)
	if m.histShown() || m.chromeRows() != 4 {
		t.Errorf("Changes tab shows the histogram")
	}
	m.SetTab(TabProject)
	m.Update(key("D"))
	if m.Histogram() || m.bodyHeight() != body || strings.Contains(m.View(), "1–5") {
		t.Errorf("second D did not hide the row")
	}
}

func TestHistogramWarnBuckets(t *testing.T) {
	m := newPanel(t)
	m.ToggleHistogram()
	pal := theme.DefaultPalette()
	warnSeq := strings.TrimSuffix(lipgloss.NewStyle().Foreground(pal.Warning).Render("x"), "x\x1b[m")
	row := strings.Split(m.View(), "\n")[2]
	for label, warn := range map[string]bool{"1–5": false, "6–10": false, "11–15": true, "16–25": true, "26+": true} {
		if got := strings.Contains(row, warnSeq+label); got != warn {
			t.Errorf("bucket %s warn = %v, want %v:\n%q", label, got, warn, row)
		}
	}
}

func TestHistogramBarScale(t *testing.T) {
	for _, c := range []struct {
		n, widest int
		want      string
	}{
		{0, 10, ""},
		{10, 10, "████████"},
		{5, 10, "████"},
		{1, 1000, "▏"},
		{3, 16, "█▌"},
	} {
		if got := histBar(c.n, c.widest); got != c.want {
			t.Errorf("histBar(%d, %d) = %q, want %q", c.n, c.widest, got, c.want)
		}
	}
}

func TestHistogramBucketClick(t *testing.T) {
	m := newPanel(t)
	m.Update(key("D"))
	m.filter.SetText("lang:go")
	m.Refresh()
	// The 16–25 bucket spans x 42..57 (" 1–5 ████████ 2 · 6–10 0 · 11–15
	// ████ 1 · 16–25 …").
	m.Click(45, 2)
	if got := m.filter.Text(); got != "lang:go min:16 max:25" {
		t.Fatalf("filter after bucket click = %q", got)
	}
	if got := strings.Join(names(m.Rows()), ","); got != "render" {
		t.Errorf("rows = %s, want render (Page::show is php)", got)
	}
	// The row still counts the whole tab; the active bucket is marked.
	if got := viewLine(m, 2); !strings.Contains(got, "16–25 ████████ 2") || m.activeBucket() != 3 {
		t.Errorf("row %q active %d", got, m.activeBucket())
	}
	// A second click on the same bucket clears both keys.
	m.Click(45, 2)
	if got := m.filter.Text(); got != "lang:go" {
		t.Errorf("filter after second click = %q, want lang:go", got)
	}
	// The first bucket keeps a cognitive 0, the last has no max; a sort
	// column other than the counted metric moves to it.
	m.SetSort(ColNLOC)
	m.filter.SetText("")
	m.Refresh()
	m.Click(2, 2)
	if got := m.filter.Text(); got != "min:0 max:5" || m.sortCol != ColCCN {
		t.Errorf("first bucket: filter %q sort %v", got, m.sortCol)
	}
	if got := strings.Join(names(m.Rows()), ","); got != "inner,tiny" {
		t.Errorf("1–5 rows = %s", got)
	}
	m.SetSort(ColCog)
	m.filter.SetText("")
	m.Refresh()
	segs := m.histSegments()
	m.Click(segs[4].start, 2)
	if got := m.filter.Text(); got != "min:26" {
		t.Errorf("last bucket filter = %q, want min:26", got)
	}
	if got := strings.Join(names(m.Rows()), ","); got != "render" {
		t.Errorf("26+ cognitive rows = %s", got)
	}
	// A click on the separator does nothing.
	m.Click(segs[0].end+1, 2)
	if got := m.filter.Text(); got != "min:26" {
		t.Errorf("separator click changed the filter to %q", got)
	}
}

func TestHistogramShiftsMouseRows(t *testing.T) {
	m := newPanel(t)
	m.ToggleHistogram()
	// y 3 is now the column header: a click on the function column sorts.
	m.Click(25, 3)
	if c, _ := m.SortColumn(); c != ColFunc {
		t.Errorf("column header click at y 3 sorted %v, want function", c)
	}
	// The list starts at y 4.
	m.Click(2, 5)
	if m.Cursor() != 1 {
		t.Errorf("row click at y 5 selected %d, want 1", m.Cursor())
	}
}

func TestHistogramModeRoundTrip(t *testing.T) {
	m := testsPanel(t)
	m.Update(key("D"))
	m.Update(key("t"))
	if got := m.Mode(); got != "tests:hidden,hist:on" {
		t.Fatalf("mode = %q", got)
	}
	r := testsPanel(t)
	r.SetMode("project:class,hist:on")
	if !r.Histogram() || r.HideTests() || r.Mode() != "project:class,hist:on" {
		t.Errorf("restored %v %v %q", r.Histogram(), r.HideTests(), r.Mode())
	}
	r.SetMode("")
	if r.Histogram() {
		t.Error("empty mode left the histogram on")
	}
}

func TestTotalsPercentilesFollowSortMetric(t *testing.T) {
	m := newPanel(t)
	m.SetSize(240, 12)
	// CCN 1, 3, 12, 16, 25: p50 12, p90 and p99 25.
	head := viewLine(m, 0)
	if !strings.Contains(head, "CCN Σ57 avg 11.4 p50 12 p90 25 p99 25 max 25") || !strings.Contains(head, "cog Σ54 avg 10.8 max 30") {
		t.Errorf("CCN-sorted header = %q", head)
	}
	m.SetSort(ColCog)
	head = viewLine(m, 0)
	if !strings.Contains(head, "CCN Σ57 avg 11.4 max 25") || !strings.Contains(head, "cog Σ54 avg 10.8 p50 4 p90 30 p99 30 max 30") {
		t.Errorf("cog-sorted header = %q", head)
	}
}
