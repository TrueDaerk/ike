package editor

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	"ike/internal/editor/viewport"
)

// cells_test.go — the cell layout (#2526): wide glyphs and grapheme clusters
// render as one unit at their terminal width, and the render loop, the mouse
// map, DisplayOffset and soft wrap agree on where every column sits.

// shrug is 🤷🏼‍♂️: base, skin tone, ZWJ, ♂, VS16 — five runes, one glyph,
// two cells.
const shrug = "\U0001F937\U0001F3FC‍♂️"

// plainRow returns the first rendered row without ANSI styling or right padding.
func plainRow(m Model) string {
	return strings.TrimRight(ansi.Strip(strings.SplitN(m.View(), "\n", 2)[0]), " ")
}

// TestLineCellsLayout pins the width table for the cluster shapes the render
// loop must handle.
func TestLineCellsLayout(t *testing.T) {
	m := New()
	cases := []struct {
		name string
		text string
		want cellWidths
	}{
		{"ascii fast path", "abc", nil},
		{"zwj emoji sequence", "a" + shrug + "b", cellWidths{1, 2, 0, 0, 0, 0, 1}},
		{"skin tone modifier", "\U0001F44D\U0001F3FD", cellWidths{2, 0}},
		{"vs16 text symbol", "☹️", cellWidths{2, 0}},
		{"cjk wide", "日本", cellWidths{2, 2}},
		{"combining mark", "éx", cellWidths{1, 0, 1}},
		{"lone combining mark keeps a cell", "́", cellWidths{1}},
		{"stray zwj between ascii splits", "a‍b", cellWidths{1, 1, 1}},
		{"zero-width space stays a cell", "x​y", cellWidths{1, 1, 1}},
		{"precomposed latin", "über", cellWidths{1, 1, 1, 1}},
	}
	for _, tc := range cases {
		if got := m.lineCells([]rune(tc.text)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: lineCells(%q) = %v, want %v", tc.name, tc.text, got, tc.want)
		}
	}
}

// TestZWJSequenceRendersJoined: the whole sequence reaches the terminal
// verbatim — no ∅ placeholder for the joiner, no torn-off skin tone — and the
// text after it sits at the glyph's real width.
func TestZWJSequenceRendersJoined(t *testing.T) {
	m, _ := loaded(t, "a"+shrug+"b\n")
	row := plainRow(m)
	if !strings.Contains(row, "a"+shrug+"b") {
		t.Fatalf("row %q does not carry the joined sequence", row)
	}
	if strings.Contains(row, "∅") {
		t.Fatalf("row %q shows a joiner placeholder inside an emoji sequence", row)
	}
	if w := ansi.StringWidth(row); w != 4 {
		t.Fatalf("row width = %d, want 4 (a + two-cell glyph + b)", w)
	}
}

// TestWideGlyphNoShift: text after a wide glyph is not shifted — the row's
// display width equals the sum of the cell widths, not the rune count.
func TestWideGlyphNoShift(t *testing.T) {
	m, _ := loaded(t, "日本x\n")
	if row := plainRow(m); ansi.StringWidth(row) != 5 || !strings.HasSuffix(row, "x") {
		t.Fatalf("row = %q (width %d), want 日本x at width 5", row, ansi.StringWidth(row))
	}
}

// TestStrayZWJKeepsPlaceholder: a joiner outside an emoji/joining context is
// still torn out and drawn as ∅ with its #1654 note — the security rendering
// does not relax just because clusters now render joined.
func TestStrayZWJKeepsPlaceholder(t *testing.T) {
	m, _ := loaded(t, "a‍b\n")
	if row := plainRow(m); row != "a∅b" {
		t.Fatalf("row = %q, want a∅b", row)
	}
}

// TestCursorOnWideGlyphHighlightsWhole: the cursor cell on an emoji covers the
// whole glyph — both cells, the full sequence — never a split.
func TestCursorOnWideGlyphHighlightsWhole(t *testing.T) {
	m, _ := loaded(t, "a"+shrug+"b\n")
	m.cursor = buffer.Position{Line: 0, Col: 1}
	rev := lipgloss.NewStyle().Reverse(true)
	body, _ := m.renderSpanUncached(0, 0, -1, 40, rev, lipgloss.NewStyle())
	re := regexp.MustCompile("\x1b\\[7m([^\x1b]*)\x1b\\[")
	got := re.FindStringSubmatch(body)
	if got == nil || got[1] != shrug {
		t.Fatalf("reversed cursor cell = %q, want the whole sequence %q\nbody=%q", got, shrug, body)
	}
	// A cursor parked on an absorbed column (h/l step by rune) renders on the
	// glyph it belongs to instead of vanishing.
	m.cursor.Col = 3
	body, _ = m.renderSpanUncached(0, 0, -1, 40, rev, lipgloss.NewStyle())
	if got := re.FindStringSubmatch(body); got == nil || got[1] != shrug {
		t.Fatalf("cursor on absorbed column: reversed cell = %q, want %q", got, shrug)
	}
}

