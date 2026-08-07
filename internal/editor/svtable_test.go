package editor

import (
	"fmt"
	"image/color"
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

// bgSGR is the SGR fragment a lipgloss background of c renders as.
func bgSGR(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// tintedText returns the visible characters of a rendered row whose style
// carries sgr — the cells the tint actually painted.
func tintedText(row, sgr string) string {
	var b strings.Builder
	for {
		loc := ansiRE.FindStringIndex(row)
		if loc == nil {
			return b.String()
		}
		seq, rest := row[loc[0]:loc[1]], row[loc[1]:]
		end := len(rest)
		if next := ansiRE.FindStringIndex(rest); next != nil {
			end = next[0]
		}
		if strings.Contains(seq, sgr) {
			b.WriteString(rest[:end])
		}
		row = rest[end:]
	}
}

// TestSVColumnStripeSpansVisibleRows: the caret's column is tinted on every
// visible row, not just the caret's own (#1659) — including the alignment
// padding, so the stripe is one uninterrupted block.
func TestSVColumnStripeSpansVisibleRows(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 1, Col: 0} // "apple" — column 1
	tint := bgSGR(m.svColumnTint())

	rows := strings.Split(m.View(), "\n")
	// Column 1 is 5 wide plus the 2-cell gap; the caret cell itself renders in
	// the cursor colour, so row 1 shows the remaining cells only.
	for i, want := range []string{"name   ", "pple  ", "pear   "} {
		if got := tintedText(rows[i], tint); got != want {
			t.Errorf("row %d tinted %q, want %q", i, got, want)
		}
	}
}

// TestSVColumnStripeFollowsCursor: moving into another field moves the stripe,
// and the last column's stripe stops at the content (no trailing separator).
func TestSVColumnStripeFollowsCursor(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 2, Col: 6} // "pear,12" → column 2
	tint := bgSGR(m.svColumnTint())

	rows := strings.Split(m.View(), "\n")
	if got := tintedText(rows[0], tint); got != "qty" {
		t.Errorf("header row tinted %q, want %q", got, "qty")
	}
	if got := tintedText(rows[1], tint); got != "3" {
		t.Errorf("data row tinted %q, want %q", got, "3")
	}
	if got := tintedText(rows[2], tint); got != "1" {
		// Column 2 of "pear,12": the caret sits on "2", so only "1" is left.
		t.Errorf("caret row tinted %q, want %q", got, "2")
	}
}

// TestSVColumnStripeQuotedField: the stripe uses the shared parser, so an
// embedded separator does not shift the column (#1659).
func TestSVColumnStripeQuotedField(t *testing.T) {
	m := csvLoaded(t, "h1,h2\n\"a,b\",z\n")
	m.cursor = buffer.Position{Line: 1, Col: 6} // "z" — column 2
	tint := bgSGR(m.svColumnTint())

	rows := strings.Split(m.View(), "\n")
	if got := tintedText(rows[0], tint); got != "h2" {
		t.Errorf("header row tinted %q, want %q", got, "h2")
	}
	if got := m.SVColumnLabel(); got != "column 2: h2" {
		t.Errorf("label = %q", got)
	}
}

// TestSVColumnStripeRawMode: raw rendering (toggle off) draws no stripe, and
// the status label disappears with it.
func TestSVColumnStripeRawMode(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 1, Col: 0}
	m.svRender = false
	if got := tintedText(m.View(), bgSGR(m.svColumnTint())); got != "" {
		t.Errorf("raw mode tinted %q", got)
	}
	if got := m.SVColumnLabel(); got != "" {
		t.Errorf("raw mode label = %q, want empty", got)
	}
}

// TestSVColumnStripeUnfocused: an unfocused pane shows no caret, so it shows
// no column stripe either.
func TestSVColumnStripeUnfocused(t *testing.T) {
	m := csvLoaded(t, csvDoc)
	m.cursor = buffer.Position{Line: 1, Col: 0}
	m.SetFocused(false)
	if got := tintedText(m.View(), bgSGR(m.svColumnTint())); got != "" {
		t.Errorf("unfocused pane tinted %q", got)
	}
}

// TestSVColumnLabelHeaderFallback: without a header-ish first row the label is
// the bare index; a caret past the header's field count too.
func TestSVColumnLabelHeaderFallback(t *testing.T) {
	m := csvLoaded(t, "1,2\n3,4\n")
	m.cursor = buffer.Position{Line: 1, Col: 2}
	if got := m.SVColumnLabel(); got != "column 2" {
		t.Errorf("numeric first row label = %q, want %q", got, "column 2")
	}

	m = csvLoaded(t, "name,qty\napple,3,extra\n")
	m.cursor = buffer.Position{Line: 1, Col: 8} // the third field
	if got := m.SVColumnLabel(); got != "column 3" {
		t.Errorf("beyond-header label = %q, want %q", got, "column 3")
	}
	m.cursor = buffer.Position{Line: 1, Col: 0}
	if got := m.SVColumnLabel(); got != "column 1: name" {
		t.Errorf("header label = %q", got)
	}
}

// TestSVColumnStripeShortRow: a row without the caret's column is simply left
// untinted — no per-line padding work on rows that do not have the field.
func TestSVColumnStripeShortRow(t *testing.T) {
	m := csvLoaded(t, "a,b,c\nx\ny,z,w\n")
	m.cursor = buffer.Position{Line: 2, Col: 4} // column 3
	rows := strings.Split(m.View(), "\n")
	if got := tintedText(rows[1], bgSGR(m.svColumnTint())); got != "" {
		t.Errorf("short row tinted %q, want empty", got)
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
