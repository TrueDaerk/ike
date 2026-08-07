package locations

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

func items() []Item {
	return []Item{
		{Path: "a.go", Line: 1, StartCol: 0, EndCol: 3, Text: "foo bar"},
		{Path: "a.go", Line: 9, StartCol: 4, EndCol: 7, Text: "foo bar"},
		{Path: "b.go", Line: 2, StartCol: 0, EndCol: 3, Text: "foo again"},
	}
}

func TestAppendGroupsByFile(t *testing.T) {
	var l List
	l.Append(items())
	if l.Total() != 3 || l.Files() != 2 {
		t.Fatalf("total=%d files=%d, want 3/2", l.Total(), l.Files())
	}
	// A later batch for a new file starts a new group; the same trailing file
	// extends its group.
	l.Append([]Item{{Path: "b.go", Line: 5, Text: "x"}})
	if l.Files() != 2 || l.Total() != 4 {
		t.Fatalf("contiguous append must extend the last group: files=%d total=%d", l.Files(), l.Total())
	}
}

func TestMoveClampsAndCurrent(t *testing.T) {
	var l List
	l.Append(items())
	l.Move(1)
	if it, _ := l.Current(); it.Line != 9 {
		t.Fatalf("cursor should be on the second item, got line %d", it.Line)
	}
	l.Move(99)
	if it, _ := l.Current(); it.Path != "b.go" {
		t.Fatalf("cursor must clamp to the last item, got %s", it.Path)
	}
	l.Move(-99)
	if it, _ := l.Current(); it.Line != 1 || it.Path != "a.go" {
		t.Fatalf("cursor must clamp to the first item, got %+v", it)
	}
}

func TestAdvanceWraps(t *testing.T) {
	var l List
	l.Append(items())
	if it, ok := l.Advance(-1); !ok || it.Path != "b.go" {
		t.Fatalf("advance(-1) from the first item must wrap to the last, got %+v", it)
	}
	if it, _ := l.Advance(1); it.Line != 1 || it.Path != "a.go" {
		t.Fatalf("advance(1) from the last item must wrap to the first, got %+v", it)
	}
}

func TestRenderShowsGroupsAndCursor(t *testing.T) {
	var l List
	l.Append(items())
	out := l.Render(60, 10, theme.DefaultPalette(), nil)
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Fatalf("render missing group headers:\n%s", out)
	}
	if !strings.Contains(out, "(2)") {
		t.Fatalf("render missing per-file count:\n%s", out)
	}
	// The match range renders styled, so assert the unstyled tail.
	if !strings.Contains(out, " bar") {
		t.Fatalf("render missing item text:\n%s", out)
	}
}

func TestRenderScrollsCursorIntoView(t *testing.T) {
	var l List
	var many []Item
	for i := 1; i <= 30; i++ {
		many = append(many, Item{Path: "big.go", Line: i, Text: "needle row"})
	}
	l.Append(many)
	l.Move(29)
	out := l.Render(40, 5, theme.DefaultPalette(), nil)
	if !strings.Contains(out, "30:") {
		t.Fatalf("cursor row must be scrolled into view:\n%s", out)
	}
}

func TestRenderEmpty(t *testing.T) {
	var l List
	if out := l.Render(40, 5, theme.DefaultPalette(), nil); out != "" {
		t.Fatalf("empty list must render empty, got %q", out)
	}
}

func TestItemAtMapsRowsToItems(t *testing.T) {
	var l List
	l.Append(items())
	l.Render(60, 10, theme.DefaultPalette(), nil) // top = 0
	// Rows: 0 header a.go, 1 item0, 2 item1, 3 header b.go, 4 item2.
	if _, ok := l.ItemAt(0); ok {
		t.Fatal("header row must not map to an item")
	}
	for row, want := range map[int]int{1: 0, 2: 1, 4: 2} {
		if got, ok := l.ItemAt(row); !ok || got != want {
			t.Fatalf("ItemAt(%d) = %d,%v want %d,true", row, got, ok, want)
		}
	}
	if _, ok := l.ItemAt(3); ok {
		t.Fatal("second header row must not map to an item")
	}
	if _, ok := l.ItemAt(99); ok {
		t.Fatal("row past the end must not map to an item")
	}
	if _, ok := l.ItemAt(-1); ok {
		t.Fatal("negative row must not map to an item")
	}
}

func TestItemAtHonorsScrolledWindow(t *testing.T) {
	var l List
	var many []Item
	for i := 1; i <= 30; i++ {
		many = append(many, Item{Path: "big.go", Line: i, Text: "needle row"})
	}
	l.Append(many)
	l.Move(29)
	l.Render(40, 5, theme.DefaultPalette(), nil) // window scrolled to the tail
	// Visible row 4 is the last item (index 29).
	if got, ok := l.ItemAt(4); !ok || got != 29 {
		t.Fatalf("ItemAt(4) = %d,%v want 29,true", got, ok)
	}
}

func TestSetCursorClamps(t *testing.T) {
	var l List
	l.Append(items())
	l.SetCursor(1)
	if l.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", l.Cursor())
	}
	l.SetCursor(99)
	if l.Cursor() != 2 {
		t.Fatalf("cursor must clamp high, got %d", l.Cursor())
	}
	l.SetCursor(-5)
	if l.Cursor() != 0 {
		t.Fatalf("cursor must clamp low, got %d", l.Cursor())
	}
}