// TestDisplayOffsetWideGlyphs: overlays anchored after a wide glyph land two
// cells right; a column inside a cluster anchors on the cluster's head.
func TestDisplayOffsetWideGlyphs(t *testing.T) {
	m, _ := loaded(t, "a"+shrug+"b日c\n")
	want := map[int]int{0: 0, 1: 1, 3: 1, 6: 3, 7: 4, 8: 6}
	for col, off := range want {
		if got := m.DisplayOffset(0, col); got != off {
			t.Errorf("DisplayOffset(0, %d) = %d, want %d", col, got, off)
		}
	}
}

// TestClickAfterWideGlyphs: a click lands on the clicked character, not one
// column off per wide glyph; a click inside a glyph lands on its first rune.
func TestClickAfterWideGlyphs(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 2: 1, 3: 6, 4: 7, 5: 7, 6: 8}
	for x, col := range cases {
		// A fresh model per click: repeated clicks on one model would run
		// into the double/triple-click streak.
		m, _ := loaded(t, "a"+shrug+"b日c\n")
		gutter := m.view.GutterWidth(m.buf.LineCount())
		m.MouseClick(gutter+x, 0)
		if m.cursor != (buffer.Position{Line: 0, Col: col}) {
			t.Errorf("click at cell %d: cursor = %v, want {0 %d}", x, m.cursor, col)
		}
	}
}

// TestCombiningMarkRendersJoined: a base plus combining accent is one cell and
// reaches the terminal as one cluster.
func TestCombiningMarkRendersJoined(t *testing.T) {
	m, _ := loaded(t, "éx\n")
	row := plainRow(m)
	if row != "éx" || ansi.StringWidth(row) != 2 {
		t.Fatalf("row = %q (width %d), want éx at width 2", row, ansi.StringWidth(row))
	}
}

// TestWideGlyphAtRightEdgeYields: a wide glyph that would straddle the right
// edge yields to a blank like a clipped tab — the row never overflows and the
// over flag reports the remaining content.
func TestWideGlyphAtRightEdgeYields(t *testing.T) {
	m, _ := loaded(t, "ab日c\n")
	body, over := m.renderSpanUncached(0, 0, -1, 3, lipgloss.NewStyle(), lipgloss.NewStyle())
	if w := ansi.StringWidth(body); w != 3 {
		t.Fatalf("body width = %d, want 3: %q", w, body)
	}
	if strings.Contains(body, "日") || !over {
		t.Fatalf("body = %q over=%v; the straddling glyph must yield and the row report overflow", body, over)
	}
}

// TestWrapBreaksOnCellWidths: under soft wrap a row budgets wide glyphs at two
// cells and never starts on an absorbed column.
func TestWrapBreaksOnCellWidths(t *testing.T) {
	m, _ := loaded(t, strings.Repeat(shrug, 30)+"\n")
	m.softWrap = true
	tw := m.scrollTextWidth()
	segs := m.wrapSegs(0)
	raw := viewport.WrapSegments([]rune(m.buf.Line(0)), tw, m.tabWidth)
	if reflect.DeepEqual(segs, raw) {
		t.Fatalf("segments %v ignore the glyph widths (raw wrap %v)", segs, raw)
	}
	runes := []rune(m.buf.Line(0))
	cw := m.lineCells(runes)
	for i, s := range segs {
		if cw.at(runes, s, m.tabWidth) == 0 {
			t.Errorf("segment %d starts on absorbed column %d", i, s)
		}
		end := viewport.SegmentEnd(segs, i, len(runes))
		w := 0
		for c := s; c < end; c++ {
			w += cw.at(runes, c, m.tabWidth)
		}
		if w > tw {
			t.Errorf("segment %d spans %d cells, exceeds %d", i, w, tw)
		}
	}
}

// TestNarrowLinesUnchanged: an ASCII line with tabs renders exactly as before
// — the fast path hands the loop a nil table.
func TestNarrowLinesUnchanged(t *testing.T) {
	m, _ := loaded(t, "a\tb c\n")
	if row := plainRow(m); row != "a    b c" {
		t.Fatalf("row = %q, want %q", row, "a    b c")
	}
	if got := m.lineCells([]rune("a\tb c")); got != nil {
		t.Fatalf("lineCells on ASCII = %v, want nil", got)
	}
}
