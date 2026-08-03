package editor

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
)

// --- render (#1469) --------------------------------------------------------

// TestCtrlBytesRenderAsGlyphs guards #1469: a buffer line carrying a raw ANSI
// escape renders the escape as visible one-cell placeholders instead of
// leaking the bytes to the terminal, so display width equals rune count.
func TestCtrlBytesRenderAsGlyphs(t *testing.T) {
	m, _ := loaded(t, "id \x1b[1;33mSTATUS ok\nplain\n")
	row := strings.SplitN(m.View(), "\n", 2)[0]
	got := strings.TrimRight(ansi.Strip(row), " ")
	// The ESC rune shows as ␛; the rest of the sequence is ordinary text.
	if !strings.Contains(got, "␛[1;33mSTATUS") {
		t.Fatalf("rendered = %q, want the escape visible as ␛[1;33m", got)
	}
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("raw ESC leaked into the stripped render: %q", got)
	}
	// One rune, one cell: the row's plain width matches the buffer's rune
	// count, which is what keeps click mapping and the caret aligned.
	runes := []rune(m.buf.Line(0))
	if w := ansi.StringWidth(got); w != len(runes) {
		t.Fatalf("display width = %d, want %d (rune count)", w, len(runes))
	}
}

// TestOtherC0ControlsRenderAsGlyphs: NUL, BEL and BS get the same placeholder
// treatment; tab keeps its expansion.
func TestOtherC0ControlsRenderAsGlyphs(t *testing.T) {
	m, _ := loaded(t, "a\x00b\x07c\x08d\te\n")
	row := strings.TrimRight(ansi.Strip(strings.SplitN(m.View(), "\n", 2)[0]), " ")
	if want := "a␀b␇c␈d    e"; row != want {
		t.Fatalf("rendered = %q, want %q", row, want)
	}
}

// TestClickAfterEscapeSequence: clicking right of the escape lands the caret
// on the clicked character — the escape occupies exactly its rune count in
// display cells, so screen and buffer columns agree.
func TestClickAfterEscapeSequence(t *testing.T) {
	m, _ := loaded(t, "id \x1b[1;33mSTATUS ok\n")
	line := []rune(m.buf.Line(0))
	// The first 'S' of STATUS sits right after "id " plus the 7-rune escape
	// sequence and, with one cell per rune, at the same screen column.
	col := strings.IndexRune(string(line), 'S')
	gutter := m.view.GutterWidth(m.buf.LineCount())
	m.MouseClick(gutter+col, 0)
	if m.cursor != (buffer.Position{Line: 0, Col: col}) {
		t.Fatalf("cursor = %v, want {0 %d}", m.cursor, col)
	}
}

// TestCaretOnEscapedLineKeepsContent: the caret sitting inside the coloured
// word must not disturb the rendered characters — the file's SGR state never
// reaches the terminal, so nothing can bleed around the caret cell.
func TestCaretOnEscapedLineKeepsContent(t *testing.T) {
	m, _ := loaded(t, "id \x1b[1;33mSTATUS ok\n")
	m.cursor = buffer.Position{Line: 0, Col: 14} // on the U of STATUS
	row := strings.TrimRight(ansi.Strip(strings.SplitN(m.View(), "\n", 2)[0]), " ")
	if !strings.HasSuffix(row, "␛[1;33mSTATUS ok") {
		t.Fatalf("rendered = %q, caret must not disturb the line's characters", row)
	}
}

// --- SGR interpretation (#1469) --------------------------------------------

func TestParseSGRSpans(t *testing.T) {
	runes := []rune("id \x1b[1;33mSTATUS\x1b[0m ok")
	spans := parseSGRSpans(runes)
	if len(spans) != 3 {
		t.Fatalf("spans = %+v, want seq + text + seq", spans)
	}
	seq := spans[0]
	if !seq.Seq || seq.Start != 3 || seq.End != 10 {
		t.Fatalf("seq span = %+v, want Seq [3,10)", seq)
	}
	text := spans[1]
	if text.Seq || text.Start != 10 || text.End != 16 {
		t.Fatalf("text span = %+v, want [10,16)", text)
	}
	if !text.Bold || text.FG != lipgloss.Color("3") {
		t.Fatalf("text span = %+v, want bold yellow", text)
	}
	if !spans[2].Seq {
		t.Fatalf("reset span = %+v, want Seq", spans[2])
	}
	// After the reset nothing is styled: no fourth span for " ok".
}

func TestParseSGRSpansExtendedColors(t *testing.T) {
	spans := parseSGRSpans([]rune("\x1b[38;5;196mred"))
	if len(spans) != 2 || spans[1].FG != lipgloss.Color("196") {
		t.Fatalf("spans = %+v, want 256-colour index 196", spans)
	}
	spans = parseSGRSpans([]rune("\x1b[38;2;1;2;3mrgb"))
	if len(spans) != 2 || spans[1].FG == nil {
		t.Fatalf("spans = %+v, want a truecolor foreground", spans)
	}
}

func TestParseSGRSpansIgnoresNonSGR(t *testing.T) {
	// Unterminated escape and a non-'m' CSI: no spans, but the render path
	// still shows them via the control glyphs.
	if spans := parseSGRSpans([]rune("a\x1b[12")); len(spans) != 0 {
		t.Fatalf("unterminated: spans = %+v, want none", spans)
	}
	if spans := parseSGRSpans([]rune("a\x1b[2Jb")); len(spans) != 0 {
		t.Fatalf("non-SGR CSI: spans = %+v, want none", spans)
	}
}

// TestSGRColorsRenderedText: the governed word renders with the file's own
// colour — kept as a deliberate feature — while the neighbouring text stays
// unstyled by it.
func TestSGRColorsRenderedText(t *testing.T) {
	m, _ := loaded(t, "one\nid \x1b[33mSTATUS\n")
	rows := strings.Split(m.View(), "\n")
	// Line 1 is not the cursor line, so any yellow in the row comes from the
	// interpreted SGR state, emitted through lipgloss per cell.
	if !strings.Contains(rows[1], "33m") {
		t.Fatalf("row = %q, want the mapped yellow foreground applied", strconv.Quote(rows[1]))
	}
	if idx := strings.Index(ansi.Strip(rows[1]), "␛"); idx < 0 {
		t.Fatalf("row = %q, want the sequence itself visible", rows[1])
	}
}
