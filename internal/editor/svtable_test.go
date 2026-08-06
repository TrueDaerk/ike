package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/mode"
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

// TestSVTableAlignsColumns guards the core #1589 behavior under the #1594
// reveal rules: every row renders aligned with the separator concealed —
// including the caret line, as long as the caret is not on a separator.
func TestSVTableAlignsColumns(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 2} // col 0: on a field, not a separator

	view := plainView(m)
	// widths: col0 = max(name, apple, pear) = 5, gap 2.
	for _, want := range []string{"name   qty", "apple  3", "pear   12"} {
		if !strings.Contains(view, want) {
			t.Errorf("aligned row %q missing:\n%s", want, view)
		}
	}
	if strings.Contains(view, ",") {
		t.Errorf("separator visible although no caret is on one:\n%s", view)
	}
}

// TestSVCaretOnSeparatorRevealsIt: only the separator under the caret shows
// raw; the rest of the line keeps its concealed alignment (#1594).
func TestSVCaretOnSeparatorRevealsIt(t *testing.T) {
	m := csvLoaded(t, "a,bb,c\nx,yy,z\n")
	m.cursor = buffer.Position{Line: 0, Col: 1} // the first separator

	view := plainView(m)
	if !strings.Contains(view, "a,bb") {
		t.Errorf("caret separator not revealed:\n%s", view)
	}
	if strings.Contains(view, "bb,c") {
		t.Errorf("second separator must stay concealed:\n%s", view)
	}
	if !strings.Contains(view, "x  yy") {
		t.Errorf("other lines must stay aligned:\n%s", view)
	}
}

// TestSVSelectionRevealsCrossedSeparator: a selection crossing a separator
// reveals it; separators outside the selection stay concealed.
func TestSVSelectionRevealsCrossedSeparator(t *testing.T) {
	m := csvLoaded(t, "a,bb,c\nx,yy,z\n")
	m.cursor = buffer.Position{Line: 0, Col: 2}
	m.mode = mode.Visual
	m.anchor = buffer.Position{Line: 0, Col: 0} // selection [0,2] crosses col 1

	view := plainView(m)
	if !strings.Contains(view, "a,bb") {
		t.Errorf("selected separator not revealed:\n%s", view)
	}
	if strings.Contains(view, "bb,c") {
		t.Errorf("unselected separator must stay concealed:\n%s", view)
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
