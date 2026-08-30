package app

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// pickerpreview_test.go covers the app-side consumers of the shared code
// preview (#2053): the symbol picker and the bookmarks picker point their rows
// at file positions and opt into the palette's code column.

// TestSymbolPickerRowsCarryPreviewTargets: every cached workspace-symbol row
// points the preview at its declaration (1-based), and the mode opts in.
func TestSymbolPickerRowsCarryPreviewTargets(t *testing.T) {
	s := &symbolMode{}
	if !s.CodePreview() {
		t.Fatal("the symbol picker must render the code column")
	}
	s.lastSent = "Help"
	s.SetHits("Help", []ilsp.SymbolHit{
		{Name: "Helper", Ref: ilsp.Reference{Path: "a.go", Line: 3, Preview: "func Helper() {"}},
		{Name: "Helped", Ref: ilsp.Reference{Path: "sub/b.go", Line: 0}},
	})
	items := s.Results("Help", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("want 2 rows, got %d", len(items))
	}
	want := map[string]palette.PreviewTarget{
		"Helper": {Path: "a.go", Line: 4},
		"Helped": {Path: "sub/b.go", Line: 1},
	}
	for _, it := range items {
		// Targets carry match ranges since #2327 and are no longer
		// comparable; the picker rows only ever fill in path and line.
		if got, w := it.Preview, want[it.Title]; got.Path != w.Path || got.Line != w.Line {
			t.Fatalf("row %q preview = %+v, want %+v", it.Title, got, w)
		}
	}
	// The class category is a filtered view of the same cache, so its rows
	// inherit the targets without a second source (#1849).
	for _, it := range newClassMode(s).Results("Help", palette.Context{}) {
		if it.Preview.Path == "" {
			t.Fatalf("class row %q lost its preview target", it.Title)
		}
	}
}

// TestBookmarksPickerRowsCarryPreviewTargets: local marks, global marks and
// project bookmarks all point at their file and 1-based line; a pathless mark
// carries none, so its column stays blank instead of claiming a bad read.
func TestBookmarksPickerRowsCarryPreviewTargets(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n  two\nthree\n")
	b := writeTemp(t, dir, "b.txt", "bee line\n")
	m := openApp(t, a)
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	ed.SetCursor(1, 2)
	*ed, _ = ed.Update(keyMsg('m'))
	*ed, _ = ed.Update(keyMsg('c'))
	m.gmarks.Set('B', b, 0, 3)
	m.bmarks.Add(bpKey(a), 2)

	mode := &bookmarksMode{}
	if !mode.CodePreview() {
		t.Fatal("the bookmarks picker must render the code column")
	}
	mode.Set(ed, m.gmarks, m.bmarks)
	items := mode.Results("", palette.Context{})
	if len(items) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(items), items)
	}
	for _, it := range items {
		if it.Preview.Path == "" || it.Preview.Line < 1 {
			t.Fatalf("row %q has no preview target: %+v", it.Title, it.Preview)
		}
	}
	if got := items[0]; !strings.Contains(got.Title, "b.txt") || got.Preview.Path != b || got.Preview.Line != 1 {
		t.Fatalf("global mark row = %q / %+v, want %s:1", got.Title, got.Preview, b)
	}
	if got := items[1]; got.Preview.Path != a || got.Preview.Line != 2 {
		t.Fatalf("local mark row = %q / %+v, want %s:2", got.Title, got.Preview, a)
	}
	if got := items[2]; got.Preview.Path != a || got.Preview.Line != 3 {
		t.Fatalf("project bookmark row = %q / %+v, want %s:3", got.Title, got.Preview, a)
	}
}

// TestBookmarksPickerPathlessMarkHasNoTarget: a mark in a scratch buffer has
// no file, so the row leaves the preview column empty (#2053).
func TestBookmarksPickerPathlessMarkHasNoTarget(t *testing.T) {
	if got := markTarget("", 7); got.Path != "" || got.Line != 0 {
		t.Fatalf("pathless mark target = %+v, want the zero target", got)
	}
	if got := markTarget("a.go", 7); got.Line != 8 {
		t.Fatalf("mark target line = %d, want the 1-based 8", got.Line)
	}
}
