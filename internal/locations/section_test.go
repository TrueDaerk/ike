package locations

import (
	"strings"
	"testing"

	"ike/internal/theme"
)

// sectioned builds the shape Find in All Projects feeds in (#2413): two
// projects, two files each, two matches per file.
func sectioned() *List {
	l := &List{}
	for _, sec := range []string{"/alpha", "/beta"} {
		for _, file := range []string{"a.go", "b.go"} {
			for _, line := range []int{1, 2} {
				l.Append([]Item{{
					Section: sec, Path: sec + "/" + file, Line: line,
					Text: "needle", StartCol: 0, EndCol: 6,
				}})
			}
		}
	}
	return l
}

// TestSectionsRenderAboveFileHeaders: a section header opens each project's
// run of files, and the file headers keep their own rows below it.
func TestSectionsRenderAboveFileHeaders(t *testing.T) {
	l := sectioned()
	l.SectionLabel = func(section string, items int) string { return "PROJECT " + section }
	out := l.Render(60, 40, theme.DefaultPalette(), nil)
	rows := strings.Split(out, "\n")
	// 2 sections + 4 files + 8 items.
	if len(rows) != 14 {
		t.Fatalf("got %d rows, want 14:\n%s", len(rows), out)
	}
	if !strings.Contains(rows[0], "PROJECT /alpha") {
		t.Fatalf("row 0 = %q, want the alpha section header", rows[0])
	}
	if !strings.Contains(rows[1], "a.go") {
		t.Fatalf("row 1 = %q, want the first file header", rows[1])
	}
	if !strings.Contains(rows[7], "PROJECT /beta") {
		t.Fatalf("row 7 = %q, want the beta section header", rows[7])
	}
}

// TestSectionLabelReportsItemCount: the label hook is handed the number of
// items under the whole section, not just its first file.
func TestSectionLabelReportsItemCount(t *testing.T) {
	l := sectioned()
	counts := map[string]int{}
	l.SectionLabel = func(section string, items int) string {
		counts[section] = items
		return section
	}
	l.Render(60, 40, theme.DefaultPalette(), nil)
	if counts["/alpha"] != 4 || counts["/beta"] != 4 {
		t.Fatalf("counts = %v, want 4 per project", counts)
	}
}

// TestSectionRowsShiftItemHitTesting: ItemAt must skip the section rows, or a
// click would open the match one row off.
func TestSectionRowsShiftItemHitTesting(t *testing.T) {
	l := sectioned()
	l.Render(60, 40, theme.DefaultPalette(), nil)
	if _, ok := l.ItemAt(0); ok {
		t.Fatal("row 0 is the section header, not an item")
	}
	if _, ok := l.ItemAt(1); ok {
		t.Fatal("row 1 is the file header, not an item")
	}
	idx, ok := l.ItemAt(2)
	if !ok || idx != 0 {
		t.Fatalf("ItemAt(2) = %d,%v, want the first item", idx, ok)
	}
	// Row 7 opens the second section; row 9 is its first item (index 4).
	if _, ok := l.ItemAt(7); ok {
		t.Fatal("row 7 is the second section header, not an item")
	}
	idx, ok = l.ItemAt(9)
	if !ok || idx != 4 {
		t.Fatalf("ItemAt(9) = %d,%v, want the beta project's first item", idx, ok)
	}
}

// TestSectionlessListIsUnchanged: find-in-path items carry no section, so the
// flat file layout — and its row math — stays exactly as it was.
func TestSectionlessListIsUnchanged(t *testing.T) {
	l := &List{}
	l.Append([]Item{
		{Path: "a.go", Line: 1, Text: "x"},
		{Path: "a.go", Line: 2, Text: "x"},
		{Path: "b.go", Line: 3, Text: "x"},
	})
	out := l.Render(60, 40, theme.DefaultPalette(), nil)
	if rows := strings.Split(out, "\n"); len(rows) != 5 { // 2 headers + 3 items
		t.Fatalf("got %d rows, want 5:\n%s", len(rows), out)
	}
	if _, ok := l.ItemAt(1); !ok {
		t.Fatal("row 1 must still be the first item without sections")
	}
}

// TestSameFileInTwoSectionsStaysSeparate: two projects can hold the same
// relative path — the groups must not merge across a section boundary.
func TestSameFileInTwoSectionsStaysSeparate(t *testing.T) {
	l := &List{}
	l.Append([]Item{
		{Section: "/alpha", Path: "/x/main.go", Line: 1, Text: "x"},
		{Section: "/beta", Path: "/x/main.go", Line: 1, Text: "x"},
	})
	if l.Files() != 2 {
		t.Fatalf("Files() = %d, want the two sections' files kept apart", l.Files())
	}
}

// TestSectionsCountTowardsPaging: the page jump measures render rows, so the
// section headers have to be part of the count.
func TestSectionsCountTowardsPaging(t *testing.T) {
	l := sectioned()
	l.Render(60, 6, theme.DefaultPalette(), nil)
	l.Page(1)
	// One page is six render rows, two of which are headers here, so the jump
	// lands on the item nearest render row 8 — the second project's first.
	if l.Cursor() != 4 {
		t.Fatalf("Cursor() = %d, want the item a screenful of rows down", l.Cursor())
	}
	l.Page(-1)
	if l.Cursor() >= 4 {
		t.Fatalf("a page up must move back, got %d", l.Cursor())
	}
}
