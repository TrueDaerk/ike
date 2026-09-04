package dataview

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/datasrc"
	"ike/internal/theme"
)

// update rewrites the golden files instead of comparing against them:
// go test ./internal/dataview -run Golden -update
var update = flag.Bool("update", false, "rewrite the golden files")

// goldenPage is a loaded page exercising every cell shape the grid draws:
// NULL cells (the ∅ glyph, faint), an empty string next to them, a cell past
// maxColWidth (ellipsis), and more rows than a 24-line pane shows, so the row
// window scrolls.
func goldenPage() datasrc.Page {
	p := datasrc.Page{Columns: []string{"id", "name", "note", "description"}, Total: -1}
	for i := 1; i <= 30; i++ {
		row := []datasrc.Cell{
			{Text: fmt.Sprintf("%d", i)},
			{Text: fmt.Sprintf("user-%d", i)},
			{Text: fmt.Sprintf("note %d", i)},
			{Text: strings.Repeat("long text ", 5)},
		}
		if i%2 == 0 {
			row[2] = datasrc.Cell{Null: true}
		}
		if i%5 == 0 {
			row[3] = datasrc.Cell{Text: ""}
		}
		p.Rows = append(p.Rows, row)
	}
	return p
}

// goldenModel builds a pane over a listed database without touching a file:
// three sidebar objects (an estimated count, an exact one, a view with an
// unknown count), the page above, and the cursors placed by the scenario.
func goldenModel(w, h int, r region, focused bool, rowCur, colOff int) Model {
	m := New("data", "/tmp/app.db", theme.DefaultPalette())
	m.tables = []datasrc.Table{
		{Name: "users", Type: "table", Rows: 1200, Estimated: true},
		{Name: "empty", Type: "table", Rows: 0},
		{Name: "named", Type: "view", Rows: -1},
	}
	m.sel = 0
	m.page = goldenPage()
	m.region = r
	m.rowCur = rowCur
	m.colOff = colOff
	m.SetFocused(focused)
	m.SetSize(w, h)
	return m
}

// TestGoldenViews pins the rendered pane byte-for-byte across the states the
// grid and sidebar renderers distinguish, so the extraction into
// internal/gridview (#2468) can be proven rendering-neutral.
func TestGoldenViews(t *testing.T) {
	cases := []struct {
		name string
		m    Model
	}{
		{"sidebar", goldenModel(100, 24, regionSidebar, true, 0, 0)},
		{"grid", goldenModel(100, 24, regionGrid, true, 12, 0)},
		{"grid-blurred", goldenModel(100, 24, regionGrid, false, 12, 0)},
		{"grid-coloff", goldenModel(100, 24, regionGrid, true, 3, 2)},
		{"narrow", goldenModel(40, 12, regionGrid, true, 12, 0)},
	}
	sorted := goldenModel(100, 24, regionGrid, true, 0, 0)
	sorted.sort = datasrc.Sort{Column: "name", Desc: true}
	cases = append(cases, struct {
		name string
		m    Model
	}{"grid-sorted", sorted})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkGolden(t, tc.name, tc.m.View())
		})
	}
}

// checkGolden compares got against testdata/<name>.golden, rewriting the file
// under -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Fatalf("view drifted from %s:\n got %q\nwant %q", path, got, string(want))
	}
}
