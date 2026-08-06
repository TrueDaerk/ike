package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/lang"
)

// csvLoaded loads content under a .csv path with a registered bare csv
// language, sized and focused.
func csvLoaded(t *testing.T, content string) Model {
	t.Helper()
	lang.Register(lang.Language{ID: "csv", Extensions: []string{"csv"}})
	path := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(60, 10)
	m.SetFocused(true)
	return m
}

const csvDoc = "name,qty\napple,3\npear,12\n"

// TestSVTableAlignsColumns guards the core #1589 behavior: off-caret rows
// render aligned with the separator concealed; the caret line stays raw.
func TestSVTableAlignsColumns(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 2}

	view := plainView(m)
	// widths: col0 = max(name, apple, pear) = 5, gap 2.
	if !strings.Contains(view, "name   qty") {
		t.Errorf("header not aligned:\n%s", view)
	}
	if !strings.Contains(view, "apple  3") {
		t.Errorf("row not aligned:\n%s", view)
	}
	if !strings.Contains(view, "pear,12") {
		t.Errorf("caret line must stay raw:\n%s", view)
	}
	if strings.Contains(view, "apple,3") {
		t.Error("off-caret separator still visible")
	}
}

// TestSVSelectionRevealsRaw: a selection touching a row shows raw source.
func TestSVSelectionRevealsRaw(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 2}
	if _, ok := m.svRow(1); !ok {
		t.Fatal("row 1 should render aligned before selecting")
	}
	m.MouseClick(m.view.GutterWidth(m.buf.LineCount()), 1) // anchor on line 1
	m.MouseDrag(m.view.GutterWidth(m.buf.LineCount())+3, 1)
	if _, ok := m.svRow(1); ok {
		t.Fatal("selected row must render raw")
	}
}

// TestSVToggleOff: editor.csv_rendering=false renders raw everywhere.
func TestSVToggleOff(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 2}
	m.svRender = false
	if view := plainView(m); !strings.Contains(view, "apple,3") {
		t.Error("toggle off must render raw separators")
	}
}

// TestSVQuotedFieldNotSplit: a quoted separator stays inside its field.
func TestSVQuotedFieldNotSplit(t *testing.T) {
	m := csvLoaded(t, "\"a,b\",c\nx,y\n")
	m.cursor = buffer.Position{Line: 1}
	if view := plainView(m); !strings.Contains(view, `"a,b"`) {
		t.Errorf("quoted field torn apart:\n%s", view)
	}
}

// TestSVStickyHeaderPinned: scrolling pins the first line at the top.
func TestSVStickyHeaderPinned(t *testing.T) {
	var content strings.Builder
	content.WriteString("name,qty\n")
	for i := 0; i < 30; i++ {
		content.WriteString("apple,3\n")
	}
	m := csvLoaded(t, content.String())
	m.SetSize(30, 6)
	m.cursor = buffer.Position{Line: 20}
	m.SetScroll(15, 0)

	rows := strings.Split(plainView(m), "\n")
	if len(rows) == 0 || !strings.Contains(rows[0], "name") {
		t.Fatalf("header not pinned, first row: %q", rows[0])
	}
}

// TestSVClickMapping: clicks on an aligned row map display cells back to
// buffer columns — padding lands on the concealed separator.
func TestSVClickMapping(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 2}
	// Line 1 "apple,3": field0 [0,5), separator col 5, field1 [6,7);
	// display "apple  3": cells 0-4 field0, 5-6 padding, 7 field1.
	for _, tc := range []struct{ offset, want int }{
		{0, 0}, {4, 4}, {5, 5}, {6, 5}, {7, 6},
	} {
		if got := m.displayClickCol(1, 0, tc.offset); got != tc.want {
			t.Errorf("offset %d → col %d, want %d", tc.offset, got, tc.want)
		}
	}
}

// TestSVDisplayOffset: overlay anchors count through the padded layout.
func TestSVDisplayOffset(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 2}
	if got := m.DisplayOffset(1, 6); got != 7 {
		t.Errorf("DisplayOffset(1, 6) = %d, want 7", got)
	}
	if got := m.DisplayOffset(1, 0); got != 0 {
		t.Errorf("DisplayOffset(1, 0) = %d, want 0", got)
	}
}

// TestSVScrollRecomputesWidths: the layout follows the visible rows — a wider
// row scrolled into view widens the column, and the line cache refreshes.
func TestSVScrollRecomputesWidths(t *testing.T) {
	var content strings.Builder
	content.WriteString("h,q\n")
	for i := 0; i < 20; i++ {
		content.WriteString("a,1\n")
	}
	content.WriteString("longvalue,2\n")
	m := csvLoaded(t, content.String())
	m.SetSize(30, 5)
	m.cursor = buffer.Position{Line: 0}

	m.SetScroll(1, 0)
	if view := plainView(m); strings.Contains(view, "a          1") {
		t.Fatalf("narrow viewport must not use the wide row's width:\n%s", view)
	}
	m.SetScroll(18, 0)
	// "longvalue" (9 cells) is now visible: rows pad to 9 + 2 gap.
	if view := plainView(m); !strings.Contains(view, "a          1") {
		t.Fatalf("wide row in view must widen the column:\n%s", view)
	}
}
