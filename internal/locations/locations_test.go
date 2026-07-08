package locations

import (
	"strings"
	"testing"

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
