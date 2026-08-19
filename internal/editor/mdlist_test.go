package editor

import (
	"strconv"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// mdlist_test.go covers the markdown list rendering (#1966): unordered items
// render as a two-cell indent plus a bullet, ordered items as their number
// right-aligned to the widest number of their own list.

// mdViewLines renders m and returns the plain text of its rows, trimmed of
// trailing padding, so assertions can compare whole rendered lines.
func mdViewLines(m Model) []string {
	rows := strings.Split(plainView(m), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return rows
}

// mdRow returns the rendered row that shows source line n, with the gutter
// (line numbers, signs) stripped: everything up to and including the source
// line's number.
func mdRow(t *testing.T, m Model, n int) string {
	t.Helper()
	rows := mdViewLines(m)
	if n >= len(rows) {
		t.Fatalf("source line %d beyond the %d rendered rows", n, len(rows))
	}
	return rows[n]
}

// TestListUnorderedBullet: `- item` renders as a two-space indent plus a
// bullet, the marker itself hidden behind the stand-in.
func TestListUnorderedBullet(t *testing.T) {
	m, _ := mdLoaded(t, "- alpha\n- beta\n")
	m.cursor = buffer.Position{Line: 5} // off both item lines
	for n, want := range map[int]string{0: "  • alpha", 1: "  • beta"} {
		if row := mdRow(t, m, n); !strings.HasSuffix(row, want) {
			t.Errorf("line %d renders %q, want it to end in %q", n, row, want)
		}
	}
}

// TestListUnorderedStarAndPlus: the other unordered markers render the same
// bullet; a thematic break stays a rule.
func TestListUnorderedStarAndPlus(t *testing.T) {
	m, _ := mdLoaded(t, "* star\n+ plus\n\n---\n")
	m.cursor = buffer.Position{Line: 6}
	for n, want := range map[int]string{0: "  • star", 1: "  • plus"} {
		if row := mdRow(t, m, n); !strings.HasSuffix(row, want) {
			t.Errorf("line %d renders %q, want it to end in %q", n, row, want)
		}
	}
	if row := mdRow(t, m, 3); !strings.HasSuffix(row, "---") {
		t.Errorf("thematic break renders %q, want the raw rule", row)
	}
}

// TestListOrderedSingleDigit: a one-digit list needs no alignment padding —
// every number sits two cells in.
func TestListOrderedSingleDigit(t *testing.T) {
	m, _ := mdLoaded(t, "1. one\n2. two\n3. three\n")
	m.cursor = buffer.Position{Line: 5}
	for n, want := range map[int]string{0: "  1. one", 1: "  2. two", 2: "  3. three"} {
		if row := mdRow(t, m, n); !strings.HasSuffix(row, want) {
			t.Errorf("line %d renders %q, want it to end in %q", n, row, want)
		}
	}
}

// TestListOrderedWidthBoundary: crossing 9 → 10 the whole list aligns on the
// widest number, right-aligned, so the dots line up in one column.
func TestListOrderedWidthBoundary(t *testing.T) {
	var src strings.Builder
	for i := 1; i <= 10; i++ {
		src.WriteString(strconv.Itoa(i) + ". item\n")
	}
	ranges := detectListRanges(strings.Split(strings.TrimRight(src.String(), "\n"), "\n"))
	for i := 1; i <= 10; i++ {
		want := "  " + strconv.Itoa(i) + "."
		if i < 10 {
			want = "   " + strconv.Itoa(i) + "."
		}
		if got := ranges[i-1].repl; got != want {
			t.Errorf("item %d stands in as %q, want %q", i, got, want)
		}
	}
}

// TestListOrderedWidthBoundaryRendered checks the same alignment end to end:
// the rendered rows put every dot in the same display column.
func TestListOrderedWidthBoundaryRendered(t *testing.T) {
	m, _ := mdLoaded(t, "9. nine\n10. ten\n")
	m.cursor = buffer.Position{Line: 5}
	nine, ten := mdRow(t, m, 0), mdRow(t, m, 1)
	if !strings.HasSuffix(nine, "   9. nine") {
		t.Errorf("line 0 renders %q, want it right-aligned to width 2", nine)
	}
	if !strings.HasSuffix(ten, "  10. ten") {
		t.Errorf("line 1 renders %q, want a two-cell indent", ten)
	}
	if strings.Index(nine, "9. ") != strings.Index(ten, "10.")+1 {
		t.Errorf("dots not in one column:\n%q\n%q", nine, ten)
	}
}

// TestListNestedKeepsIndentAndOwnWidth: a nested list keeps its source indent
// (shifted by the two-cell stand-in) and aligns on its own widest number, not
// the outer list's.
func TestListNestedKeepsIndentAndOwnWidth(t *testing.T) {
	src := []string{
		"1. outer",
		"   1. inner",
		"   2. inner",
		"2. outer",
		"   - bullet",
	}
	ranges := detectListRanges(src)
	for line, want := range map[int]string{
		0: "  1.", 1: "  1.", 2: "  2.", 3: "  2.", 4: "  " + mdBullet,
	} {
		if got := ranges[line].repl; got != want {
			t.Errorf("line %d stands in as %q, want %q", line, got, want)
		}
	}
	if got := ranges[1].start; got != 3 {
		t.Errorf("nested marker starts at column %d, want 3 (the source indent stays)", got)
	}
}

// TestListSeparateRunsAlignApart: a paragraph between two lists ends the
// first one, so a wide list does not pad an unrelated short one.
func TestListSeparateRunsAlignApart(t *testing.T) {
	src := []string{"9. nine", "10. ten", "", "text", "", "1. one"}
	ranges := detectListRanges(src)
	if got := ranges[0].repl; got != "   9." {
		t.Errorf("first list item stands in as %q, want %q", got, "   9.")
	}
	if got := ranges[5].repl; got != "  1." {
		t.Errorf("second list item stands in as %q, want %q", got, "  1.")
	}
}

// TestListSkipsFencedCode: list-looking lines inside a fence are source, not
// markdown, and keep their raw markers.
func TestListSkipsFencedCode(t *testing.T) {
	src := []string{"```sh", "- not a list", "1. neither", "```", "- yes"}
	ranges := detectListRanges(src)
	for _, line := range []int{1, 2} {
		if _, ok := ranges[line]; ok {
			t.Errorf("line %d inside the fence must not render a marker", line)
		}
	}
	if _, ok := ranges[4]; !ok {
		t.Error("the list after the fence must still render")
	}
}

// TestListCaretRevealsRawMarker (#1594): the caret on the marker shows the
// raw source, like every other conceal range.
func TestListCaretRevealsRawMarker(t *testing.T) {
	m, _ := mdLoaded(t, "10. ten\n11. eleven\n")
	m.cursor = buffer.Position{Line: 0, Col: 1}
	row := mdRow(t, m, 0)
	if !strings.HasSuffix(row, "10. ten") || strings.Contains(row, "  10. ten") {
		t.Errorf("caret on the marker renders %q, want the raw source", row)
	}
	if other := mdRow(t, m, 1); !strings.HasSuffix(other, "  11. eleven") {
		t.Errorf("the other line renders %q, want it still rendered", other)
	}
}

// TestListToggleOffKeepsSource: with markdown rendering toggled off the
// markers render raw again.
func TestListToggleOffKeepsSource(t *testing.T) {
	m, _ := mdLoaded(t, "- alpha\n1. one\n")
	m.cursor = buffer.Position{Line: 5}
	m.toggleMarkdownRendering()
	for n, want := range map[int]string{0: "- alpha", 1: "1. one"} {
		row := mdRow(t, m, n)
		if !strings.HasSuffix(row, want) || strings.Contains(row, mdBullet) {
			t.Errorf("line %d renders %q with rendering off, want the raw source", n, row)
		}
	}
}

// TestListNonMarkdownBufferUntouched: the layer is markdown-only.
func TestListNonMarkdownBufferUntouched(t *testing.T) {
	m := New()
	m.SetSize(60, 10)
	m.buf = buffer.New([]string{"- alpha"})
	if r := m.mdListConcealRanges(0); len(r) != 0 {
		t.Errorf("a non-markdown buffer got %d list ranges, want none", len(r))
	}
}
