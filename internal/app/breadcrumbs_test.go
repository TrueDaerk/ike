package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/theme"
)

// crumbTree is a small two-level symbol tree over a 10-line file: Outer spans
// 0-based lines 1..8, its child inner spans 3..5.
func crumbTree() []ilsp.SymbolNode {
	return []ilsp.SymbolNode{{
		Name: "Outer", Kind: 5, Line: 1, Col: 0, EndLine: 8,
		Children: []ilsp.SymbolNode{{Name: "inner", Kind: 6, Line: 3, Col: 4, EndLine: 5}},
	}}
}

// crumbModel is a sized model with one open 10-line file and the editor pane's
// key and rect. No symbol data is fed yet.
func crumbModel(t *testing.T) (Model, layout.Rect, string, string) {
	t.Helper()
	m := sized(t, 100, 40)
	path := filepath.Join(t.TempDir(), "main.go")
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = "line body text"
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	for key, r := range m.lay.Panes {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			return m, r, key, canonicalPath(path)
		}
	}
	t.Fatal("setup: no editor pane")
	return m, layout.Rect{}, "", ""
}

// feedSymbols delivers a documentSymbol reply; the settled Update pass then
// claims the breadcrumbs row and re-runs layout.
func feedSymbols(m Model, path string, syms []ilsp.SymbolNode) Model {
	return step(m, ilsp.DocumentSymbolsMsg{Path: path, Symbols: syms})
}

func TestSymbolChainDerivation(t *testing.T) {
	tree := []ilsp.SymbolNode{
		{Name: "A", Line: 0, EndLine: 4, Children: []ilsp.SymbolNode{
			{Name: "a1", Line: 1, EndLine: 2},
			{Name: "a2", Line: 3, EndLine: 4},
		}},
		{Name: "B", Line: 6, EndLine: 9},
	}
	cases := []struct {
		line int
		want []string
	}{
		{0, []string{"A"}},
		{2, []string{"A", "a1"}},
		{3, []string{"A", "a2"}}, // later containing sibling wins (most specific)
		{7, []string{"B"}},
		{5, nil}, // between symbols: no enclosing chain
	}
	for _, c := range cases {
		chain := symbolChain(tree, c.line)
		got := make([]string, 0, len(chain))
		for _, n := range chain {
			got = append(got, n.Name)
		}
		if strings.Join(got, "/") != strings.Join(c.want, "/") {
			t.Errorf("line %d: chain %v, want %v", c.line, got, c.want)
		}
	}
}

func TestCrumbTruncationKeepsDeepestSegments(t *testing.T) {
	labels := []string{"file.go", "Alpha", "Beta"}
	// Wide enough: everything visible from the first segment.
	if lo := crumbWindow(labels, 80); lo != 0 {
		t.Fatalf("wide row must show all segments, lo=%d", lo)
	}
	// Too narrow for the first segment: it is elided, the deep ones stay.
	if lo := crumbWindow(labels, 17); lo != 1 {
		t.Fatalf("narrow row must drop the front segment, lo=%d", lo)
	}
	row := ansi.Strip(renderCrumbRow(labels, 17, theme.DefaultPalette()))
	if !strings.HasPrefix(row, "… ▸ ") || !strings.Contains(row, "Beta") {
		t.Fatalf("narrow row must lead with an ellipsis and keep the deepest segment: %q", row)
	}
	if ansi.StringWidth(row) > 17 {
		t.Fatalf("row overflows its width: %d cells", ansi.StringWidth(row))
	}
	// A lone segment that still overflows truncates with a trailing ellipsis.
	lone := ansi.Strip(renderCrumbRow([]string{"averylongfilename.go"}, 8, theme.DefaultPalette()))
	if ansi.StringWidth(lone) > 8 || !strings.HasSuffix(lone, "…") {
		t.Fatalf("lone overflowing segment must truncate: %q", lone)
	}
}

func TestCrumbHitZones(t *testing.T) {
	labels := []string{"ab", "cd"} // "ab ▸ cd": ab=0..1, sep=2..4, cd=5..6
	cases := []struct {
		x, want int
	}{
		{0, 0}, {1, 0}, {2, -1}, {4, -1}, {5, 1}, {6, 1}, {7, -1}, {-1, -1},
	}
	for _, c := range cases {
		if got := crumbHit(labels, 40, c.x); got != c.want {
			t.Errorf("x=%d: hit %d, want %d", c.x, got, c.want)
		}
	}
	// With a front elision the leading "… ▸ " cells hit nothing.
	long := []string{"longfirstsegment", "x"}
	if got := crumbHit(long, 6, 2); got != -1 {
		t.Fatalf("lead ellipsis cells must miss, got %d", got)
	}
	if got := crumbHit(long, 6, 4); got != 1 {
		t.Fatalf("visible segment after the lead must hit, got %d", got)
	}
}

func TestBreadcrumbRowAppearsWithSymbolData(t *testing.T) {
	m, _, key, path := crumbModel(t)
	inst := m.activeWS().Panes.Get(key)
	if rows := m.breadcrumbRows(inst); rows != 0 {
		t.Fatalf("no symbol data: the row must be hidden, rows=%d", rows)
	}
	m.activeEditor().JumpTo(3, 0) // inside Outer and inner
	m = feedSymbols(m, path, crumbTree())
	inst = m.activeWS().Panes.Get(key)
	if rows := m.breadcrumbRows(inst); rows != 1 {
		t.Fatalf("with symbol data the row must render, rows=%d", rows)
	}
	view := ansi.Strip(m.render())
	if !strings.Contains(view, "main.go ▸ Outer ▸ inner") {
		t.Fatalf("view missing the breadcrumb chain:\n%s", view)
	}
}

