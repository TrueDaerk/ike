package editor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/lang"
)

// mdLoaded loads content under a .md path (the table layer checks the
// language) with a registered bare markdown language, sized and focused.
func mdLoaded(t *testing.T, content string) (Model, string) {
	t.Helper()
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md"}})
	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(60, 10)
	m.SetFocused(true)
	return m, path
}

// ansiRE strips styling so assertions can match text across styled cells.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plainView(m Model) string { return ansiRE.ReplaceAllString(m.View(), "") }

// concealSpans builds the parse result for "**bold** x" on the given line:
// style spans plus the two @conceal delimiter ranges.
func concealSpans(line int) []highlight.Span {
	return []highlight.Span{
		{Line: line, StartCol: 0, EndCol: 2, Capture: "conceal"},
		{Line: line, StartCol: 0, EndCol: 8, Capture: "markup.bold"},
		{Line: line, StartCol: 6, EndCol: 8, Capture: "conceal"},
	}
}

// TestConcealHidesMarkersOffCursorLine guards the core #881 behavior: marker
// cells vanish on a line the cursor is not on, and the SpansMsg split keeps
// them out of the style index.
func TestConcealHidesMarkersOffCursorLine(t *testing.T) {
	m, path := mdLoaded(t, "**bold** x\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: concealSpans(0)})
	m = mm

	view := plainView(m)
	if strings.Contains(view, "**") {
		t.Error("markers still visible on a non-cursor line")
	}
	if !strings.Contains(view, "bold x") {
		t.Error("concealed line lost its text")
	}
	// The conceal spans must not act as style spans.
	if got := m.hlIndex.CaptureAt(0, 0); got == "conceal" {
		t.Error("conceal span leaked into the style index")
	}
}

// TestConcealCaretPositionRaw (#1594): only the marker the caret sits in
// shows raw; the line's other markers stay concealed, and a caret elsewhere
// on the line conceals everything.
func TestConcealCaretPositionRaw(t *testing.T) {
	m, path := mdLoaded(t, "**bold** x\nplain\n")
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: concealSpans(0)})
	m = mm
	// Cursor at col 0 — inside the leading marker [0,2): it shows raw, the
	// trailing marker [6,8) stays hidden.
	view := plainView(m)
	if !strings.Contains(view, "**bold") {
		t.Errorf("caret marker must show raw, view:\n%s", view)
	}
	if strings.Contains(view, "bold**") {
		t.Errorf("other marker must stay concealed, view:\n%s", view)
	}
	// Cursor mid-word — outside both ranges: everything conceals.
	m.cursor = buffer.Position{Line: 0, Col: 4}
	if view := plainView(m); strings.Contains(view, "**") {
		t.Errorf("caret outside the ranges must conceal all markers, view:\n%s", view)
	}
}

// TestConcealToggleOff: editor.markdown_rendering=false shows raw everywhere.
func TestConcealToggleOff(t *testing.T) {
	m, path := mdLoaded(t, "**bold** x\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	m.mdRender = false
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: concealSpans(0)})
	m = mm
	if view := plainView(m); !strings.Contains(view, "**") {
		t.Error("toggle off must render raw markers")
	}
}

