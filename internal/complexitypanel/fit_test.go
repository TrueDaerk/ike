package complexitypanel

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// fit_test.go covers the column budget (#2992): long text cells are
// truncated with an ellipsis so the metric columns stay on the pane.

var (
	longDir  = "internal/" + strings.Repeat("deeply/", 5) + "nested/package/" // 80 cells with the file name
	longFile = longDir + strings.Repeat("x", 80-len(longDir)-len("_file.go")) + "_file.go"
	longName = "SomeLongClassName::" + strings.Repeat("someVeryLongMethod", 3)[:41] // 60 cells
)

func longPanel(t *testing.T, width int) *Model {
	t.Helper()
	if len(longFile) != 80 || len(longName) != 60 {
		t.Fatalf("fixture widths %d / %d", len(longFile), len(longName))
	}
	m := New(theme.DefaultPalette())
	m.pathLang = langByExt
	m.SetSize(width, 12)
	m.SetRoot("/proj")
	m.Set(complexity.View{Gen: 1, Files: map[string][]complexity.Function{
		"/proj/" + longFile:    {fn(longName, 0, 40, 123, 456)},
		"/proj/short/short.go": {fn("tiny", 0, 2, 1, 0)},
	}}, complexity.Stats{Enabled: true})
	return &m
}

// gridLines are the view's plain header and data lines (the column header
// is the line holding "ccn").
func gridLines(t *testing.T, m *Model) []string {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, l := range lines {
		if strings.Contains(l, "ccn") && strings.Contains(l, "nloc") {
			return lines[i:]
		}
	}
	t.Fatalf("no column header in:\n%s", m.View())
	return nil
}

func TestLongCellsKeepMetricsVisible(t *testing.T) {
	m := longPanel(t, 100)
	lines := gridLines(t, m)
	row := lines[1]
	if lipgloss.Width(row) > 100 {
		t.Fatalf("row is %d cells wide:\n%s", lipgloss.Width(row), row)
	}
	// Every metric of the long row is fully on the pane: line, ccn, cog,
	// nloc, params, depth, cmt%.
	fields := strings.Fields(row)
	if n := len(fields); n < 9 || strings.Join(fields[n-7:], " ") != "1 123 456 41 1 2 0" {
		t.Fatalf("metrics missing from the row:\n%s", row)
	}
	file, name := fields[0], fields[1]
	if !strings.HasPrefix(file, "…") || !strings.HasSuffix(file, "_file.go") {
		t.Fatalf("file cell %q must be cut from the left, keeping the file name", file)
	}
	if !strings.HasSuffix(name, "…") || !strings.HasPrefix(name, "SomeLongClassName::") {
		t.Fatalf("function cell %q must be cut from the right, keeping its prefix", name)
	}
	// The header keeps every label and the sort marker.
	for _, l := range []string{"file", "function", "ccn ▼", "cog", "nloc", "cmt%"} {
		if !strings.Contains(lines[0], l) {
			t.Fatalf("header lost %q:\n%s", l, lines[0])
		}
	}
	// The footer shows the cursor row's full text; the export keeps it.
	view := ansi.Strip(m.View())
	foot := strings.Split(view, "\n")
	if last := foot[len(foot)-1]; !strings.Contains(last, longFile[:40]) {
		t.Fatalf("footer must lead with the full path:\n%s", last)
	}
	if !strings.Contains(m.CSV(), longFile) || !strings.Contains(m.CSV(), longName) {
		t.Fatalf("CSV must keep the full text:\n%s", m.CSV())
	}
	if !strings.Contains(m.Markdown(), longName) {
		t.Fatalf("Markdown must keep the full name:\n%s", m.Markdown())
	}
}

func TestShortCellsAreUntouched(t *testing.T) {
	m := longPanel(t, 100)
	row := gridLines(t, m)[2]
	if !strings.Contains(row, "short/short.go") || !strings.Contains(row, "tiny") || strings.Contains(row, "…") {
		t.Fatalf("short row changed:\n%s", row)
	}
}

func TestWidePaneKeepsFullText(t *testing.T) {
	m := longPanel(t, 220)
	row := gridLines(t, m)[1]
	if !strings.Contains(row, longFile) || !strings.Contains(row, longName) {
		t.Fatalf("a pane wide enough must not truncate:\n%s", row)
	}
	foot := strings.Split(ansi.Strip(m.View()), "\n")
	if strings.Contains(foot[len(foot)-1], longFile) {
		t.Fatalf("nothing truncated, the footer must not repeat the row")
	}
}

func TestTruncatedFunctionKeepsMarks(t *testing.T) {
	m := longPanel(t, 100)
	m.Follow("/proj/"+longFile, 5)
	m.SetTab(TabFile)
	if m.CaretRow() != 0 {
		t.Fatalf("caret row = %d", m.CaretRow())
	}
	view := m.View()
	row := gridLines(t, m)[1]
	if !strings.HasPrefix(strings.TrimSpace(row), caretMark+"SomeLong") || !strings.Contains(row, "…") {
		t.Fatalf("caret row must keep the caret mark on a truncated name:\n%s", row)
	}
	// The occurrence highlight still paints the truncated cell.
	occ := lipgloss.NewStyle().Background(theme.DefaultPalette().OccurrenceRead).Render("x")
	bg := occ[strings.Index(occ, "48;"):strings.Index(occ, "m")] // the background parameter
	if !strings.Contains(view, bg) {
		t.Fatalf("truncated caret cell lost its occurrence highlight %q:\n%q", bg, view)
	}
}

func TestFitTextKeepsTrailingMarks(t *testing.T) {
	s := caretMark + longName + acceptedMark + pinMark
	got := fitText(ColFunc, s, 30)
	if lipgloss.Width(got) != 30 || !strings.HasPrefix(got, caretMark+"SomeLong") || !strings.HasSuffix(got, "…"+acceptedMark+pinMark) {
		t.Fatalf("fitText = %q", got)
	}
	if got := fitText(ColFile, longFile, 20); !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "_file.go") {
		t.Fatalf("file fitText = %q", got)
	}
	if got := fitText(ColSubject, "fix: a very long commit subject line", 15); got != "fix: a very lo…" {
		t.Fatalf("subject fitText = %q", got)
	}
}

func TestFitLabelsKeepsSortMarker(t *testing.T) {
	m := longPanel(t, 100)
	m.widths = []int{6, 6}
	got := m.fitLabels([]string{"function ▼", "file"})
	if got[0] != "fun… ▼" || got[1] != "file" {
		t.Fatalf("labels = %q", got)
	}
}