// TestRenderRowsNeverWrap guards #971: overlong rows must occupy exactly one
// line each — lipgloss MaxWidth WRAPS instead of clipping, so a width check
// per line misses the bug; the row COUNT is the real assertion.
func TestRenderRowsNeverWrap(t *testing.T) {
	long := "* [Syntax Highlighting](/architecture/highlighting.md) - Tree-sitter lexical layer: per-language grammars parsed off-loop into theme-coloured spans, applied per cell (Roadmap 0100)"
	idx := strings.Index(long, "architecture/high")
	l := &List{}
	l.Append([]Item{
		{Path: "wiki/architecture/index.md", Line: 26, Text: long, StartCol: idx, EndCol: idx + 25},
		{Path: "wiki/architecture/index.md", Line: 27, Text: "short", StartCol: 0, EndCol: 3},
	})
	for _, sel := range []int{0, 1} {
		l.cursor = sel
		out := l.Render(86, 10, theme.DefaultPalette(), nil)
		rows := strings.Split(out, "\n")
		if len(rows) != 3 { // header + two items
			t.Fatalf("sel=%d: rows = %d, want 3 (a wrapped row splits):\n%s", sel, len(rows), out)
		}
		for i, r := range rows {
			if w := lipgloss.Width(r); w > 86 {
				t.Errorf("sel=%d row %d width %d > 86", sel, i, w)
			}
		}
	}
}

// TestRenderFlattensEmbeddedNewlines guards #971: a match text carrying a
// newline (multi-line context) must not render a literal second row.
func TestRenderFlattensEmbeddedNewlines(t *testing.T) {
	l := &List{}
	l.Append([]Item{{Path: "wiki/log.md", Line: 2647,
		Text: "[LSP](/architecture/lsp.md) and [Syntax\nHighlighting](/architecture/highlighting.md).",
		StartCol: 5, EndCol: 25}})
	out := l.Render(86, 10, theme.DefaultPalette(), nil)
	if rows := strings.Split(out, "\n"); len(rows) != 2 { // header + one item
		t.Fatalf("rows = %d, want 2:\n%s", len(rows), out)
	}
}

// TestStepWrapsAtBothEnds guards #1666: single-step navigation wraps.
func TestStepWrapsAtBothEnds(t *testing.T) {
	var l List
	l.Append(items()) // 3 items
	l.Step(-1)
	if l.Cursor() != 2 {
		t.Fatalf("up on the first item = %d, want 2", l.Cursor())
	}
	l.Step(1)
	if l.Cursor() != 0 {
		t.Fatalf("down on the last item = %d, want 0", l.Cursor())
	}
	// A single-entry list stays put; an empty one never moves off 0.
	var one List
	one.Append(items()[:1])
	one.Step(1)
	one.Step(-1)
	if one.Cursor() != 0 {
		t.Fatalf("single-entry cursor = %d, want 0", one.Cursor())
	}
	var empty List
	empty.Step(1)
	if empty.Cursor() != 0 {
		t.Fatalf("empty cursor = %d, want 0", empty.Cursor())
	}
}

// TestPageJumpsOneRenderWindow guards #1666: pgup/pgdn move by the last
// rendered height (headers counted) and clamp instead of wrapping.
func TestPageJumpsOneRenderWindow(t *testing.T) {
	var l List
	var batch []Item
	for i := 0; i < 30; i++ {
		batch = append(batch, Item{Path: "a.go", Line: i + 1, Text: "x"})
	}
	l.Append(batch)
	l.Render(40, 10, theme.DefaultPalette(), nil) // window = 10 rows, 1 is the header
	l.Page(1)
	// Rows: header at 0, item i at row i+1. Cursor row 1 + 10 = 11 -> item 10.
	if l.Cursor() != 10 {
		t.Fatalf("pgdn cursor = %d, want 10", l.Cursor())
	}
	l.Page(-1)
	if l.Cursor() != 0 {
		t.Fatalf("pgup cursor = %d, want 0", l.Cursor())
	}
	// Page jumps clamp — they must not wrap round to the other end.
	l.Page(-1)
	if l.Cursor() != 0 {
		t.Fatalf("pgup on the first item must clamp, got %d", l.Cursor())
	}
	l.SetCursor(29)
	l.Page(1)
	if l.Cursor() != 29 {
		t.Fatalf("pgdn on the last item must clamp, got %d", l.Cursor())
	}
}

// TestHomeEndJumpToExtremes guards #1666.
func TestHomeEndJumpToExtremes(t *testing.T) {
	var l List
	l.Append(items())
	l.End()
	if l.Cursor() != 2 {
		t.Fatalf("End cursor = %d, want 2", l.Cursor())
	}
	l.Home()
	if l.Cursor() != 0 {
		t.Fatalf("Home cursor = %d, want 0", l.Cursor())
	}
	var empty List
	empty.End()
	if empty.Cursor() != 0 {
		t.Fatalf("End on an empty list = %d, want 0", empty.Cursor())
	}
}
