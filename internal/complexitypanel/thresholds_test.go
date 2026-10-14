package complexitypanel

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/theme"

	_ "ike/plugins/languages/php" // the lang:php selector
)

// thresholds_test.go covers the per-file thresholds (#2960): the warning
// colours, the over / excess totals and the File tab's thresholds label
// follow the complexity.overrides entry matching each file.

// overridePanel is the sample panel under globals 15 / 15 / 40 with
// app/ relaxed to 30 / 40 and PHP's CCN to 20.
func overridePanel(t *testing.T) *Model {
	t.Helper()
	m := newPanel(t)
	c := config.Complexity{CCNWarn: 15, CognitiveWarn: 15, CommentDensityWarn: 40,
		Overrides: []string{"internal/app/** ccn=30 cog=40", "lang:php ccn=20"}}
	m.SetThresholds(complexity.Global(c), complexity.ResolverFor(c, "/proj"))
	return m
}

// rowIndex is the index of the row named name.
func rowIndex(t *testing.T, m *Model, name string) int {
	t.Helper()
	for i, r := range m.Rows() {
		if r.Fn.Name == name {
			return i
		}
	}
	t.Fatalf("no row %q in %v", name, names(m.Rows()))
	return -1
}

func TestPerFileThresholdsOverCount(t *testing.T) {
	m := newPanel(t)
	// The globals: render (CCN 25, cog 30), ListNav (cog 18) and
	// Page::show (CCN 16) are over.
	if s := m.Summary(); s.OverThreshold != 3 || s.Excess != 10+15+3+1 {
		t.Fatalf("global summary = %+v, want 3 over, excess 29", s)
	}
	m = overridePanel(t)
	if s := m.Summary(); s.OverThreshold != 1 || s.Excess != 3 {
		t.Fatalf("overridden summary = %+v, want only ListNav over (excess 3)", s)
	}
	// Back to the globals: no resolver restores the global behaviour.
	m.SetThresholds(complexity.Thresholds{CCN: 15, Cog: 15, Cmt: 40}, nil)
	if s := m.Summary(); s.OverThreshold != 3 {
		t.Fatalf("summary after clearing the overrides = %+v, want 3 over", s)
	}
}

func TestPerFileThresholdsColour(t *testing.T) {
	pal := theme.DefaultPalette()
	warnSeq := strings.TrimSuffix(lipgloss.NewStyle().Foreground(pal.Warning).Render("x"), "x\x1b[m")
	g := newPanel(t)
	g.SetSort(ColNLOC) // render first: the cursor sits on row 0
	g.cursor = rowIndex(t, g, "tiny")
	if row := g.renderRow(pal, rowIndex(t, g, "Page::show")); !strings.Contains(row, warnSeq) {
		t.Fatalf("CCN 16 must warn under the global 15:\n%q", row)
	}
	m := overridePanel(t)
	m.SetSort(ColNLOC)
	m.cursor = rowIndex(t, m, "tiny")
	if row := m.renderRow(pal, rowIndex(t, m, "Page::show")); strings.Contains(row, warnSeq) {
		t.Fatalf("CCN 16 must stay plain under lang:php ccn=20:\n%q", row)
	}
	if row := m.renderRow(pal, rowIndex(t, m, "render")); strings.Contains(row, warnSeq) {
		t.Fatalf("render must stay plain under internal/app/** ccn=30 cog=40:\n%q", row)
	}
	if row := m.renderRow(pal, rowIndex(t, m, "ListNav")); !strings.Contains(row, warnSeq) {
		t.Fatalf("ListNav (no override) must keep the warning colour:\n%q", row)
	}
}

func TestFileTabShowsOverriddenThresholds(t *testing.T) {
	m := overridePanel(t)
	m.SetTab(TabFile)
	m.Follow("/proj/internal/app/app.go", 0)
	if v := m.View(); !strings.Contains(v, "thresholds ccn 30 cog 40 (internal/app/**)") {
		t.Fatalf("the File tab must name the thresholds in force:\n%s", v)
	}
	m.Follow("/proj/internal/ui/list.go", 0)
	if v := m.View(); strings.Contains(v, "thresholds ") {
		t.Fatalf("a file under the globals shows no thresholds label:\n%s", v)
	}
	m.Follow("/proj/internal/app/app.go", 0)
	m.SetTab(TabProject)
	if v := m.View(); strings.Contains(v, "thresholds ") {
		t.Fatalf("the Project tab shows no thresholds label:\n%s", v)
	}
}
