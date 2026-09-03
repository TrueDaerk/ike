package espane

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/esq"
	"ike/internal/theme"
)

// update rewrites the golden files instead of comparing against them:
// go test ./internal/espane -run Golden -update
var update = flag.Bool("update", false, "rewrite the golden files")

// goldenResult is a loaded hit page exercising every cell shape the grid
// draws: absent fields (the ∅ glyph, faint), an empty string next to them, a
// cell past maxColWidth (ellipsis), and more rows than a 24-line pane shows,
// so the row window scrolls.
func goldenResult() *esq.Result {
	r := &esq.Result{Total: 250, Exact: true, Columns: []string{"_id", "_score", "level", "meta", "msg"}}
	for i := 0; i < 30; i++ {
		row := []esq.Cell{
			{Text: fmt.Sprintf("doc-%d", i)},
			{Text: "1"},
			{Text: "info"},
			{Text: fmt.Sprintf(`{"seq":%d}`, i)},
			{Text: strings.Repeat("long text ", 5)},
		}
		if i%2 == 0 {
			row[2] = esq.Cell{Null: true}
		}
		if i%5 == 0 {
			row[4] = esq.Cell{Text: ""}
		}
		r.Rows = append(r.Rows, row)
	}
	return r
}

// goldenModel builds a console over a listed cluster without a network: two
// indices and an alias in the sidebar, the page above loaded for "logs", and
// the cursors placed by the scenario.
func goldenModel(w, h int, r region, focused bool, rowCur, colOff int) Model {
	m := New("es:test", "test", theme.DefaultPalette())
	m.indices = []esq.Index{
		{Name: "empty", Docs: 0},
		{Name: "logs", Docs: 250},
		{Name: "all-logs", Docs: -1, Alias: true},
	}
	m.sel = 1
	m.icur = 1
	m.res = goldenResult()
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
	queried := goldenModel(100, 24, regionGrid, true, 0, 0)
	queried.queries["logs"] = `{"query":{"term":{"level":"info"}}}`
	cases = append(cases, struct {
		name string
		m    Model
	}{"grid-queried", queried})
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