func TestBreadcrumbRowHiddenBetweenSymbols(t *testing.T) {
	m, _, _, path := crumbModel(t)
	m = feedSymbols(m, path, crumbTree()) // cursor still on line 0, outside Outer
	view := ansi.Strip(m.render())
	if !strings.Contains(view, "main.go") || strings.Contains(view, "main.go ▸ ") {
		t.Fatalf("outside any symbol the row must show only the basename:\n%s", view)
	}
}

func TestBreadcrumbClickJumpsToSymbol(t *testing.T) {
	m, r, _, path := crumbModel(t)
	m.activeEditor().JumpTo(3, 0)
	m = feedSymbols(m, path, crumbTree())
	// Row layout: "main.go ▸ Outer ▸ inner" — "Outer" starts at cell 10.
	x := r.X + paneContentX + 11
	y := r.Y + 2 // the breadcrumbs row, right under the title row
	m = step(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if line, col := m.activeEditor().Cursor(); line != 2 || col != 1 {
		t.Fatalf("clicking the Outer segment must jump to its position, cursor %d:%d", line, col)
	}
	// The separator between segments dispatches nothing.
	before, _ := m.activeEditor().Cursor()
	m = step(m, tea.MouseClickMsg{X: r.X + paneContentX + 8, Y: y, Button: tea.MouseLeft})
	if line, _ := m.activeEditor().Cursor(); line != before {
		t.Fatalf("separator click must not move the cursor, line %d", line)
	}
}

func TestBreadcrumbClickRecordsNavHistory(t *testing.T) {
	m, r, _, path := crumbModel(t)
	m.activeEditor().JumpTo(3, 0)
	m = feedSymbols(m, path, crumbTree())
	m = step(m, tea.MouseClickMsg{X: r.X + paneContentX + 11, Y: r.Y + 2, Button: tea.MouseLeft})
	// The jump went through the openPathAt funnel: navigating back must
	// return to the pre-jump position (0-based line 3).
	pos, ok := m.navHist.Back(m.currentNavPos())
	if !ok || pos.Line != 3 {
		t.Fatalf("a breadcrumb jump must record nav history, back=%v ok=%v", pos, ok)
	}
}

// TestBreadcrumbGeometryContentClick is the risky-geometry guard (#1153): the
// same buffer line must be reachable by mouse with the row hidden and shown —
// the content-local translation shifts by exactly the row the layout reserved.
func TestBreadcrumbGeometryContentClick(t *testing.T) {
	m, r, _, path := crumbModel(t)
	// Row hidden: content row 2 is buffer line 3 (1-based).
	m = step(m, tea.MouseClickMsg{X: r.X + paneContentX + 8, Y: r.Y + paneContentY + 2, Button: tea.MouseLeft})
	if line, _ := m.activeEditor().Cursor(); line != 3 {
		t.Fatalf("without breadcrumbs: click on content row 2 must land on line 3, got %d", line)
	}
	m = feedSymbols(m, path, crumbTree())
	// Row shown: the content moved one row down, the same click cell now maps
	// to buffer line 2 …
	m = step(m, tea.MouseClickMsg{X: r.X + paneContentX + 8, Y: r.Y + paneContentY + 2, Button: tea.MouseLeft})
	if line, _ := m.activeEditor().Cursor(); line != 2 {
		t.Fatalf("with breadcrumbs: the shifted click must land on line 2, got %d", line)
	}
	// … and one cell lower reaches line 3 again.
	m = step(m, tea.MouseClickMsg{X: r.X + paneContentX + 8, Y: r.Y + paneContentY + 3, Button: tea.MouseLeft})
	if line, _ := m.activeEditor().Cursor(); line != 3 {
		t.Fatalf("with breadcrumbs: content row 3 must land on line 3, got %d", line)
	}
}

func TestBreadcrumbConfigToggleLive(t *testing.T) {
	cfg := host.MapConfig{}
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(strings.Repeat("text\n", 10)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = m.openPath(path, false)
	m = out.(Model)
	m.activeEditor().JumpTo(3, 0)
	m = feedSymbols(m, canonicalPath(path), crumbTree())
	key := m.activeEditorKey()
	if rows := m.breadcrumbRows(m.activeWS().Panes.Get(key)); rows != 1 {
		t.Fatalf("default-on breadcrumbs must render, rows=%d", rows)
	}
	// Turning the setting off applies on the next settled pass: the row
	// releases its line without a restart.
	cfg["editor.breadcrumbs"] = "false"
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if rows := m.breadcrumbRows(m.activeWS().Panes.Get(key)); rows != 0 {
		t.Fatalf("toggling editor.breadcrumbs off must hide the row, rows=%d", rows)
	}
	cfg["editor.breadcrumbs"] = "true"
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if rows := m.breadcrumbRows(m.activeWS().Panes.Get(key)); rows != 1 {
		t.Fatalf("toggling editor.breadcrumbs back on must restore the row, rows=%d", rows)
	}
}