// TestConcealClickMapping: clicks on a concealed line map display cells back
// to buffer columns through the hidden ranges (#881).
func TestConcealClickMapping(t *testing.T) {
	m, path := mdLoaded(t, "**bold** x\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: concealSpans(0)})
	m = mm

	// Display shows "bold x": offset 0 → buffer col 2 (the b), offset 3 →
	// col 5 (the d), offset 4 → col 8 (the space after **), offset 5 → col 9.
	for _, tc := range []struct{ offset, want int }{
		{0, 2}, {3, 5}, {4, 8}, {5, 9},
	} {
		if got := m.displayClickCol(0, 0, tc.offset); got != tc.want {
			t.Errorf("offset %d → col %d, want %d", tc.offset, got, tc.want)
		}
	}

	// End-to-end through MouseClick: gutter width + offset 0 lands on col 2.
	gw := m.view.GutterWidth(m.buf.LineCount())
	m.MouseClick(gw+0, 0)
	if m.cursor.Line != 0 || m.cursor.Col != 2 {
		t.Errorf("click mapped to %v, want line 0 col 2", m.cursor)
	}
}

const tableDoc = `before
| Name | Qty |
| :--- | ---: |
| apple | 3 |
| pear | 12 |
after
`

// TestTableRendersBoxDrawing: cursor outside the block → box-drawing render,
// delimiter row becomes the ├─┼─┤ separator, cells align per the delimiter.
func TestTableRendersBoxDrawing(t *testing.T) {
	m, _ := mdLoaded(t, tableDoc)
	m.cursor = buffer.Position{Line: 0}
	view := plainView(m)
	if !strings.Contains(view, "│") || !strings.Contains(view, "├") || !strings.Contains(view, "┼") {
		t.Fatalf("no box drawing in view:\n%s", view)
	}
	if strings.Contains(view, "| Name") {
		t.Error("raw pipe row still visible while cursor is outside the table")
	}
	// Right alignment from the delimiter row: Qty column (width 3) pads the 3
	// left — "   3 " — where left alignment would give " 3   ". The border
	// glyphs are styled, so the assertion stays inside one cell.
	if !strings.Contains(view, "   3 ") {
		t.Errorf("expected right-aligned qty cell, view:\n%s", view)
	}
}

// TestTableCursorRowRawOnChrome (#1599): the cursor on the delimiter row (or
// any table chrome) reveals only that row as raw source; the rest of the
// block stays box-drawn.
func TestTableCursorRowRawOnChrome(t *testing.T) {
	m, _ := mdLoaded(t, tableDoc)
	m.cursor = buffer.Position{Line: 2}
	view := plainView(m)
	if !strings.Contains(view, ":---") {
		t.Errorf("cursor row must show raw source, view:\n%s", view)
	}
	if !strings.Contains(view, "│") {
		t.Errorf("other table rows must stay box-drawn, view:\n%s", view)
	}
	if strings.Contains(view, "| apple") {
		t.Errorf("non-cursor rows must not render raw, view:\n%s", view)
	}
}

// TestTableCursorCellRaw (#1599): the cursor inside a cell reveals only that
// cell raw — the frame stays box-drawn, the other cells stay rendered, and
// the cursor on a pipe reveals the whole row instead.
func TestTableCursorCellRaw(t *testing.T) {
	m, _ := mdLoaded(t, tableDoc)
	// Line 3 is "| apple | 3 |"; col 2 sits inside the first cell.
	m.cursor = buffer.Position{Line: 3, Col: 2}
	view := plainView(m)
	if !strings.Contains(view, "│") {
		t.Errorf("frame must stay box-drawn with the cursor mid-cell, view:\n%s", view)
	}
	if !strings.Contains(view, "apple") {
		t.Errorf("cursor cell must show its raw source, view:\n%s", view)
	}
	if strings.Contains(view, "| apple") {
		t.Errorf("the row's pipes must stay rendered mid-cell, view:\n%s", view)
	}
	// Col 0 is the leading pipe — table chrome: the whole row reveals raw.
	m.cursor = buffer.Position{Line: 3, Col: 0}
	if view := plainView(m); !strings.Contains(view, "| apple | 3 |") {
		t.Errorf("cursor on a pipe must reveal the raw row, view:\n%s", view)
	}
}

// TestTableFullyRawWithSelection (#1599): a selection touching the block
// still flips it fully raw — selection styling only renders on raw lines.
func TestTableFullyRawWithSelection(t *testing.T) {
	m, _ := mdLoaded(t, tableDoc)
	m.cursor = buffer.Position{Line: 1, Col: 2}
	m.enterVisual(Visual)
	m.cursor = buffer.Position{Line: 3, Col: 2}
	view := plainView(m)
	if strings.Contains(view, "│") {
		t.Errorf("box drawing shown while a selection crosses the table, view:\n%s", view)
	}
	if !strings.Contains(view, "| apple | 3 |") {
		t.Errorf("raw table source missing under selection, view:\n%s", view)
	}
}

// TestTableRawUnderSoftWrap: wrap segments slice raw buffer text, so table
// rendering stays off under soft wrap (documented decision).
func TestTableRawUnderSoftWrap(t *testing.T) {
	m, _ := mdLoaded(t, tableDoc)
	m.cursor = buffer.Position{Line: 0}
	m.softWrap, m.wrapSet = true, true
	if view := plainView(m); strings.Contains(view, "│") {
		t.Error("box drawing must be off under soft wrap")
	}
}

// TestDetectTables covers the pure detection: delimiter row required, blocks
// end at the first non-pipe line, escaped pipes stay cell content.
func TestDetectTables(t *testing.T) {
	blocks := detectTables([]string{
		"text",
		"| a | b |",
		"| --- | --- |",
		"| 1 | x\\|y |",
		"done",
		"| not | a table |", // no delimiter row below
	}, mdCellStyles{})
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	b := blocks[0]
	if b.start != 1 || b.end != 3 {
		t.Errorf("block range %d-%d, want 1-3", b.start, b.end)
	}
	// Rows carry their styling (faint borders) since #945: strip for text
	// assertions.
	if row := ansiRE.ReplaceAllString(b.rows[2], ""); !strings.Contains(row, "x|y") {
		t.Errorf("escaped pipe mangled: %q", row)
	}
	if row := ansiRE.ReplaceAllString(b.rows[1], ""); !strings.HasPrefix(row, "├") || !strings.HasSuffix(row, "┤") {
		t.Errorf("delimiter row not a separator: %q", row)
	}
	// Row-preserving: one display row per source line.
	if len(b.rows) != 3 {
		t.Errorf("rows = %d, want 3 (line↔row mapping must stay 1:1)", len(b.rows))
	}
}

// TestRenderCellInline covers the cell renderer (#945): marker chrome drops,
// attributes apply, unmatched markers and escapes stay literal.
func TestRenderCellInline(t *testing.T) {
	st := mdCellStyles{}
	for _, tc := range []struct {
		in, plain, attr string
	}{
		{"`hello`", "hello", ""},
		{"**bold**", "bold", "\x1b[1m"},
		{"__bold__", "bold", "\x1b[1m"},
		{"*Firm*", "Firm", "\x1b[3m"},
		{"_ital_", "ital", "\x1b[3m"},
		{"~~gone~~", "gone", "\x1b[9m"},
		{"[site](https://x.y)", "site", ""},
		{"![alt](img.png)", "alt", ""},
		{"**bold *nested***", "bold nested", ";3m"}, // italic inside bold (combined SGR 1;3)
		{"a * b", "a * b", ""},                          // unmatched marker literal
		{"snake_case_name", "snake_case_name", ""},      // no intra-word underscore emphasis
		{"\\*lit\\*", "*lit*", ""},                      // escapes
		{"****", "****", ""},                            // empty emphasis stays literal
	} {
		got := renderCellInline(tc.in, st)
		if plain := ansiRE.ReplaceAllString(got, ""); plain != tc.plain {
			t.Errorf("%q → plain %q, want %q", tc.in, plain, tc.plain)
		}
		if tc.attr != "" && !strings.Contains(got, tc.attr) {
			t.Errorf("%q → %q, missing attribute %q", tc.in, got, tc.attr)
		}
	}
}

// TestTableCellInlineStyling is the end-to-end #945 case: rendered cells show
// content without marker chrome, styled, and columns size by display width.
func TestTableCellInlineStyling(t *testing.T) {
	m, _ := mdLoaded(t, "x\n| A | B |\n|---|---|\n| `hello` | True |\n| *Firm* | Abc |\n")
	m.cursor = buffer.Position{Line: 0}
	view := plainView(m)
	if strings.Contains(view, "`") || strings.Contains(view, "*") {
		t.Errorf("marker chrome visible in rendered cells:\n%s", view)
	}
	if !strings.Contains(view, "hello") || !strings.Contains(view, "Firm") {
		t.Errorf("cell content missing:\n%s", view)
	}
	// Column A sizes by display width: "hello" (5) not "`hello`" (7) — the
	// Firm row pads to 5, so its cell reads "│ Firm  │" once markers drop.
	if !strings.Contains(view, "│ Firm  │") {
		t.Errorf("column width not sized by concealed display width:\n%s", view)
	}
	// The italic attribute survives into the row render.
	if !strings.Contains(m.View(), "\x1b[3m") {
		t.Error("italic cell lost its text attribute")
	}
}

// extentSpans is concealSpans plus the enclosing-span extent (#1599), the
// shape the grammar's @conceal.extent capture delivers for "**bold** x".
func extentSpans(line int) []highlight.Span {
	return append(concealSpans(line),
		highlight.Span{Line: line, StartCol: 0, EndCol: 8, Capture: "conceal.extent"})
}

// TestConcealExtentReveal (#1599): the caret anywhere inside an inline span —
// not only on a marker — reveals the span's markers; outside it everything
// conceals, and the extent span never leaks into the style index.
func TestConcealExtentReveal(t *testing.T) {
	m, path := mdLoaded(t, "**bold** x\nplain\n")
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: extentSpans(0)})
	m = mm
	// Cursor mid-word (col 4) — inside the extent: both markers show raw.
	m.cursor = buffer.Position{Line: 0, Col: 4}
	if view := plainView(m); !strings.Contains(view, "**bold** x") {
		t.Errorf("caret inside the span must reveal its markers, view:\n%s", view)
	}
	// Cursor past the span (col 8, the space) — everything conceals.
	m.cursor = buffer.Position{Line: 0, Col: 8}
	if view := plainView(m); strings.Contains(view, "**") {
		t.Errorf("caret outside the span must conceal the markers, view:\n%s", view)
	}
	if got := m.hlIndex.CaptureAt(0, 4); got == "conceal.extent" {
		t.Error("extent span leaked into the style index")
	}
}

