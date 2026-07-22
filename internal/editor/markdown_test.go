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

// TestConcealCursorLineAlwaysRaw: the cursor line shows raw source so editing
// stays exact.
func TestConcealCursorLineAlwaysRaw(t *testing.T) {
	m, path := mdLoaded(t, "**bold** x\nplain\n")
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: concealSpans(0)})
	m = mm
	// Cursor sits on line 0 (default).
	if view := plainView(m); !strings.Contains(view, "**") {
		t.Error("cursor line must show raw markers")
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
		if got := m.concealClickCol(0, 0, tc.offset); got != tc.want {
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

// TestTableRawWhenCursorInside: entering the block flips it to raw source.
func TestTableRawWhenCursorInside(t *testing.T) {
	m, _ := mdLoaded(t, tableDoc)
	// Cursor on the delimiter row: line 3 then renders plain (no cursor cell
	// styling breaking the substring).
	m.cursor = buffer.Position{Line: 2}
	view := plainView(m)
	if strings.Contains(view, "│") {
		t.Error("box drawing shown while cursor is inside the table")
	}
	if !strings.Contains(view, "| apple | 3 |") {
		t.Error("raw table source missing with cursor inside")
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
	})
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	b := blocks[0]
	if b.start != 1 || b.end != 3 {
		t.Errorf("block range %d-%d, want 1-3", b.start, b.end)
	}
	if !strings.Contains(b.rows[2], "x|y") {
		t.Errorf("escaped pipe mangled: %q", b.rows[2])
	}
	if !strings.HasPrefix(b.rows[1], "├") || !strings.HasSuffix(b.rows[1], "┤") {
		t.Errorf("delimiter row not a separator: %q", b.rows[1])
	}
	// Row-preserving: one display row per source line.
	if len(b.rows) != 3 {
		t.Errorf("rows = %d, want 3 (line↔row mapping must stay 1:1)", len(b.rows))
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