// TestToggleMarkdownRendering (#1599): the view toggle flips rendering off
// (raw markers, no markup attributes, raw tables) and back on, and sticks
// across the per-Update config refresh.
func TestToggleMarkdownRendering(t *testing.T) {
	m, path := mdLoaded(t, "**bold** x\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: concealSpans(0)})
	m = mm
	mm, _ = m.Update(ActionMsg{Action: "toggle_markdown_rendering"})
	m = mm
	if view := plainView(m); !strings.Contains(view, "**bold**") {
		t.Errorf("toggle off must show raw markers, view:\n%s", view)
	}
	if st, _ := m.styleAt(0, 3); st.GetBold() {
		t.Error("toggle off must drop the markup.bold attribute")
	}
	if !m.mdRenderSet {
		t.Error("toggle must set the sticky per-view override")
	}
	mm, _ = m.Update(ActionMsg{Action: "toggle_markdown_rendering"})
	m = mm
	if view := plainView(m); strings.Contains(view, "**") {
		t.Errorf("toggle back on must conceal again, view:\n%s", view)
	}
}

// TestTableClickMapping (#1599): clicks on a box-drawn table row land in the
// pointed-at cell, border clicks on the pipe they draw.
func TestTableClickMapping(t *testing.T) {
	m, _ := mdLoaded(t, tableDoc)
	m.cursor = buffer.Position{Line: 0}
	// Line 3 is "| apple | 3 |", rendered "│ apple │     3 │" (Name width 5,
	// Qty width 3... widths come from the header row). Display col 0 is the
	// left border → the leading pipe (col 0); col 2 is the 'a' → col 2.
	if col, ok := m.tableClickCol(3, 0); !ok || col != 0 {
		t.Errorf("border click → col %d ok=%v, want 0", col, ok)
	}
	if col, ok := m.tableClickCol(3, 2); !ok || col != 2 {
		t.Errorf("cell click → col %d ok=%v, want 2", col, ok)
	}
	// tableDisplayCol is the inverse for the same cell.
	if d, ok := m.tableDisplayCol(3, 2); !ok || d != 2 {
		t.Errorf("display col %d ok=%v, want 2", d, ok)
	}
	// A raw line (outside any block) maps through neither.
	if _, ok := m.tableClickCol(0, 0); ok {
		t.Error("non-table line must not table-map")
	}
}

// TestMarkupAttributes: markup.* captures resolve to text attributes.
func TestMarkupAttributes(t *testing.T) {
	m, _ := mdLoaded(t, "text\n")
	m.hlIndex = highlight.NewIndex([]highlight.Span{
		{Line: 0, StartCol: 0, EndCol: 2, Capture: "markup.bold"},
		{Line: 0, StartCol: 2, EndCol: 4, Capture: "markup.italic"},
	})
	if st, ok := m.styleAt(0, 0); !ok || !st.GetBold() {
		t.Error("markup.bold must render bold")
	}
	if st, ok := m.styleAt(0, 2); !ok || !st.GetItalic() {
		t.Error("markup.italic must render italic")
	}
}
